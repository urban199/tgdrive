package com.tgdrive.app.model

import java.text.DecimalFormat
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlin.math.abs

const val ROOT_PATH = "/"

data class DriveEntry(
    val name: String,
    val path: String,
    val isDirectory: Boolean,
    val size: Long,
    val contentHash: String = "",
    val updatedAt: Instant?,
    val mimeType: String,
    val hasThumbnail: Boolean,
)

data class AppStats(
    val fileCount: Int,
    val totalBytes: Long,
)

data class AppSettings(
    val botToken: String = "",
    val apiId: String = "",
    val apiHash: String = "",
    val chatId: String = "",
    val encryptionKey: String = "",
    val syncPhotos: Boolean = false,
    val syncVideos: Boolean = false,
    val syncFolder: String = "Phone Backup",
    val gridView: Boolean = false,
    val sortOption: String = "NAME_ASC",
    val maxCacheMb: Int = 500,
    val cacheTtlHours: Int = 168,
) {
    fun isTelegramConfigured(): Boolean =
        botToken.isNotBlank() &&
            apiId.isNotBlank() &&
            apiHash.isNotBlank() &&
            chatId.isNotBlank() &&
            encryptionKey.isNotBlank()
}

enum class SortOption(val label: String) {
    NAME_ASC("Nama A–Z"),
    NAME_DESC("Nama Z–A"),
    UPDATED_NEWEST("Terbaru"),
    UPDATED_OLDEST("Terlama"),
    SIZE_LARGEST("Ukuran terbesar"),
    SIZE_SMALLEST("Ukuran terkecil"),
}

enum class FileCategory {
    FOLDER,
    IMAGE,
    VIDEO,
    AUDIO,
    PDF,
    TEXT,
    ARCHIVE,
    OTHER,
}

private val dateFormatter = DateTimeFormatter.ofPattern("d MMM yyyy")
private val sizeFormatter = DecimalFormat("#,##0.0")

fun Long.readableFileSize(): String {
    if (this < 1024) return "$this B"
    val units = listOf("KB", "MB", "GB", "TB")
    var current = this.toDouble()
    var unitIndex = -1
    while (current >= 1024 && unitIndex < units.lastIndex) {
        current /= 1024
        unitIndex++
    }
    return "${sizeFormatter.format(current)} ${units[unitIndex]}"
}

fun Instant?.readableDate(): String {
    val instant = this ?: return "—"
    if (abs(instant.toEpochMilli()) < 1000) return "—"
    return dateFormatter.format(instant.atZone(ZoneId.systemDefault()))
}

fun DriveEntry.isImage(): Boolean = mimeType.startsWith("image/") ||
    name.hasExtension("jpg", "jpeg", "png", "gif", "webp", "heic", "heif", "bmp")

fun DriveEntry.isVideo(): Boolean = mimeType.startsWith("video/") ||
    name.hasExtension("mp4", "mkv", "webm", "mov", "avi", "m4v", "3gp", "ts")

fun DriveEntry.isAudio(): Boolean = mimeType.startsWith("audio/") ||
    name.endsWith(".mp3", ignoreCase = true) ||
    name.endsWith(".flac", ignoreCase = true) ||
    name.endsWith(".ogg", ignoreCase = true) ||
    name.endsWith(".m4a", ignoreCase = true) ||
    name.endsWith(".wav", ignoreCase = true) ||
    name.endsWith(".aac", ignoreCase = true)

fun DriveEntry.isPdf(): Boolean =
    mimeType == "application/pdf" || name.endsWith(".pdf", ignoreCase = true)

fun DriveEntry.isText(): Boolean =
    mimeType.startsWith("text/") ||
        name.endsWith(".md", ignoreCase = true) ||
        name.endsWith(".txt", ignoreCase = true) ||
        name.endsWith(".log", ignoreCase = true)

private fun String.hasExtension(vararg extensions: String): Boolean =
    extensions.any { extension -> endsWith(".$extension", ignoreCase = true) }

fun DriveEntry.isArchive(): Boolean =
    mimeType in setOf(
        "application/zip",
        "application/x-tar",
        "application/x-rar-compressed",
        "application/x-7z-compressed",
        "application/gzip",
        "application/x-bzip2",
    ) ||
        name.endsWith(".zip", ignoreCase = true) ||
        name.endsWith(".tar", ignoreCase = true) ||
        name.endsWith(".rar", ignoreCase = true) ||
        name.endsWith(".7z", ignoreCase = true) ||
        name.endsWith(".gz", ignoreCase = true)

fun DriveEntry.fileCategory(): FileCategory = when {
    isDirectory -> FileCategory.FOLDER
    isImage() -> FileCategory.IMAGE
    isVideo() -> FileCategory.VIDEO
    isAudio() -> FileCategory.AUDIO
    isPdf() -> FileCategory.PDF
    isText() -> FileCategory.TEXT
    isArchive() -> FileCategory.ARCHIVE
    else -> FileCategory.OTHER
}

fun DriveEntry.canPreview(): Boolean = isImage() || isVideo() || isAudio() || isPdf()
