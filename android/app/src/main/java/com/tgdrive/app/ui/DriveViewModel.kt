package com.tgdrive.app.ui

import android.app.Application
import android.content.ContentResolver
import android.net.Uri
import android.provider.OpenableColumns
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.tgdrive.app.data.DriveApi
import com.tgdrive.app.data.EmbeddedRuntime
import com.tgdrive.app.data.SettingsStore
import com.tgdrive.app.data.TransferControl
import com.tgdrive.app.data.TransferNotifications
import com.tgdrive.app.model.AppSettings
import com.tgdrive.app.model.DriveEntry
import com.tgdrive.app.model.ROOT_PATH
import com.tgdrive.app.model.SortOption
import com.tgdrive.app.worker.SyncScheduler
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.Job
import kotlinx.coroutines.withContext

enum class AppSection {
    DRIVE,
    TRANSFERS,
    SYNC,
    SETTINGS,
}

private fun sortDriveEntries(entries: List<DriveEntry>, option: SortOption): List<DriveEntry> {
    val itemComparator = when (option) {
        SortOption.NAME_ASC -> compareBy<DriveEntry> { it.name.lowercase() }
        SortOption.NAME_DESC -> compareByDescending<DriveEntry> { it.name.lowercase() }
        SortOption.UPDATED_NEWEST -> compareByDescending<DriveEntry> { it.updatedAt?.toEpochMilli() ?: Long.MIN_VALUE }
        SortOption.UPDATED_OLDEST -> compareBy<DriveEntry> { it.updatedAt?.toEpochMilli() ?: Long.MIN_VALUE }
        SortOption.SIZE_LARGEST -> compareByDescending<DriveEntry> { it.size }
        SortOption.SIZE_SMALLEST -> compareBy<DriveEntry> { it.size }
    }
    return entries.sortedWith(compareByDescending<DriveEntry> { it.isDirectory }.then(itemComparator))
}

data class DriveUiState(
    val settings: AppSettings,
    val section: String = AppSection.DRIVE.name,
    val configured: Boolean = settings.isTelegramConfigured(),
    val currentPath: String = ROOT_PATH,
    val entries: List<DriveEntry> = emptyList(),
    val isLoading: Boolean = false,
    val isUploading: Boolean = false,
    val uploadFileName: String? = null,
    val uploadSentBytes: Long = 0L,
    val uploadTotalBytes: Long = 0L,
    val uploadProgress: Int? = null,
    val isUploadFinalizing: Boolean = false,
    val isUploadPaused: Boolean = false,
    val selectedPaths: Set<String> = emptySet(),
    val isDeletingSelected: Boolean = false,
    val errorMessage: String? = null,
    val notice: String? = null,
    val searchQuery: String = "",
    val isSearching: Boolean = false,
) {
    val sectionValue: AppSection
        get() = runCatching { AppSection.valueOf(section) }.getOrDefault(AppSection.DRIVE)

    val sortedEntries: List<DriveEntry>
        get() = sortDriveEntries(entries, sortOption)

    val sortOption: SortOption
        get() = runCatching { SortOption.valueOf(settings.sortOption) }.getOrDefault(SortOption.NAME_ASC)
}

class DriveViewModel(application: Application) : AndroidViewModel(application) {
    private val settingsStore = SettingsStore(application)
    private val initialSettings = settingsStore.load()
    private var api: DriveApi? = null
    private var uploadJob: Job? = null
    private var uploadControl: TransferControl? = null
    private val _state = MutableStateFlow(
        DriveUiState(
            settings = initialSettings,
            configured = initialSettings.isTelegramConfigured(),
        ),
    )
    val state: StateFlow<DriveUiState> = _state.asStateFlow()

    init {
        if (initialSettings.isTelegramConfigured()) connect()
    }

    fun toggleLayout() {
        val settings = settingsStore.load()
        settingsStore.saveDriveViewOptions(!settings.gridView, settings.sortOption)
        _state.update { it.copy(settings = settingsStore.load()) }
    }

    fun setSortOption(option: SortOption) {
        val settings = settingsStore.load()
        settingsStore.saveDriveViewOptions(settings.gridView, option.name)
        _state.update { it.copy(settings = settingsStore.load()) }
    }

