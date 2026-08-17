package com.tgdrive.app.data

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

class TransferActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val downloadId = intent.getLongExtra(EXTRA_DOWNLOAD_ID, -1L)
        if (downloadId < 0L) return
        DownloadTransfers.handleAction(
            context = context.applicationContext,
            action = intent.action.orEmpty(),
            downloadId = downloadId,
        )
    }

    companion object {
        const val ACTION_PAUSE = "com.tgdrive.app.action.PAUSE_DOWNLOAD"
        const val ACTION_RESUME = "com.tgdrive.app.action.RESUME_DOWNLOAD"
        const val ACTION_CANCEL = "com.tgdrive.app.action.CANCEL_DOWNLOAD"
        const val EXTRA_DOWNLOAD_ID = "download_id"
    }
}
