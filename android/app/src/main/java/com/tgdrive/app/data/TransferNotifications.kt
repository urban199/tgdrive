package com.tgdrive.app.data

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build

object TransferNotifications {
    private const val CHANNEL_ID = "tgdrive-transfers-v2"
    private const val CHANNEL_NAME = "Transfer TgDrive"

    fun prepare(context: Context) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val manager = context.getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_ID,
                CHANNEL_NAME,
                NotificationManager.IMPORTANCE_DEFAULT,
            ).apply {
                description = "Progress upload dan download TgDrive"
                setShowBadge(false)
            },
        )
    }

    fun uploadProgress(
        context: Context,
        fileName: String,
        sentBytes: Long,
        totalBytes: Long,
        finalizing: Boolean = false,
    ) {
        val progress = percentage(sentBytes, totalBytes)
        val text = when {
            finalizing && progress != null -> "$progress% · Menyelesaikan upload"
            progress == null -> "Mengirim ke Telegram…"
            else -> "$progress% · ${formatBytes(sentBytes)}"
        }
        notify(context, uploadId(fileName), buildProgressNotification(
            context = context,
            title = "Mengunggah $fileName",
            text = text,
            progress = progress,
            icon = android.R.drawable.stat_sys_upload,
        ))
    }

    fun uploadPaused(context: Context, fileName: String, sentBytes: Long, totalBytes: Long) {
        val progress = percentage(sentBytes, totalBytes)
        notify(context, uploadId(fileName), buildProgressNotification(
            context = context,
            title = "Upload dijeda",
            text = if (progress == null) "Menunggu dilanjutkan" else "$progress% · ${formatBytes(sentBytes)}",
            progress = progress,
            icon = android.R.drawable.stat_sys_upload,
            ongoing = false,
        ))
    }

    fun uploadFinished(context: Context, fileName: String) {
        notifyFinished(context, uploadId(fileName), buildFinishedNotification(
            context,
            "Upload selesai",
            fileName,
            android.R.drawable.stat_sys_upload_done,
        ))
    }

    fun uploadCanceled(context: Context, fileName: String) {
        notifyFinished(context, uploadId(fileName), buildFinishedNotification(
            context,
            "Upload dibatalkan",
            fileName,
            android.R.drawable.stat_notify_error,
        ))
    }

    fun uploadFailed(context: Context, fileName: String) {
        notifyFinished(context, uploadId(fileName), buildFinishedNotification(
            context,
            "Upload gagal",
            fileName,
            android.R.drawable.stat_notify_error,
        ))
    }

    fun downloadProgress(
        context: Context,
        downloadId: Long,
        fileName: String,
        downloaded: Long,
        total: Long,
        paused: Boolean = false,
    ) {
        val progress = percentage(downloaded, total)
        val actions = listOf(
            action(
                context,
                if (paused) TransferActionReceiver.ACTION_RESUME else TransferActionReceiver.ACTION_PAUSE,
                downloadId,
                if (paused) "Lanjutkan" else "Jeda",
            ),
            action(context, TransferActionReceiver.ACTION_CANCEL, downloadId, "Batal"),
        )
        notify(context, downloadId(downloadId), buildProgressNotification(
            context = context,
            title = if (paused) "Download dijeda" else "Mengunduh $fileName",
            text = if (paused) {
                if (progress == null) "Menunggu dilanjutkan" else "$progress% · ${formatBytes(downloaded)}"
            } else if (progress == null) {
                "Menerima data…"
            } else {
                "$progress% · ${formatBytes(downloaded)}"
            },
            progress = progress,
            icon = android.R.drawable.stat_sys_download,
            ongoing = !paused,
            actions = actions,
        ))
    }

    fun downloadFinished(context: Context, downloadId: Long, fileName: String) {
        notifyFinished(context, downloadId(downloadId), buildFinishedNotification(
            context,
            "Download selesai",
            fileName,
            android.R.drawable.stat_sys_download_done,
        ))
    }

    fun downloadCanceled(context: Context, downloadId: Long, fileName: String) {
        notifyFinished(context, downloadId(downloadId), buildFinishedNotification(
            context,
            "Download dibatalkan",
            fileName,
            android.R.drawable.stat_notify_error,
        ))
    }

    fun downloadFailed(context: Context, downloadId: Long, fileName: String) {
        notifyFinished(context, downloadId(downloadId), buildFinishedNotification(
            context,
            "Download gagal",
            fileName,
            android.R.drawable.stat_notify_error,
        ))
    }

    private fun buildProgressNotification(
        context: Context,
        title: String,
        text: String,
        progress: Int?,
        icon: Int,
        ongoing: Boolean = true,
        actions: List<Notification.Action> = emptyList(),
    ): Notification {
        return Notification.Builder(context, CHANNEL_ID)
            .setSmallIcon(icon)
            .setContentTitle(title)
            .setContentText(text)
            .setOnlyAlertOnce(true)
            .setOngoing(ongoing)
            .setProgress(100, progress ?: 0, progress == null)
            .apply { actions.forEach(::addAction) }
            .build()
    }

    private fun buildFinishedNotification(
        context: Context,
        title: String,
        fileName: String,
        icon: Int,
    ): Notification {
        return Notification.Builder(context, CHANNEL_ID)
            .setSmallIcon(icon)
            .setContentTitle(title)
            .setContentText(fileName)
            .setOnlyAlertOnce(false)
            .setOngoing(false)
            .setProgress(0, 0, false)
            .setAutoCancel(true)
            .setTimeoutAfter(8_000)
            .build()
    }

    private fun action(
        context: Context,
        action: String,
        downloadId: Long,
        title: String,
    ): Notification.Action {
        val intent = Intent(context, TransferActionReceiver::class.java)
            .setAction(action)
            .putExtra(TransferActionReceiver.EXTRA_DOWNLOAD_ID, downloadId)
        val pendingIntent = PendingIntent.getBroadcast(
            context,
            (downloadId xor action.hashCode().toLong()).toInt(),
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val icon = when (action) {
            TransferActionReceiver.ACTION_PAUSE -> android.R.drawable.ic_media_pause
            TransferActionReceiver.ACTION_RESUME -> android.R.drawable.ic_media_play
            else -> android.R.drawable.ic_menu_close_clear_cancel
        }
        return Notification.Action.Builder(icon, title, pendingIntent).build()
    }

    private fun notify(context: Context, id: Int, notification: Notification) {
        prepare(context)
        try {
            context.getSystemService(NotificationManager::class.java).notify(id, notification)
        } catch (_: SecurityException) {
            // Android 13+ requires POST_NOTIFICATIONS. The transfer itself still continues.
        }
    }

    private fun notifyFinished(context: Context, id: Int, notification: Notification) {
        prepare(context)
        try {
            val manager = context.getSystemService(NotificationManager::class.java)
            manager.cancel(id)
            manager.notify(id, notification)
        } catch (_: SecurityException) {
            // Android 13+ requires POST_NOTIFICATIONS. The transfer itself still continues.
        }
    }

    private fun uploadId(fileName: String): Int = ("upload:$fileName").hashCode() and Int.MAX_VALUE

    private fun downloadId(downloadId: Long): Int = ("download:$downloadId").hashCode() and Int.MAX_VALUE

    private fun percentage(current: Long, total: Long): Int? {
        if (total <= 0L) return null
        return ((current.coerceAtLeast(0L) * 100L) / total).coerceIn(0L, 100L).toInt()
    }

    private fun formatBytes(bytes: Long): String {
        if (bytes < 1024) return "$bytes B"
        val units = listOf("KB", "MB", "GB", "TB")
        var value = bytes.toDouble()
        var index = -1
        while (value >= 1024 && index < units.lastIndex) {
            value /= 1024
            index++
        }
        return "%.1f %s".format(value, units[index])
    }
}
