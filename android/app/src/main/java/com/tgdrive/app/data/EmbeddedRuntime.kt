package com.tgdrive.app.data

import android.content.Context
import com.tgdrive.app.model.AppSettings
import com.tgdrive.bridge.Bridge
import org.json.JSONObject

object EmbeddedRuntime {
    fun start(context: Context, settings: AppSettings): DriveApi {
        val apiId = settings.apiId.toIntOrNull()
            ?: throw IllegalArgumentException("API ID harus berupa angka")
        val chatId = settings.chatId.toLongOrNull()
            ?: throw IllegalArgumentException("Chat ID harus berupa angka")
        val config = JSONObject()
            .put("bot_token", settings.botToken)
            .put("api_id", apiId)
            .put("api_hash", settings.apiHash)
            .put("chat_id", chatId)
            .put("encryption_key", settings.encryptionKey)
            .put("max_cache_mb", settings.maxCacheMb)
            .put("cache_ttl_hours", settings.cacheTtlHours)
            .toString()
        val dataDir = context.getDir("tgdrive", Context.MODE_PRIVATE).absolutePath
        val port = Bridge.start(config, dataDir)
        if (port <= 0) throw IllegalStateException("Layanan penyimpanan tidak dapat dimulai")
        return DriveApi("http://127.0.0.1:$port", "")
    }

    fun stop() {
        runCatching { Bridge.stop() }
    }
}
