package com.tgdrive.app.data

import android.content.ContentResolver
import android.net.Uri
import com.tgdrive.app.model.AppStats
import com.tgdrive.app.model.DriveEntry
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import okhttp3.Callback
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.OkHttpClient
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Request
import okhttp3.RequestBody
import okio.BufferedSink
import org.json.JSONObject
import java.io.File
import java.io.IOException
import java.time.Instant
import java.util.UUID
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

private const val STREAM_BUFFER_SIZE = 64 * 1024
private const val UPLOAD_PROGRESS_POLL_INTERVAL_MS = 250L

class DriveApi(
    serverUrl: String,
    private val accessToken: String,
) {
    private val baseUrl = serverUrl.trim().trimEnd('/')
    @Volatile
    private var activeUploadId: String? = null
    private val client = OkHttpClient.Builder()
        .connectTimeout(30, TimeUnit.SECONDS)
        .readTimeout(0, TimeUnit.MILLISECONDS)
        .writeTimeout(0, TimeUnit.MILLISECONDS)
        .callTimeout(0, TimeUnit.MILLISECONDS)
        .retryOnConnectionFailure(true)
        .build()

    suspend fun checkConnection(): Boolean {
        val request = Request.Builder()
            .url("$baseUrl/api/v1/health")
            .get()
            .build()
        return withContext(Dispatchers.IO) {
            runCatching {
                client.newCall(request).execute().use { response -> response.isSuccessful }
            }.getOrDefault(false)
        }
    }

    suspend fun listEntries(directory: String): List<DriveEntry> {
        val json = getJson("entries", directory)
        val entries = json.optJSONArray("entries") ?: return emptyList()
        return buildList(entries.length()) {
            for (index in 0 until entries.length()) {
                add(parseEntry(entries.getJSONObject(index)))
            }
        }
    }

    suspend fun search(query: String): List<DriveEntry> {
        val json = getJson("search", null, mapOf("q" to query))
        val entries = json.optJSONArray("entries") ?: return emptyList()
        return buildList(entries.length()) {
            for (index in 0 until entries.length()) {
                add(parseEntry(entries.getJSONObject(index)))
            }
        }
    }

    suspend fun stats(): AppStats {
        val json = executeJson(
            Request.Builder()
                .url(endpointUrl("stats"))
                .get()
                .build(),
        )
        return AppStats(
            fileCount = json.optInt("file_count"),
            totalBytes = json.optLong("total_bytes"),
        )
    }

    suspend fun uploadUri(
        contentResolver: ContentResolver,
        source: Uri,
        remotePath: String,
        mimeType: String,
        size: Long,
        control: TransferControl = TransferControl(),
        onProgress: (sentBytes: Long, totalBytes: Long) -> Unit = { _, _ -> },
    ) {
        val uploadId = UUID.randomUUID().toString()
        activeUploadId = uploadId
        val body = ContentUriRequestBody(contentResolver, source, mimeType, size, control)
        val request = authorizedRequest(endpointUrl("files", remotePath))
            .post(body)
            .header("Content-Type", mimeType)
            .header("X-Upload-ID", uploadId)
            .build()

        try {
            coroutineScope {
                val uploadCall = async(Dispatchers.IO) { executeCancellable(request) }
                try {
                    while (!uploadCall.isCompleted) {
                        runCatching { pollUploadProgress(uploadId) }.getOrNull()?.let { progress ->
                            onProgress(progress.uploadedBytes, progress.totalBytes)
                        }
                        delay(UPLOAD_PROGRESS_POLL_INTERVAL_MS)
                    }
                    uploadCall.await()
                    runCatching { pollUploadProgress(uploadId) }.getOrNull()?.let { progress ->
                        onProgress(progress.uploadedBytes, progress.totalBytes)
                    }
                } finally {
                    if (!uploadCall.isCompleted) uploadCall.cancel()
                }
            }
        } finally {
            if (activeUploadId == uploadId) activeUploadId = null
        }
    }

    suspend fun pauseUpload() = controlActiveUpload("pause")

    suspend fun resumeUpload() = controlActiveUpload("resume")

    suspend fun cancelUpload() = controlActiveUpload("cancel")

    suspend fun clearCache() {
        execute(authorizedRequest(endpointUrl("cache")).delete().build())
    }

    suspend fun downloadTo(remotePath: String, destination: File) {
        val request = authorizedRequest(contentUrl(remotePath, download = true))
            .get()
            .build()
        withContext(Dispatchers.IO) {
            client.newCall(request).execute().use { response ->
                checkResponse(response)
                val body = response.body ?: throw DriveApiException("Empty download response")
                destination.outputStream().use { output -> body.byteStream().copyTo(output) }
            }
        }
    }

    suspend fun createFolder(folderPath: String) {
        val body = JSONObject().put("path", folderPath).toString()
            .toRequestBody("application/json".toMediaTypeOrNull())
        val request = authorizedRequest(endpointUrl("folders"))
            .post(body)
            .header("Content-Type", "application/json")
            .build()
        execute(request)
    }

    suspend fun rename(oldPath: String, newPath: String) {
        val body = JSONObject()
            .put("old_path", oldPath)
            .put("new_path", newPath)
            .toString()
            .toRequestBody("application/json".toMediaTypeOrNull())
        val request = authorizedRequest(endpointUrl("files"))
            .patch(body)
            .header("Content-Type", "application/json")
            .build()
        execute(request)
    }

    suspend fun deleteFile(filePath: String) {
        execute(authorizedRequest(endpointUrl("files", filePath)).delete().build())
    }

    suspend fun deleteFolder(folderPath: String) {
        execute(authorizedRequest(endpointUrl("folders", folderPath)).delete().build())
    }

    fun contentUrl(filePath: String, download: Boolean = false): String {
        val suffix = if (download) "&download=1" else ""
        return "${authorizedMediaUrl("files/content", filePath)}$suffix"
    }

    fun mediaContentUrl(filePath: String): String = authorizedMediaUrl("files/content", filePath)

    fun thumbnailUrl(filePath: String): String = authorizedMediaUrl("files/thumbnail", filePath)

    fun accessToken(): String = accessToken

    fun hasAccessToken(): Boolean = accessToken.isNotBlank()

    fun baseUrl(): String = baseUrl

    private suspend fun getJson(
        endpoint: String,
        path: String?,
        extraQuery: Map<String, String> = emptyMap(),
    ): JSONObject {
        val url = buildString {
            append(endpointUrl(endpoint))
            append('?')
            var first = true
            if (path != null) {
                append("path=")
                append(Uri.encode(path))
                first = false
            }
            for ((key, value) in extraQuery) {
                if (!first) append('&')
                append(Uri.encode(key))
                append('=')
                append(Uri.encode(value))
                first = false
            }
        }
        return executeJson(authorizedRequest(url).get().build())
    }

    private fun endpointUrl(endpoint: String, path: String? = null): String {
        val query = if (path == null) "" else "?path=${Uri.encode(path)}"
        return "$baseUrl/api/v1/$endpoint$query"
    }

    private fun authorizedMediaUrl(endpoint: String, path: String): String {
        val tokenQuery = if (accessToken.isBlank()) "" else "&token=${Uri.encode(accessToken)}"
        return "${endpointUrl(endpoint, path)}$tokenQuery"
    }

    private fun authorizedRequest(url: String): Request.Builder {
        val builder = Request.Builder().url(url)
        if (accessToken.isNotBlank()) {
            builder.header("Authorization", "Bearer $accessToken")
        }
        return builder
    }

    private suspend fun execute(request: Request) {
        withContext(Dispatchers.IO) {
            client.newCall(request).execute().use(::checkResponse)
        }
    }

    private suspend fun executeCancellable(request: Request) = suspendCancellableCoroutine<Unit> { continuation ->
        val call = client.newCall(request)
        continuation.invokeOnCancellation { call.cancel() }
        call.enqueue(object : Callback {
            override fun onFailure(call: okhttp3.Call, error: IOException) {
                if (continuation.isActive) continuation.resumeWithException(error)
            }

            override fun onResponse(call: okhttp3.Call, response: okhttp3.Response) {
                response.use {
                    try {
                        checkResponse(it)
                        if (continuation.isActive) continuation.resume(Unit)
                    } catch (error: Exception) {
                        if (continuation.isActive) continuation.resumeWithException(error)
                    }
                }
            }
        })
    }

    private suspend fun controlActiveUpload(action: String) {
        val uploadId = activeUploadId ?: return
        val request = authorizedRequest(
            endpointUrl("uploads/control") + "?upload_id=${Uri.encode(uploadId)}&action=$action",
        )
            .post(ByteArray(0).toRequestBody(null))
            .build()
        execute(request)
    }

    private suspend fun pollUploadProgress(uploadId: String): UploadProgress? = withContext(Dispatchers.IO) {
        val request = authorizedRequest(endpointUrl("uploads/progress") + "?upload_id=${Uri.encode(uploadId)}")
            .get()
            .build()
        client.newCall(request).execute().use { response ->
            if (response.code == 404) return@withContext null
            checkResponse(response)
            val json = JSONObject(response.body?.string().orEmpty())
            UploadProgress(
                uploadedBytes = json.optLong("uploaded_bytes", 0L),
                totalBytes = json.optLong("total_bytes", -1L),
            )
        }
    }

    private suspend fun executeJson(request: Request): JSONObject = withContext(Dispatchers.IO) {
        client.newCall(request).execute().use { response ->
            checkResponse(response)
            val content = response.body?.string().orEmpty()
            JSONObject(content)
        }
    }

    private fun checkResponse(response: okhttp3.Response) {
        if (response.isSuccessful) return
        val message = runCatching {
            JSONObject(response.body?.string().orEmpty()).optString("error")
        }.getOrDefault("")
        throw DriveApiException(message.ifBlank { "Request failed: HTTP ${response.code}" })
    }

    private fun parseEntry(json: JSONObject): DriveEntry {
        val updatedAt = json.optString("updated_at").takeIf { it.isNotBlank() }?.let {
            runCatching { Instant.parse(it) }.getOrNull()
        }
        return DriveEntry(
            name = json.optString("name"),
            path = json.optString("path"),
            isDirectory = json.optBoolean("is_dir"),
            size = json.optLong("size"),
            contentHash = json.optString("content_hash"),
            updatedAt = updatedAt,
            mimeType = json.optString("mime_type", "application/octet-stream"),
            hasThumbnail = json.optBoolean("thumbnail"),
        )
    }
}