    fun handleBack(): Boolean {
        val current = _state.value
        if (current.sectionValue != AppSection.DRIVE) {
            selectSection(AppSection.DRIVE.name)
            return true
        }
        if (current.isSearching) {
            clearSearch()
            return true
        }
        if (current.currentPath != ROOT_PATH) {
            goUp()
            return true
        }
        return false
    }

    fun selectSection(section: String) {
        _state.update { it.copy(section = section, errorMessage = null, notice = null) }
        if (section == AppSection.DRIVE.name && _state.value.configured) refresh()
    }

    fun connect() {
        val settings = settingsStore.load()
        if (!settings.isTelegramConfigured()) {
            showError("Lengkapi konfigurasi Telegram terlebih dahulu")
            return
        }

        viewModelScope.launch {
            setLoading(true)
            var runtimeStarted = false
            try {
                val startedApi = withContext(Dispatchers.IO) {
                    EmbeddedRuntime.start(getApplication(), settings)
                }
                runtimeStarted = true
                val entries = startedApi.listEntries(ROOT_PATH)
                api = startedApi
                _state.update {
                    it.copy(
                        configured = true,
                        currentPath = ROOT_PATH,
                        entries = entries,
                        section = AppSection.DRIVE.name,
                        errorMessage = null,
                    )
                }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                if (runtimeStarted) {
                    withContext(Dispatchers.IO) { EmbeddedRuntime.stop() }
                }
                showError("Tidak dapat terhubung. Periksa konfigurasi lalu coba lagi.")
            } finally {
                setLoading(false)
            }
        }
    }

    fun saveTelegramConfig(
        botToken: String,
        apiId: String,
        apiHash: String,
        chatId: String,
        encryptionKey: String,
    ) {
        if (botToken.isBlank() || apiId.isBlank() || apiHash.isBlank() || chatId.isBlank() || encryptionKey.isBlank()) {
            showError("Semua konfigurasi Telegram wajib diisi")
            return
        }
        settingsStore.saveTelegramConfig(botToken, apiId, apiHash, chatId, encryptionKey)
        rebuildApi()
        _state.update { it.copy(notice = "Konfigurasi disimpan", errorMessage = null) }
        connect()
    }

    fun saveCacheOptions(maxCacheMbText: String, cacheTtlHoursText: String) {
        val maxCacheMb = maxCacheMbText.trim().toIntOrNull()
        val cacheTtlHours = cacheTtlHoursText.trim().toIntOrNull()
        if (maxCacheMb == null || maxCacheMb < 50 || maxCacheMb > 64_000) {
            showError("Batas cache harus 50–64000 MB")
            return
        }
        if (cacheTtlHours == null || cacheTtlHours < 0 || cacheTtlHours > 8_760) {
            showError("Masa cache harus 0–8760 jam; 0 berarti tidak kedaluwarsa")
            return
        }
        settingsStore.saveCacheOptions(maxCacheMb, cacheTtlHours)
        rebuildApi()
        _state.update { it.copy(notice = "Pengaturan cache disimpan", errorMessage = null) }
        connect()
    }

    fun saveSyncOptions(syncPhotos: Boolean, syncVideos: Boolean, syncFolder: String) {
        val folder = syncFolder.trim().trim('/').ifBlank { "Phone Backup" }
        settingsStore.saveSyncOptions(syncPhotos, syncVideos, folder)
        _state.update {
            it.copy(
                settings = settingsStore.load(),
                notice = "Pengaturan sinkronisasi disimpan",
                errorMessage = null,
            )
        }
        val context = getApplication<Application>()
        SyncScheduler.cancel(context)
        if (syncPhotos || syncVideos) {
            SyncScheduler.schedule(context)
            SyncScheduler.runNow(context)
        }
    }

    fun runSyncNow() {
        SyncScheduler.runNow(getApplication())
        _state.update { it.copy(notice = "Sinkronisasi dijadwalkan", errorMessage = null) }
    }

