package com.tgdrive.app.data

import android.content.ContentValues
import android.content.Context
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.io.OutputStream
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong

private const val DOWNLOAD_BUFFER_SIZE = 64 * 1024

enum class DownloadStatus {
    QUEUED,
    DOWNLOADING,
    PAUSED,
    COMPLETED,
    FAILED,
    CANCELED,
}

data class DownloadTransfer(
    val id: Long,
    val fileName: String,
    val downloadedBytes: Long,
    val totalBytes: Long,
    val status: DownloadStatus,
    val errorMessage: String? = null,
) {
    val progress: Int?
        get() {
            if (totalBytes <= 0L) return null
            return ((downloadedBytes.coerceAtLeast(0L) * 100L) / totalBytes)
                .coerceIn(0L, 100L)
                .toInt()
        }

    val isActive: Boolean
        get() = status == DownloadStatus.QUEUED ||
            status == DownloadStatus.DOWNLOADING ||
            status == DownloadStatus.PAUSED
}

object DownloadTransfers {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO.limitedParallelism(2))
    private val nextId = AtomicLong(System.currentTimeMillis())
    private val tasks = ConcurrentHashMap<Long, DownloadTask>()
    private val _transfers = MutableStateFlow<List<DownloadTransfer>>(emptyList())
    val transfers: StateFlow<List<DownloadTransfer>> = _transfers.asStateFlow()
    private val client = OkHttpClient.Builder()
        .connectTimeout(30, TimeUnit.SECONDS)
        .readTimeout(0, TimeUnit.MILLISECONDS)
        .callTimeout(0, TimeUnit.MILLISECONDS)
        .retryOnConnectionFailure(true)
        .build()

    fun start(context: Context, url: String, token: String, fileName: String, mimeType: String): Long {
        val id = nextId.getAndIncrement()
        val appContext = context.applicationContext
        val target = runCatching { DownloadTarget.create(appContext, fileName, mimeType) }
            .getOrElse { error ->
                publish(
                    DownloadTransfer(
                        id = id,
                        fileName = fileName,
                        downloadedBytes = 0L,
                        totalBytes = 0L,
                        status = DownloadStatus.FAILED,
                        errorMessage = error.message ?: "Tidak dapat membuat file download",
                    ),
                )
                TransferNotifications.downloadFailed(appContext, id, fileName)
                return id
            }
        val task = DownloadTask(
            id = id,
            context = appContext,
            url = url,
            token = token,
            fileName = fileName,
            target = target,
        )
        tasks[id] = task
        publish(task)
        TransferNotifications.downloadProgress(appContext, id, fileName, 0L, 0L)
        startJob(task)
        return id
    }

    fun pause(downloadId: Long) {
        val task = tasks[downloadId] ?: return
        if (task.status != DownloadStatus.DOWNLOADING) return
        task.status = DownloadStatus.PAUSED
        task.call?.cancel()
        publish(task)
        TransferNotifications.downloadProgress(
            task.context,
            task.id,
            task.fileName,
            task.downloadedBytes,
            task.totalBytes,
            paused = true,
        )
    }

    fun resume(downloadId: Long) {
        val task = tasks[downloadId] ?: return
        if (task.status != DownloadStatus.PAUSED) return
        task.status = DownloadStatus.QUEUED
        publish(task)
        startJob(task)
    }

    fun cancel(downloadId: Long) {
        val task = tasks[downloadId] ?: return
        if (!task.isActive) return
        task.status = DownloadStatus.CANCELED
        task.call?.cancel()
        if (task.job?.isActive != true) {
            finishCanceled(task)
        } else {
            publish(task)
        }
    }

    fun handleAction(context: Context, action: String, downloadId: Long) {
        when (action) {
            TransferActionReceiver.ACTION_PAUSE -> pause(downloadId)
            TransferActionReceiver.ACTION_RESUME -> resume(downloadId)
            TransferActionReceiver.ACTION_CANCEL -> cancel(downloadId)
        }
    }

    private fun startJob(task: DownloadTask) {
        task.job = scope.launch { run(task) }
    }

    private fun run(task: DownloadTask) {
        try {
            if (task.status == DownloadStatus.CANCELED) {
                finishCanceled(task)
                return
            }
            if (task.status == DownloadStatus.PAUSED) return
            task.status = DownloadStatus.DOWNLOADING
            publish(task)
            download(task)
            if (task.totalBytes > 0L && task.downloadedBytes < task.totalBytes) {
                throw IOException("Download terputus sebelum selesai")
            }
            if (task.status == DownloadStatus.DOWNLOADING || task.contentComplete) {
                task.target.complete()
                task.status = DownloadStatus.COMPLETED
                TransferNotifications.downloadFinished(task.context, task.id, task.fileName)
            } else if (task.status == DownloadStatus.CANCELED) {
                finishCanceled(task)
            }
        } catch (error: Exception) {
            when (task.status) {
                DownloadStatus.PAUSED -> {
                    // The partial file remains in place. Resume starts a Range request from its length.
                }
                DownloadStatus.CANCELED -> finishCanceled(task)
                else -> {
                    task.target.delete()
                    task.status = DownloadStatus.FAILED
                    task.errorMessage = error.message ?: "Download gagal"
                    TransferNotifications.downloadFailed(task.context, task.id, task.fileName)
                }
            }
        } finally {
            task.call = null
            if (!task.isActive) tasks.remove(task.id)
            publish(task)
        }
    }

    private fun download(task: DownloadTask) {
        val existingBytes = task.target.length()
        val request = Request.Builder()
            .url(task.url)
            .apply {
                if (task.token.isNotBlank()) {
                    header("Authorization", "Bearer ${task.token}")
                }
                if (existingBytes > 0L) {
                    header("Range", "bytes=$existingBytes-")
                }
            }
            .build()
        val call = client.newCall(request)
        task.call = call
        call.execute().use { response ->
            if (!response.isSuccessful) {
                throw IOException("Download gagal: HTTP ${response.code}")
            }
            val resumed = existingBytes > 0L && response.code == 206
            if (resumed) {
                task.downloadedBytes = existingBytes
            } else {
                task.target.reset()
                task.downloadedBytes = 0L
            }
            val body = response.body ?: throw IOException("Respons download kosong")
            task.totalBytes = response.header("Content-Range")
                ?.substringAfterLast('/')
                ?.toLongOrNull()
                ?: if (body.contentLength() > 0L) task.downloadedBytes + body.contentLength() else 0L
            notifyProgress(task)
            body.byteStream().use { input ->
                task.target.open(append = resumed).use { output ->
                    val buffer = ByteArray(DOWNLOAD_BUFFER_SIZE)
                    while (true) {
                        if (task.status != DownloadStatus.DOWNLOADING) return
                        val count = input.read(buffer)
                        if (count < 0) {
                            task.contentComplete = true
                            break
                        }
                        if (count == 0) continue
                        output.write(buffer, 0, count)
                        task.downloadedBytes += count
                        notifyProgress(task)
                    }
                }
            }
        }
    }

    private fun finishCanceled(task: DownloadTask) {
        task.target.delete()
        task.status = DownloadStatus.CANCELED
        tasks.remove(task.id)
        TransferNotifications.downloadCanceled(task.context, task.id, task.fileName)
        publish(task)
    }

    private fun notifyProgress(task: DownloadTask) {
        val now = System.currentTimeMillis()
        if (now - task.lastNotificationAt < 250L) return
        task.lastNotificationAt = now
        publish(task)
        TransferNotifications.downloadProgress(
            task.context,
            task.id,
            task.fileName,
            task.downloadedBytes,
            task.totalBytes,
            paused = task.status == DownloadStatus.PAUSED,
        )
    }

    private fun publish(task: DownloadTask) {
        publish(
            DownloadTransfer(
                id = task.id,
                fileName = task.fileName,
                downloadedBytes = task.downloadedBytes,
                totalBytes = task.totalBytes,
                status = task.status,
                errorMessage = task.errorMessage,
            ),
        )
    }

    private fun publish(transfer: DownloadTransfer) {
        _transfers.update { current ->
            val updated = buildList {
                add(transfer)
                addAll(current.filterNot { it.id == transfer.id })
            }
            val active = updated.filter { it.isActive }
            val terminalLimit = (MAX_VISIBLE_TRANSFERS - active.size).coerceAtLeast(0)
            active + updated.filterNot { it.isActive }.take(terminalLimit)
        }
    }

    private class DownloadTask(
        val id: Long,
        val context: Context,
        val url: String,
        val token: String,
        val fileName: String,
        val target: DownloadTarget,
    ) {
        @Volatile var call: okhttp3.Call? = null
        @Volatile var job: Job? = null
        @Volatile var downloadedBytes: Long = 0L
        @Volatile var totalBytes: Long = 0L
        @Volatile var lastNotificationAt: Long = 0L
        @Volatile var status: DownloadStatus = DownloadStatus.QUEUED
        @Volatile var errorMessage: String? = null
        @Volatile var contentComplete: Boolean = false

        val isActive: Boolean
            get() = status == DownloadStatus.QUEUED ||
                status == DownloadStatus.DOWNLOADING ||
                status == DownloadStatus.PAUSED
    }

    private const val MAX_VISIBLE_TRANSFERS = 30
}