class DriveApiException(message: String) : IOException(message)

class UnreadableContentException(message: String, cause: IOException? = null) : IOException(message, cause)

private data class UploadProgress(
    val uploadedBytes: Long,
    val totalBytes: Long,
)

class TransferControl {
    private val lock = Object()
    @Volatile
    private var paused = false
    @Volatile
    private var cancelled = false

    fun pause() {
        synchronized(lock) { paused = true }
    }

    fun resume() {
        synchronized(lock) {
            paused = false
            lock.notifyAll()
        }
    }

    fun cancel() {
        synchronized(lock) {
            cancelled = true
            paused = false
            lock.notifyAll()
        }
    }

    fun isPaused(): Boolean = paused

    fun awaitIfPaused() {
        synchronized(lock) {
            while (paused && !cancelled) {
                try {
                    lock.wait(250L)
                } catch (_: InterruptedException) {
                    Thread.currentThread().interrupt()
                    throw IOException("Transfer dihentikan")
                }
            }
            if (cancelled) throw IOException("Transfer dibatalkan")
        }
    }
}

private class ContentUriRequestBody(
    private val contentResolver: ContentResolver,
    private val source: Uri,
    private val mimeType: String,
    private val size: Long,
    private val control: TransferControl,
) : RequestBody() {
    override fun contentType() = mimeType.toMediaTypeOrNull()

    override fun contentLength(): Long = size

    override fun writeTo(sink: BufferedSink) {
        val input = try {
            contentResolver.openInputStream(source)
                ?: throw UnreadableContentException("Media tidak dapat dibuka")
        } catch (error: UnreadableContentException) {
            throw error
        } catch (error: IOException) {
            throw UnreadableContentException("Media tidak dapat dibuka", error)
        }
        input.use { stream ->
            val buffer = ByteArray(STREAM_BUFFER_SIZE)
            while (true) {
                control.awaitIfPaused()
                val count = try {
                    stream.read(buffer)
                } catch (error: IOException) {
                    throw UnreadableContentException("Media tidak dapat dibaca", error)
                }
                if (count < 0) break
                sink.write(buffer, 0, count)
            }
        }
    }
}