    fun refresh() {
        val currentApi = api ?: return
        viewModelScope.launch {
            setLoading(true)
            try {
                val query = _state.value.searchQuery.trim()
                val entries = if (query.isBlank()) {
                    currentApi.listEntries(_state.value.currentPath)
                } else {
                    currentApi.search(query)
                }
                _state.update { current ->
                    val visiblePaths = entries.map { it.path }.toSet()
                    current.copy(
                        entries = entries,
                        selectedPaths = current.selectedPaths.intersect(visiblePaths),
                        errorMessage = null,
                    )
                }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                showError(error.message ?: "Gagal memuat drive")
            } finally {
                setLoading(false)
            }
        }
    }

    fun openDirectory(path: String) {
        _state.update { it.copy(currentPath = path, searchQuery = "", isSearching = false) }
        refresh()
    }

    fun goUp() {
        val currentPath = _state.value.currentPath
        if (currentPath == ROOT_PATH) return
        val parent = currentPath.substringBeforeLast('/', missingDelimiterValue = "")
            .ifBlank { ROOT_PATH }
        openDirectory(parent)
    }

    fun startSearch() {
        _state.update { it.copy(isSearching = true) }
    }

    fun updateSearchQuery(query: String) {
        _state.update { it.copy(searchQuery = query) }
        if (query.isBlank()) {
            _state.update { it.copy(isSearching = false) }
            refresh()
        }
    }

    fun submitSearch() {
        refresh()
    }

    fun clearSearch() {
        _state.update { it.copy(searchQuery = "", isSearching = false) }
        refresh()
    }

    fun toggleSelection(path: String) {
        _state.update { current ->
            val selected = current.selectedPaths.toMutableSet()
            if (!selected.add(path)) selected.remove(path)
            current.copy(selectedPaths = selected)
        }
    }

    fun selectAllVisible() {
        _state.update { current ->
            current.copy(selectedPaths = current.entries.mapTo(linkedSetOf()) { it.path })
        }
    }

    fun clearSelection() {
        _state.update { it.copy(selectedPaths = emptySet()) }
    }

    fun deleteSelected() {
        val currentApi = api ?: return
        val selectedEntries = _state.value.entries.filter { it.path in _state.value.selectedPaths }
        if (selectedEntries.isEmpty()) {
            clearSelection()
            return
        }
        viewModelScope.launch {
            _state.update { it.copy(isDeletingSelected = true, errorMessage = null, notice = null) }
            var deletedCount = 0
            val failures = mutableListOf<String>()
            try {
                for (entry in selectedEntries) {
                    try {
                        if (entry.isDirectory) currentApi.deleteFolder(entry.path) else currentApi.deleteFile(entry.path)
                        deletedCount++
                    } catch (error: Exception) {
                        if (error is CancellationException) throw error
                        failures += entry.name
                    }
                }
                _state.update { it.copy(selectedPaths = emptySet()) }
                if (failures.isEmpty()) {
                    _state.update { it.copy(notice = "$deletedCount item dihapus", errorMessage = null) }
                } else {
                    showError("$deletedCount terhapus; gagal: ${failures.take(3).joinToString()}" )
                }
                refresh()
            } finally {
                _state.update { it.copy(isDeletingSelected = false) }
            }
        }
    }

    fun createFolder(name: String) {
        val cleanName = name.trim()
        if (cleanName.isBlank() || cleanName.contains('/')) {
            showError("Nama folder tidak valid")
            return
        }
        val currentApi = api ?: return
        val folderPath = joinPath(_state.value.currentPath, cleanName)
        viewModelScope.launch {
            setLoading(true)
            try {
                currentApi.createFolder(folderPath)
                _state.update { it.copy(notice = "Folder dibuat", errorMessage = null) }
                refresh()
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                showError(error.message ?: "Gagal membuat folder")
            } finally {
                setLoading(false)
            }
        }
    }

    fun rename(entry: DriveEntry, newName: String) {
        val cleanName = newName.trim()
        if (cleanName.isBlank() || cleanName.contains('/')) {
            showError("Nama tidak valid")
            return
        }
        val currentApi = api ?: return
        val target = joinPath(parentPath(entry.path), cleanName)
        viewModelScope.launch {
            setLoading(true)
            try {
                currentApi.rename(entry.path, target)
                _state.update { it.copy(notice = "Nama diubah", errorMessage = null) }
                refresh()
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                showError(error.message ?: "Gagal mengubah nama")
            } finally {
                setLoading(false)
            }
        }
    }

