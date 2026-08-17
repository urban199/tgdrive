package com.tgdrive.app.worker

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.ContentUris
import android.content.Context
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.os.Build
import android.provider.MediaStore
import android.util.Log
import androidx.core.content.ContextCompat
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.ForegroundInfo
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.tgdrive.app.data.EmbeddedRuntime
import com.tgdrive.app.data.DriveApi
import com.tgdrive.app.data.DriveApiException
import com.tgdrive.app.data.SettingsStore
import com.tgdrive.app.data.UnreadableContentException
import com.tgdrive.app.model.AppSettings
import com.tgdrive.app.model.DriveEntry
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.IOException
import java.security.MessageDigest
import java.util.concurrent.TimeUnit

class MediaSyncWorker(
    appContext: Context,
    workerParameters: WorkerParameters,
) : CoroutineWorker(appContext, workerParameters) {
    private val settingsStore = SettingsStore(appContext)

    override suspend fun doWork(): Result {
        val settings = settingsStore.load()
        if (!settings.isTelegramConfigured() || (!settings.syncPhotos && !settings.syncVideos)) {
            return Result.success()
        }
        if (!hasMediaPermission(settings)) {
            showSyncFailure("Izin galeri diperlukan untuk sinkronisasi.")
            return Result.failure()
        }
        showSyncNotification()
        return withContext(Dispatchers.IO) {
            try {
                val mediaCounts = mediaCandidateCounts(settings)
                showSyncNotification(mediaCounts)
                ensureSyncEnabled(settings.syncPhotos)
                val api = EmbeddedRuntime.start(applicationContext, settings)
                val skippedUnreadableMedia = syncMedia(api, settings)
                settingsStore.saveLastSync(System.currentTimeMillis())
                if (skippedUnreadableMedia > 0) showSkippedUnreadableMedia(skippedUnreadableMedia)
                Result.success()
            } catch (error: CancellationException) {
                throw error
            } catch (error: SecurityException) {
                Log.e(TAG, "Media permission was rejected", error)
                showSyncFailure(syncFailureMessage(error))
                Result.failure()
            } catch (error: IllegalArgumentException) {
                Log.e(TAG, "Invalid sync configuration", error)
                showSyncFailure(syncFailureMessage(error))
                Result.failure()
            } catch (error: IOException) {
                Log.e(TAG, "Media sync failed; retrying", error)
                showSyncFailure(syncFailureMessage(error))
                Result.retry()
            } catch (error: Exception) {
                Log.e(TAG, "Media sync failed", error)
                showSyncFailure(syncFailureMessage(error))
                Result.retry()
            }
        }
    }

    private suspend fun showSyncNotification(mediaCounts: MediaCounts? = null) {
        try {
            setForeground(createForegroundInfo(mediaCounts))
        } catch (error: Exception) {
            Log.w(TAG, "Foreground sync notification unavailable", error)
        }
    }

    private fun showSyncFailure(message: String) = showSyncNotice("Sync gagal", message)

    private fun showSkippedUnreadableMedia(count: Int) {
        showSyncNotice("Sync selesai", "$count media tidak dapat dibaca dan dilewati.")
    }

    private fun showSyncNotice(title: String, message: String) {
        val manager = applicationContext.getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            manager.createNotificationChannel(
                NotificationChannel(
                    SYNC_ERROR_CHANNEL_ID,
                    "Masalah sinkronisasi",
                    NotificationManager.IMPORTANCE_DEFAULT,
                ).apply { setShowBadge(false) },
            )
        }
        val notification = Notification.Builder(applicationContext, SYNC_ERROR_CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_sys_upload)
            .setContentTitle(title)
            .setContentText(message)
            .setStyle(Notification.BigTextStyle().bigText(message))
            .setAutoCancel(true)
            .setOnlyAlertOnce(true)
            .build()
        manager.notify(SYNC_ERROR_NOTIFICATION_ID, notification)
    }

    private fun syncFailureMessage(error: Exception): String {
        val details = StringBuilder()
        var cause: Throwable? = error
        var truncated = false
        while (cause != null) {
            if (details.length >= MAX_SYNC_ERROR_NOTIFICATION_CHARS) {
                truncated = true
                break
            }
            if (details.isNotEmpty()) details.append('\n')
            details.append(cause.javaClass.simpleName)
            val message = cause.message.orEmpty().take(MAX_SYNC_ERROR_NOTIFICATION_CHARS)
            if (message.isNotBlank()) details.append(": ").append(message)
            cause = cause.cause
        }
        return if (!truncated && details.length <= MAX_SYNC_ERROR_NOTIFICATION_CHARS) {
            details.toString()
        } else {
            details.substring(0, MAX_SYNC_ERROR_NOTIFICATION_CHARS - TRUNCATED_ERROR_SUFFIX.length) +
                TRUNCATED_ERROR_SUFFIX
        }
    }

    private fun createForegroundInfo(mediaCounts: MediaCounts? = null): ForegroundInfo {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val manager = applicationContext.getSystemService(NotificationManager::class.java)
            manager.createNotificationChannel(
                NotificationChannel(
                    SYNC_CHANNEL_ID,
                    "Sinkronisasi TgDrive",
                    NotificationManager.IMPORTANCE_LOW,
                ).apply { setShowBadge(false) },
            )
        }
        val notification = Notification.Builder(applicationContext, SYNC_CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_sys_upload)
            .setContentTitle("Sinkronisasi TgDrive")
            .setContentText(
                mediaCounts?.let { "Akan sync ${it.photos} foto dan ${it.videos} video" }
                    ?: "Memeriksa media yang perlu disinkronkan",
            )
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .build()
        return ForegroundInfo(
            SYNC_NOTIFICATION_ID,
            notification,
            ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC,
        )
    }

    private fun hasMediaPermission(settings: AppSettings): Boolean {
        if (Build.VERSION.SDK_INT < 33) {
            return ContextCompat.checkSelfPermission(
                applicationContext,
                android.Manifest.permission.READ_EXTERNAL_STORAGE,
            ) == PackageManager.PERMISSION_GRANTED
        }
        val selectedPermission = ContextCompat.checkSelfPermission(
            applicationContext,
            android.Manifest.permission.READ_MEDIA_VISUAL_USER_SELECTED,
        ) == PackageManager.PERMISSION_GRANTED
        val photosGranted = !settings.syncPhotos || selectedPermission || ContextCompat.checkSelfPermission(
            applicationContext,
            android.Manifest.permission.READ_MEDIA_IMAGES,
        ) == PackageManager.PERMISSION_GRANTED
        val videosGranted = !settings.syncVideos || selectedPermission || ContextCompat.checkSelfPermission(
            applicationContext,
            android.Manifest.permission.READ_MEDIA_VIDEO,
        ) == PackageManager.PERMISSION_GRANTED
        return photosGranted && videosGranted
    }

    private suspend fun syncMedia(api: DriveApi, settings: AppSettings): Int {
        val createdFolders = mutableSetOf<String>()
        val remoteEntriesByFolder = mutableMapOf<String, List<DriveEntry>>()
        var skippedUnreadableMedia = 0
        if (settings.syncPhotos) {
            skippedUnreadableMedia += syncCollection(api, settings, true, createdFolders, remoteEntriesByFolder)
        }
        if (settings.syncVideos) {
            skippedUnreadableMedia += syncCollection(api, settings, false, createdFolders, remoteEntriesByFolder)
        }
        return skippedUnreadableMedia
    }

    private fun mediaCandidateCounts(settings: AppSettings): MediaCounts = MediaCounts(
        photos = if (settings.syncPhotos) mediaCandidateCount(true) else 0,
        videos = if (settings.syncVideos) mediaCandidateCount(false) else 0,
    )

    private fun mediaCandidateCount(isPhoto: Boolean): Int {
        val checkpointType = if (isPhoto) "images" else "videos"
        val mediaStoreVersion = mediaStoreVersion()
        val checkpoint = settingsStore.getMediaStoreCheckpoint(checkpointType)
            ?.takeIf { it.version == mediaStoreVersion }
        return mediaVolumes().sumOf { volume ->
            ensureSyncEnabled(isPhoto)
            val generation = mediaStoreGeneration(volume)
            val previousGeneration = checkpoint?.generations?.get(volume)
            if (generation != null && previousGeneration != null && generation <= previousGeneration) {
                return@sumOf 0
            }
            val selection = "${mediaSelection(isPhoto, previousGeneration)} AND ${MediaStore.MediaColumns.SIZE} <= ?"
            val selectionArgs = mediaSelectionArgs(isPhoto, previousGeneration) + AUTO_SYNC_MAX_BYTES.toString()
            applicationContext.contentResolver.query(
                MediaStore.Files.getContentUri(volume),
                arrayOf(MediaStore.MediaColumns._ID),
                selection,
                selectionArgs,
                null,
            )?.use { it.count } ?: throw IOException("MediaStore did not return volume $volume")
        }
    }

    private suspend fun syncCollection(
        api: DriveApi,
        settings: AppSettings,
        isPhoto: Boolean,
        createdFolders: MutableSet<String>,
        remoteEntriesByFolder: MutableMap<String, List<DriveEntry>>,
    ): Int {
        var skippedUnreadableMedia = 0
        ensureSyncEnabled(isPhoto)
        val checkpointType = if (isPhoto) "images" else "videos"
        val mediaStoreVersion = mediaStoreVersion()
        val checkpoint = settingsStore.getMediaStoreCheckpoint(checkpointType)
            ?.takeIf { it.version == mediaStoreVersion }
        val generations = mutableMapOf<String, Long>()
        for (volume in mediaVolumes()) {
            ensureSyncEnabled(isPhoto)
            val generation = mediaStoreGeneration(volume)
            val selection = mediaSelection(isPhoto, checkpoint?.generations?.get(volume))
            val selectionArgs = mediaSelectionArgs(isPhoto, checkpoint?.generations?.get(volume))
            if (generation != null && checkpoint?.generations?.get(volume)?.let { generation <= it } == true) {
                generations[volume] = generation
                continue
            }
            skippedUnreadableMedia += syncVolume(
                api,
                settings,
                isPhoto,
                volume,
                selection,
                selectionArgs,
                createdFolders,
                remoteEntriesByFolder,
            )
            if (generation != null) generations[volume] = generation
        }
        if (mediaStoreVersion != null && generations.isNotEmpty()) {
            settingsStore.saveMediaStoreCheckpoint(checkpointType, mediaStoreVersion, generations)
        }
        return skippedUnreadableMedia
    }

    private suspend fun syncVolume(
        api: DriveApi,
        settings: AppSettings,
        isPhoto: Boolean,
        volume: String,
        selection: String,
        selectionArgs: Array<String>,
        createdFolders: MutableSet<String>,
        remoteEntriesByFolder: MutableMap<String, List<DriveEntry>>,
    ): Int {
        var skippedUnreadableMedia = 0
        val collection = MediaStore.Files.getContentUri(volume)
        val projection = arrayOf(
            MediaStore.MediaColumns._ID,
            MediaStore.MediaColumns.DISPLAY_NAME,
            MediaStore.MediaColumns.MIME_TYPE,
            MediaStore.MediaColumns.SIZE,
            MediaStore.MediaColumns.DATE_MODIFIED,
        )
        val cursor = applicationContext.contentResolver.query(
            collection,
            projection,
            selection,
            selectionArgs,
            "${MediaStore.MediaColumns.DATE_MODIFIED} ASC",
        ) ?: throw IOException("MediaStore did not return volume $volume")
        cursor.use {
            val idColumn = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns._ID)
            val nameColumn = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns.DISPLAY_NAME)
            val mimeColumn = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns.MIME_TYPE)
            val sizeColumn = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns.SIZE)
            val modifiedColumn = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns.DATE_MODIFIED)
            while (cursor.moveToNext()) {
                val id = cursor.getLong(idColumn)
                val displayName = cursor.getString(nameColumn).orEmpty().ifBlank { "media-$id" }
                try {
                    syncMediaItem(
                        api = api,
                        settings = settings,
                        isPhoto = isPhoto,
                        source = ContentUris.withAppendedId(collection, id),
                        id = id,
                        displayName = displayName,
                        mimeType = cursor.getString(mimeColumn).orEmpty(),
                        size = cursor.getLong(sizeColumn),
                        modified = cursor.getLong(modifiedColumn),
                        createdFolders = createdFolders,
                        remoteEntriesByFolder = remoteEntriesByFolder,
                    )
                } catch (error: IOException) {
                    if (!isUnreadableContent(error)) throw error
                    skippedUnreadableMedia++
                    Log.w(TAG, "Skipping unreadable media $displayName", error)
                }
            }
        }
        return skippedUnreadableMedia
    }

    private suspend fun syncMediaItem(
        api: DriveApi,
        settings: AppSettings,
        isPhoto: Boolean,
        source: android.net.Uri,
        id: Long,
        displayName: String,
        mimeType: String,
        size: Long,
        modified: Long,
        createdFolders: MutableSet<String>,
        remoteEntriesByFolder: MutableMap<String, List<DriveEntry>>,
    ) {
        ensureSyncEnabled(isPhoto)
        if (size > AUTO_SYNC_MAX_BYTES) {
            Log.w(TAG, "Skipping $displayName: media exceeds the 1 GiB sync limit")
            return
        }
        val mediaFolder = if (isPhoto) "Foto" else "Video"
        val folder = joinRemotePath(settings.syncFolder, mediaFolder)
        ensureFolders(api, folder, createdFolders)
        val remoteEntries = remoteEntriesByFolder[folder] ?: loadRemoteEntries(api, folder).also {
            remoteEntriesByFolder[folder] = it
        }
        val remotePath = joinRemotePath(folder, "$id-$displayName")
        val existing = remoteEntries.firstOrNull { it.path == remotePath }
        val baseSignature = "$modified:$size:$displayName"
        val storedSignature = settingsStore.getSyncSignature(source.toString())
        val remoteSignature = existing?.contentHash
            ?.takeIf { it.isNotBlank() }
            ?.let { "$baseSignature:$it" }
        if (remoteSignature != null && storedSignature == remoteSignature) return

        val contentHash = calculateContentHash(source)
        if (existing != null && existing.size == size && existing.contentHash.equals(contentHash, ignoreCase = true)) {
            settingsStore.saveSyncSignature(source.toString(), "$baseSignature:$contentHash")
            return
        }
        ensureSyncEnabled(isPhoto)
        api.uploadUri(
            applicationContext.contentResolver,
            source,
            remotePath,
            mimeType.ifBlank { if (isPhoto) "image/jpeg" else "video/mp4" },
            size,
        )
        settingsStore.saveSyncSignature(source.toString(), "$baseSignature:$contentHash")
    }

    private fun isUnreadableContent(error: IOException): Boolean =
        generateSequence<Throwable>(error) { it.cause }.any { it is UnreadableContentException }

    private fun ensureSyncEnabled(isPhoto: Boolean) {
        if (isStopped) throw CancellationException("Media sync was stopped")
        val currentSettings = settingsStore.load()
        val enabled = if (isPhoto) currentSettings.syncPhotos else currentSettings.syncVideos
        if (!currentSettings.isTelegramConfigured() || !enabled) {
            throw CancellationException("Media sync was disabled")
        }
    }

    private fun mediaVolumes(): List<String> = when {
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q ->
            MediaStore.getExternalVolumeNames(applicationContext).sorted()
        else -> listOf("external")
    }

    private fun mediaStoreVersion(): String? = when {
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q -> MediaStore.getVersion(applicationContext)
        else -> null
    }

    private fun mediaStoreGeneration(volume: String): Long? = when {
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.R -> MediaStore.getGeneration(applicationContext, volume)
        else -> null
    }

    private fun mediaSelection(isPhoto: Boolean, generation: Long?): String {
        val mediaType = MediaStore.Files.FileColumns.MEDIA_TYPE
        if (generation == null) return "$mediaType = ?"
        val modified = MediaStore.MediaColumns.GENERATION_MODIFIED
        val added = MediaStore.MediaColumns.GENERATION_ADDED
        return "$mediaType = ? AND ($modified > ? OR $added > ?)"
    }

    private fun mediaSelectionArgs(isPhoto: Boolean, generation: Long?): Array<String> {
        val type = if (isPhoto) {
            MediaStore.Files.FileColumns.MEDIA_TYPE_IMAGE
        } else {
            MediaStore.Files.FileColumns.MEDIA_TYPE_VIDEO
        }
        if (generation == null) return arrayOf(type.toString())
        return arrayOf(type.toString(), generation.toString(), generation.toString())
    }

    private suspend fun loadRemoteEntries(
        api: DriveApi,
        folder: String,
    ): List<DriveEntry> {
        return try {
            api.listEntries(folder)
        } catch (error: DriveApiException) {
            if (error.message.orEmpty().contains("directory not found", ignoreCase = true)) {
                emptyList()
            } else {
                throw error
            }
        }
    }

    private fun calculateContentHash(source: android.net.Uri): String {
        val digest = MessageDigest.getInstance("SHA-256")
        val input = try {
            applicationContext.contentResolver.openInputStream(source)
                ?: throw UnreadableContentException("Media tidak dapat dibuka")
        } catch (error: UnreadableContentException) {
            throw error
        } catch (error: IOException) {
            throw UnreadableContentException("Media tidak dapat dibuka", error)
        }
        try {
            input.use { stream ->
                val buffer = ByteArray(MEDIA_HASH_BUFFER_SIZE)
                while (true) {
                    if (isStopped) throw CancellationException("Media sync was stopped")
                    val count = try {
                        stream.read(buffer)
                    } catch (error: IOException) {
                        throw UnreadableContentException("Media tidak dapat dibaca", error)
                    }
                    if (count < 0) break
                    if (count == 0) continue
                    digest.update(buffer, 0, count)
                }
            }
        } catch (error: UnreadableContentException) {
            throw error
        } catch (error: IOException) {
            throw UnreadableContentException("Media tidak dapat dibaca", error)
        }
        return buildString {
            digest.digest().forEach { byte ->
                append("%02x".format(byte.toInt() and 0xff))
            }
        }
    }

    private suspend fun ensureFolders(
        api: DriveApi,
        folder: String,
        createdFolders: MutableSet<String>,
    ) {
        var current = ""
        for (part in folder.split('/').filter(String::isNotBlank)) {
            current = if (current.isBlank()) part else "$current/$part"
            if (!createdFolders.add(current)) continue
            try {
                api.createFolder(current)
            } catch (error: IOException) {
                if (!error.message.orEmpty().contains("exists", ignoreCase = true)) throw error
            }
        }
    }

    private fun joinRemotePath(vararg parts: String): String = parts
        .map(String::trim)
        .filter(String::isNotBlank)
        .joinToString("/") { it.trim('/') }

    private data class MediaCounts(
        val photos: Int,
        val videos: Int,
    )

    companion object {
        private const val SYNC_CHANNEL_ID = "tgdrive-sync-v1"
        private const val SYNC_ERROR_CHANNEL_ID = "tgdrive-sync-errors-v1"
        private const val SYNC_NOTIFICATION_ID = 7101
        private const val SYNC_ERROR_NOTIFICATION_ID = 7102
        private const val AUTO_SYNC_MAX_BYTES = 1L * 1024 * 1024 * 1024
        private const val MAX_SYNC_ERROR_NOTIFICATION_CHARS = 100
        private const val TRUNCATED_ERROR_SUFFIX = "… (dipotong)"
        private const val MEDIA_HASH_BUFFER_SIZE = 64 * 1024
        private const val TAG = "TgDriveSync"
    }
}