private class DownloadTarget private constructor(
    private val context: Context,
    private val fileName: String,
    private val mimeType: String,
    private val partialFile: File,
) {
    private var publishedUri: Uri? = null
    private var publishedFile: File? = null

    fun length(): Long = partialFile.length()

    fun open(append: Boolean): OutputStream = FileOutputStream(partialFile, append)

    fun reset() {
        open(append = false).use { }
    }

    fun complete() {
        if (Build.VERSION.SDK_INT >= 29) {
            publishToMediaStore()
        } else {
            publishToLegacyDownloads()
        }
        partialFile.delete()
    }

    fun delete() {
        publishedUri?.let { context.contentResolver.delete(it, null, null) }
        publishedFile?.delete()
        partialFile.delete()
    }

    private fun publishToMediaStore() {
        val values = ContentValues().apply {
            put(MediaStore.Downloads.DISPLAY_NAME, fileName)
            put(MediaStore.Downloads.MIME_TYPE, mimeType)
            put(MediaStore.Downloads.RELATIVE_PATH, Environment.DIRECTORY_DOWNLOADS)
            put(MediaStore.Downloads.IS_PENDING, 1)
        }
        val uri = context.contentResolver.insert(
            MediaStore.Downloads.EXTERNAL_CONTENT_URI,
            values,
        ) ?: throw IOException("Tidak dapat membuat file download")
        publishedUri = uri
        try {
            val output = context.contentResolver.openOutputStream(uri, "w")
                ?: throw IOException("Tidak dapat membuka file download")
            partialFile.inputStream().use { input ->
                output.use { input.copyTo(it) }
            }
            val updated = context.contentResolver.update(
                uri,
                ContentValues().apply { put(MediaStore.Downloads.IS_PENDING, 0) },
                null,
                null,
            )
            if (updated == 0) throw IOException("Tidak dapat menyelesaikan file download")
        } catch (error: Exception) {
            context.contentResolver.delete(uri, null, null)
            publishedUri = null
            throw IOException("Gagal menyimpan file ke Downloads: ${error.message}", error)
        }
    }

    private fun publishToLegacyDownloads() {
        val directory = Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS)
        if (!directory.exists() && !directory.mkdirs()) {
            throw IOException("Tidak dapat membuka folder Downloads")
        }
        val destination = uniqueFile(directory, fileName)
        try {
            partialFile.inputStream().use { input ->
                destination.outputStream().use { output -> input.copyTo(output) }
            }
            publishedFile = destination
        } catch (error: Exception) {
            destination.delete()
            throw IOException("Gagal menyimpan file ke Downloads: ${error.message}", error)
        }
    }

    companion object {
        fun create(context: Context, fileName: String, mimeType: String): DownloadTarget {
            val partialDirectory = File(context.filesDir, "downloads")
            if (!partialDirectory.exists() && !partialDirectory.mkdirs()) {
                throw IOException("Tidak dapat membuat penyimpanan download")
            }
            val safeName = sanitizeFileName(fileName)
            val partialFile = File(partialDirectory, "$safeName.${System.nanoTime()}.part")
            return DownloadTarget(
                context = context,
                fileName = safeName,
                mimeType = mimeType.ifBlank { "application/octet-stream" },
                partialFile = partialFile,
            )
        }

        private fun sanitizeFileName(value: String): String {
            val name = value.substringAfterLast('/').substringAfterLast('\\')
                .replace('\u0000', '_')
                .trim()
            return name.ifBlank { "download" }.take(180)
        }

        private fun uniqueFile(directory: File, name: String): File {
            val extensionIndex = name.lastIndexOf('.')
            val base = if (extensionIndex > 0) name.substring(0, extensionIndex) else name
            val extension = if (extensionIndex > 0) name.substring(extensionIndex) else ""
            var candidate = File(directory, name)
            var suffix = 1
            while (candidate.exists()) {
                candidate = File(directory, "$base ($suffix)$extension")
                suffix++
            }
            return candidate
        }
    }
}