    fun delete(entry: DriveEntry) {
        val currentApi = api ?: return
        viewModelScope.launch {
            setLoading(true)
            try {
                if (entry.isDirectory) currentApi.deleteFolder(entry.path) else currentApi.deleteFile(entry.path)
                _state.update { it.copy(notice = "Dihapus", errorMessage = null) }
                refresh()
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                showError(error.message ?: "Gagal menghapus")
            } finally {
                setLoading(false)
            }
        }
    }

    fun upload(contentResolver: ContentResolver, uri: Uri) {
        if (_state.value.isUploading) {
            showError("Upload lain masih berjalan")
            return
        }
        val currentApi = api ?: return
        val metadata = readMetadata(contentResolver, uri)
        if (metadata.size >= 0 && encryptedUploadSize(metadata.size) > MAX_TELEGRAM_FILE_BYTES) {
            showError("File terlalu besar setelah dienkripsi; Telegram membatasi upload hingga 2 GiB")
            return
        }
        val fileName = metadata.name.ifBlank { "upload-${System.currentTimeMillis()}" }
        val remotePath = joinPath(_state.value.currentPath, fileName)
        uploadJob = viewModelScope.launch {
            val appContext = getApplication<Application>()
            val control = TransferControl()
            uploadControl = control
            TransferNotifications.prepare(appContext)
            _state.update {
                it.copy(
                    isUploading = true,
                    uploadFileName = fileName,
                    uploadSentBytes = 0L,
                    uploadTotalBytes = 0L,
                    uploadProgress = null,
                    isUploadFinalizing = false,
                    isUploadPaused = false,
                    errorMessage = null,
                    notice = null,
                )
            }
            TransferNotifications.uploadProgress(appContext, fileName, 0L, 0L)
            var uploadSucceeded = false
            try {
                currentApi.uploadUri(
                    contentResolver = contentResolver,
                    source = uri,
                    remotePath = remotePath,
                    mimeType = metadata.mimeType,
                    size = metadata.size,
                    control = control,
                ) { sentBytes, totalBytes ->
                    val hasKnownTotal = totalBytes > 0L
                    val telegramUploadComplete = hasKnownTotal && sentBytes >= totalBytes
                    val progress = if (hasKnownTotal) {
                        ((sentBytes * 100L) / totalBytes)
                            .coerceIn(0L, 100L)
                            .toInt()
                    } else {
                        null
                    }
                    _state.update {
                        it.copy(
                            uploadSentBytes = sentBytes,
                            uploadTotalBytes = totalBytes.coerceAtLeast(0L),
                            uploadProgress = progress,
                            isUploadFinalizing = telegramUploadComplete,
                        )
                    }
                    TransferNotifications.uploadProgress(
                        appContext,
                        fileName,
                        sentBytes,
                        totalBytes,
                        finalizing = telegramUploadComplete,
                    )
                }
                uploadSucceeded = true
                TransferNotifications.uploadFinished(appContext, fileName)
                _state.update { it.copy(notice = "File diupload", errorMessage = null) }
                refresh()
            } catch (error: CancellationException) {
                TransferNotifications.uploadCanceled(appContext, fileName)
                _state.update { it.copy(notice = "Upload dibatalkan", errorMessage = null) }
            } catch (error: Exception) {
                TransferNotifications.uploadFailed(appContext, fileName)
                showError(error.message ?: "Upload gagal")
            } finally {
                uploadControl = null
                uploadJob = null
                _state.update {
                    it.copy(
                        isUploading = false,
                        isUploadFinalizing = false,
                        isUploadPaused = false,
                        uploadProgress = if (uploadSucceeded) 100 else it.uploadProgress,
                    )
                }
            }
        }
    }

    fun pauseOrResumeUpload() {
        val control = uploadControl ?: return
        val currentApi = api ?: return
        if (_state.value.isUploadPaused) {
            control.resume()
            _state.update { it.copy(isUploadPaused = false) }
            viewModelScope.launch { runCatching { currentApi.resumeUpload() } }
            TransferNotifications.uploadProgress(
                getApplication(),
                _state.value.uploadFileName.orEmpty(),
                _state.value.uploadSentBytes,
                _state.value.uploadTotalBytes,
                finalizing = false,
            )
        } else {
            control.pause()
            _state.update { it.copy(isUploadPaused = true) }
            viewModelScope.launch { runCatching { currentApi.pauseUpload() } }
            TransferNotifications.uploadPaused(
                getApplication(),
                _state.value.uploadFileName.orEmpty(),
                _state.value.uploadSentBytes,
                _state.value.uploadTotalBytes,
            )
        }
    }

