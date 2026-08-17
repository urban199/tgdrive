package com.tgdrive.app.data

import android.content.Context
import com.tgdrive.app.model.AppSettings
import org.json.JSONObject

data class MediaStoreCheckpoint(
    val version: String,
    val generations: Map<String, Long>,
)

class SettingsStore(context: Context) {
    private val preferences = context.getSharedPreferences("tgdrive-settings", Context.MODE_PRIVATE)

    fun load(): AppSettings = AppSettings(
        botToken = preferences.getString(KEY_BOT_TOKEN, "").orEmpty(),
        apiId = preferences.getString(KEY_API_ID, "").orEmpty(),
        apiHash = preferences.getString(KEY_API_HASH, "").orEmpty(),
        chatId = preferences.getString(KEY_CHAT_ID, "").orEmpty(),
        encryptionKey = preferences.getString(KEY_ENCRYPTION_KEY, "").orEmpty(),
        syncPhotos = preferences.getBoolean(KEY_SYNC_PHOTOS, false),
        syncVideos = preferences.getBoolean(KEY_SYNC_VIDEOS, false),
        syncFolder = preferences.getString(KEY_SYNC_FOLDER, "Phone Backup").orEmpty(),
        gridView = preferences.getBoolean(KEY_GRID_VIEW, false),
        sortOption = preferences.getString(KEY_SORT_OPTION, "NAME_ASC").orEmpty(),
        maxCacheMb = preferences.getInt(KEY_MAX_CACHE_MB, 500),
        cacheTtlHours = preferences.getInt(KEY_CACHE_TTL_HOURS, 168),
    )

    fun saveTelegramConfig(
        botToken: String,
        apiId: String,
        apiHash: String,
        chatId: String,
        encryptionKey: String,
    ) {
        preferences.edit()
            .putString(KEY_BOT_TOKEN, botToken.trim())
            .putString(KEY_API_ID, apiId.trim())
            .putString(KEY_API_HASH, apiHash.trim())
            .putString(KEY_CHAT_ID, chatId.trim())
            .putString(KEY_ENCRYPTION_KEY, encryptionKey.trim())
            .apply()
    }

    fun saveSyncOptions(syncPhotos: Boolean, syncVideos: Boolean, syncFolder: String) {
        preferences.edit()
            .putBoolean(KEY_SYNC_PHOTOS, syncPhotos)
            .putBoolean(KEY_SYNC_VIDEOS, syncVideos)
            .putString(KEY_SYNC_FOLDER, syncFolder.trim().trim('/'))
            .apply()
    }

    fun saveCacheOptions(maxCacheMb: Int, cacheTtlHours: Int) {
        preferences.edit()
            .putInt(KEY_MAX_CACHE_MB, maxCacheMb)
            .putInt(KEY_CACHE_TTL_HOURS, cacheTtlHours)
            .apply()
    }

    fun saveDriveViewOptions(gridView: Boolean, sortOption: String) {
        preferences.edit()
            .putBoolean(KEY_GRID_VIEW, gridView)
            .putString(KEY_SORT_OPTION, sortOption)
            .apply()
    }

    fun getSyncSignature(uri: String): String? = preferences.getString(syncKey(uri), null)

    fun saveSyncSignature(uri: String, signature: String) {
        preferences.edit().putString(syncKey(uri), signature).apply()
    }

    fun saveLastSync(timestamp: Long) {
        preferences.edit().putLong(KEY_LAST_SYNC, timestamp).apply()
    }

    fun lastSync(): Long = preferences.getLong(KEY_LAST_SYNC, 0L)

    fun getMediaStoreCheckpoint(mediaType: String): MediaStoreCheckpoint? {
        val value = preferences.getString(mediaStoreCheckpointKey(mediaType), null) ?: return null
        return runCatching {
            val document = JSONObject(value)
            val generations = document.optJSONObject("generations") ?: JSONObject()
            val values = buildMap {
                val names = generations.keys()
                while (names.hasNext()) {
                    val volume = names.next()
                    put(volume, generations.getLong(volume))
                }
            }
            MediaStoreCheckpoint(document.getString("version"), values)
        }.getOrNull()
    }

    fun saveMediaStoreCheckpoint(mediaType: String, version: String, generations: Map<String, Long>) {
        val values = JSONObject()
        generations.forEach { (volume, generation) -> values.put(volume, generation) }
        val document = JSONObject()
            .put("version", version)
            .put("generations", values)
        preferences.edit().putString(mediaStoreCheckpointKey(mediaType), document.toString()).apply()
    }

    private fun syncKey(uri: String): String = "sync:$uri"

    private fun mediaStoreCheckpointKey(mediaType: String): String = "media-store:$mediaType"

    private companion object {
        const val KEY_BOT_TOKEN = "bot_token"
        const val KEY_API_ID = "api_id"
        const val KEY_API_HASH = "api_hash"
        const val KEY_CHAT_ID = "chat_id"
        const val KEY_ENCRYPTION_KEY = "encryption_key"
        const val KEY_SYNC_PHOTOS = "sync_photos"
        const val KEY_SYNC_VIDEOS = "sync_videos"
        const val KEY_SYNC_FOLDER = "sync_folder"
        const val KEY_GRID_VIEW = "grid_view"
        const val KEY_SORT_OPTION = "sort_option"
        const val KEY_MAX_CACHE_MB = "max_cache_mb"
        const val KEY_CACHE_TTL_HOURS = "cache_ttl_hours"
        const val KEY_LAST_SYNC = "last_sync"
    }
}