object SyncScheduler {
    private const val WORK_NAME = "tgdrive-media-sync"
    private const val RUN_NOW_WORK_NAME = "$WORK_NAME-now"

    fun schedule(context: Context) {
        val constraints = Constraints.Builder()
            .setRequiredNetworkType(NetworkType.CONNECTED)
            .build()
        val request = PeriodicWorkRequestBuilder<MediaSyncWorker>(12, TimeUnit.HOURS)
            .setConstraints(constraints)
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 5, TimeUnit.MINUTES)
            .build()
        WorkManager.getInstance(context).enqueueUniquePeriodicWork(
            WORK_NAME,
            ExistingPeriodicWorkPolicy.UPDATE,
            request,
        )
    }

    fun cancel(context: Context) {
        val workManager = WorkManager.getInstance(context)
        workManager.cancelUniqueWork(WORK_NAME)
        workManager.cancelUniqueWork(RUN_NOW_WORK_NAME)
    }

    fun runNow(context: Context) {
        val request = androidx.work.OneTimeWorkRequestBuilder<MediaSyncWorker>()
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 5, TimeUnit.MINUTES)
            .build()
        WorkManager.getInstance(context).enqueueUniqueWork(
            RUN_NOW_WORK_NAME,
            ExistingWorkPolicy.REPLACE,
            request,
        )
    }
}