    fun cancelUpload() {
        val currentApi = api ?: return
        val runningJob = uploadJob
        uploadControl?.cancel()
        viewModelScope.launch {
            runCatching { currentApi.cancelUpload() }
            runningJob?.cancel()
        }
    }

    fun clearCache() {
        val currentApi = api ?: return
        viewModelScope.launch {
            setLoading(true)
            try {
                currentApi.clearCache()
                getApplication<Application>().cacheDir.listFiles()?.forEach { cacheFile ->
                    cacheFile.deleteRecursively()
                }
                _state.update { it.copy(notice = "Cache dibersihkan", errorMessage = null) }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                showError(error.message ?: "Gagal membersihkan cache")
            } finally {
                setLoading(false)
            }
        }
    }

    fun contentUrl(path: String, download: Boolean = false): String? = api?.contentUrl(path, download)
    fun mediaContentUrl(path: String): String? = api?.mediaContentUrl(path)
    fun thumbnailUrl(path: String): String? = api?.thumbnailUrl(path)
    fun accessToken(): String = api?.accessToken().orEmpty()
    fun serverUrl(): String = api?.baseUrl().orEmpty()

    fun clearNotice() {
        _state.update { it.copy(notice = null) }
    }

    fun clearError() {
        _state.update { it.copy(errorMessage = null) }
    }

    override fun onCleared() {
        // WorkManager may share this process. Stopping Go here kills background sync
        // when the activity is closed.
        super.onCleared()
    }

    private fun rebuildApi() {
        EmbeddedRuntime.stop()
        api = null
        val settings = settingsStore.load()
        _state.update {
            it.copy(
                settings = settings,
                configured = settings.isTelegramConfigured(),
            )
        }
    }

    private fun setLoading(loading: Boolean) {
        _state.update { it.copy(isLoading = loading) }
    }

    private fun showError(message: String) {
        _state.update { it.copy(errorMessage = message, notice = null) }
    }

    private fun readMetadata(contentResolver: ContentResolver, uri: Uri): FileMetadata {
        var name = ""
        var size = -1L
        contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)
            ?.use { cursor ->
                if (cursor.moveToFirst()) {
                    val nameColumn = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    val sizeColumn = cursor.getColumnIndex(OpenableColumns.SIZE)
                    if (nameColumn >= 0) name = cursor.getString(nameColumn).orEmpty()
                    if (sizeColumn >= 0 && !cursor.isNull(sizeColumn)) size = cursor.getLong(sizeColumn)
                }
            }
        val mimeType = contentResolver.getType(uri) ?: "application/octet-stream"
        return FileMetadata(name, size, mimeType)
    }

    private data class FileMetadata(
        val name: String,
        val size: Long,
        val mimeType: String,
    )

    private companion object {
        private const val MAX_TELEGRAM_FILE_BYTES = 2L * 1024 * 1024 * 1024
        private const val ENCRYPTION_HEADER_BYTES = 9L
        private const val ENCRYPTION_CHUNK_BYTES = 1024L * 1024
        private const val ENCRYPTION_CHUNK_OVERHEAD_BYTES = 32L

        fun encryptedUploadSize(size: Long): Long {
            if (size == 0L) return ENCRYPTION_HEADER_BYTES
            val chunks = (size + ENCRYPTION_CHUNK_BYTES - 1) / ENCRYPTION_CHUNK_BYTES
            return ENCRYPTION_HEADER_BYTES + size + chunks * ENCRYPTION_CHUNK_OVERHEAD_BYTES
        }

        fun joinPath(parent: String, name: String): String {
            if (parent == ROOT_PATH || parent.isBlank()) return name
            return "$parent/$name"
        }

        fun parentPath(filePath: String): String = filePath.substringBeforeLast('/', missingDelimiterValue = "")
            .ifBlank { ROOT_PATH }
    }
}
