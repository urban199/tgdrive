@file:OptIn(
    androidx.compose.material3.ExperimentalMaterial3Api::class,
    androidx.compose.foundation.ExperimentalFoundationApi::class,
)

package com.tgdrive.app

import android.content.Intent
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.OnBackPressedCallback
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items as gridItems
import androidx.compose.foundation.lazy.items as columnItems
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.produceState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ArrowBack
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.CloudDone
import androidx.compose.material.icons.outlined.CloudDownload
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.CreateNewFolder
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.DriveFileRenameOutline
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.GridView
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.InsertDriveFile
import androidx.compose.material.icons.outlined.List
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Movie
import androidx.compose.material.icons.outlined.PictureAsPdf
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Sort
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Divider
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.text.KeyboardOptions
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil.compose.AsyncImage
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.media.MediaMetadataRetriever
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import com.tgdrive.app.model.DriveEntry
import com.tgdrive.app.model.FileCategory
import com.tgdrive.app.model.ROOT_PATH
import com.tgdrive.app.model.SortOption
import com.tgdrive.app.model.fileCategory
import com.tgdrive.app.model.isAudio
import com.tgdrive.app.model.isImage
import com.tgdrive.app.model.isPdf
import com.tgdrive.app.model.isText
import com.tgdrive.app.model.isVideo
import com.tgdrive.app.model.readableDate
import com.tgdrive.app.model.readableFileSize
import com.tgdrive.app.data.DownloadStatus
import com.tgdrive.app.data.DownloadTransfer
import com.tgdrive.app.data.DownloadTransfers
import com.tgdrive.app.ui.AppSection
import com.tgdrive.app.ui.DriveUiState
import com.tgdrive.app.ui.DriveViewModel
import java.io.File
import java.io.FileOutputStream
import java.security.MessageDigest

private val TgDriveColors = darkColorScheme(
    primary = Color(0xFFE2A35B),
    onPrimary = Color(0xFF20150A),
    primaryContainer = Color(0xFF4A321C),
    onPrimaryContainer = Color(0xFFFFE4BF),
    secondary = Color(0xFF83B9A4),
    background = Color(0xFF0F1110),
    onBackground = Color(0xFFF1F0E9),
    surface = Color(0xFF171A18),
    onSurface = Color(0xFFF1F0E9),
    surfaceVariant = Color(0xFF252A26),
    onSurfaceVariant = Color(0xFF9EA79F),
    outline = Color(0xFF3B443E),
    error = Color(0xFFFF7A70),
)

class MainActivity : ComponentActivity() {
    private val viewModel: DriveViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (!viewModel.handleBack()) finish()
            }
        })
        setContent { TgDriveApp(viewModel) }
    }

    fun enqueueDownload(entry: DriveEntry, url: String?, token: String) {
        if (url.isNullOrBlank()) return
        DownloadTransfers.start(this, url, token, entry.name, entry.mimeType)
    }
}

@Composable
private fun TgDriveApp(viewModel: DriveViewModel) {
    androidx.compose.material3.MaterialTheme(colorScheme = TgDriveColors) {
        DriveRoot(viewModel)
    }
}

@Composable
private fun DriveRoot(viewModel: DriveViewModel) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val downloads by DownloadTransfers.transfers.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val snackbarHostState = remember { SnackbarHostState() }
    val coroutineScope = rememberCoroutineScope()
    var pendingPermissionAction by remember { mutableStateOf<(() -> Unit)?>(null) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri != null) viewModel.upload(context.contentResolver, uri)
    }
    val permissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions(),
    ) { results ->
        val action = pendingPermissionAction
        pendingPermissionAction = null
        if (results.values.all { it }) {
            action?.invoke()
        } else {
            coroutineScope.launch {
                snackbarHostState.showSnackbar("Izin galeri diperlukan untuk sinkronisasi")
            }
        }
    }
    val notificationPermissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { }

    LaunchedEffect(Unit) {
        if (android.os.Build.VERSION.SDK_INT >= 33 &&
            androidx.core.content.ContextCompat.checkSelfPermission(
                context,
                android.Manifest.permission.POST_NOTIFICATIONS,
            ) != android.content.pm.PackageManager.PERMISSION_GRANTED
        ) {
            notificationPermissionLauncher.launch(android.Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    LaunchedEffect(state.errorMessage, state.notice) {
        val message = state.errorMessage ?: state.notice ?: return@LaunchedEffect
        snackbarHostState.showSnackbar(message)
        if (state.errorMessage != null) viewModel.clearError()
        if (state.notice != null) viewModel.clearNotice()
    }

    fun requestMediaAccess(photos: Boolean, videos: Boolean, onGranted: () -> Unit = {}) {
        val permissions = if (android.os.Build.VERSION.SDK_INT >= 33) {
            buildList {
                if (photos) add(android.Manifest.permission.READ_MEDIA_IMAGES)
                if (videos) add(android.Manifest.permission.READ_MEDIA_VIDEO)
            }
        } else if (photos || videos) {
            listOf(android.Manifest.permission.READ_EXTERNAL_STORAGE)
        } else {
            emptyList()
        }
        val missingPermissions = permissions.filter { permission ->
            androidx.core.content.ContextCompat.checkSelfPermission(
                context,
                permission,
            ) != android.content.pm.PackageManager.PERMISSION_GRANTED
        }
        if (missingPermissions.isEmpty()) {
            onGranted()
            return
        }
        pendingPermissionAction = onGranted
        permissionLauncher.launch(missingPermissions.toTypedArray())
    }

    if (!state.configured) {
        SettingsScreen(
            state = state,
            forceConnection = true,
            onSaveConnection = viewModel::saveTelegramConfig,
            onRequestSync = { requestMediaAccess(photos = true, videos = true) },
            onRunSync = viewModel::runSyncNow,
            onSaveCacheOptions = { _, _ -> },
            onClearCache = viewModel::clearCache,
        )
        return
    }

    val openEntry: (DriveEntry) -> Unit = { entry ->
        when {
            entry.isDirectory -> viewModel.openDirectory(entry.path)
            entry.isVideo() -> context.startActivity(
                Intent(context, VideoPlayerActivity::class.java)
                    .putExtra(VideoPlayerActivity.EXTRA_URL, viewModel.contentUrl(entry.path))
                    .putExtra(VideoPlayerActivity.EXTRA_TOKEN, viewModel.accessToken())
                    .putExtra(VideoPlayerActivity.EXTRA_TITLE, entry.name)
                    .putExtra(VideoPlayerActivity.EXTRA_MIME_TYPE, entry.mimeType),
            )
            entry.isAudio() -> context.startActivity(
                Intent(context, AudioPlayerActivity::class.java)
                    .putExtra(AudioPlayerActivity.EXTRA_URL, viewModel.contentUrl(entry.path))
                    .putExtra(AudioPlayerActivity.EXTRA_TOKEN, viewModel.accessToken())
                    .putExtra(AudioPlayerActivity.EXTRA_TITLE, entry.name),
            )
            entry.isPdf() -> context.startActivity(
                Intent(context, PdfViewerActivity::class.java)
                    .putExtra(PdfViewerActivity.EXTRA_SERVER_URL, viewModel.serverUrl())
                    .putExtra(PdfViewerActivity.EXTRA_TOKEN, viewModel.accessToken())
                    .putExtra(PdfViewerActivity.EXTRA_PATH, entry.path)
                    .putExtra(PdfViewerActivity.EXTRA_TITLE, entry.name),
            )
            entry.isImage() -> context.startActivity(
                Intent(context, ImageViewerActivity::class.java)
                    .putExtra(ImageViewerActivity.EXTRA_URL, viewModel.mediaContentUrl(entry.path))
                    .putExtra(ImageViewerActivity.EXTRA_TITLE, entry.name),
            )
            else -> (context as? MainActivity)?.enqueueDownload(
                entry,
                viewModel.contentUrl(entry.path, download = true),
                viewModel.accessToken(),
            )
        }
    }

    val downloadEntry: (DriveEntry) -> Unit = { entry ->
        (context as? MainActivity)?.enqueueDownload(
            entry,
            viewModel.contentUrl(entry.path, download = true),
            viewModel.accessToken(),
        )
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbarHostState) },
        bottomBar = {
            NavigationBar(containerColor = MaterialTheme.colorScheme.surface) {
                NavigationBarItem(
                    selected = state.sectionValue == AppSection.DRIVE,
                    onClick = { viewModel.selectSection(AppSection.DRIVE.name) },
                    icon = { Icon(Icons.Outlined.Folder, contentDescription = null) },
                    label = { Text("Drive") },
                )
                NavigationBarItem(
                    selected = state.sectionValue == AppSection.TRANSFERS,
                    onClick = { viewModel.selectSection(AppSection.TRANSFERS.name) },
                    icon = { Icon(Icons.Outlined.CloudDownload, contentDescription = null) },
                    label = { Text("Transfer") },
                )
                NavigationBarItem(
                    selected = state.sectionValue == AppSection.SYNC,
                    onClick = { viewModel.selectSection(AppSection.SYNC.name) },
                    icon = { Icon(Icons.Outlined.Sync, contentDescription = null) },
                    label = { Text("Sync") },
                )
                NavigationBarItem(
                    selected = state.sectionValue == AppSection.SETTINGS,
                    onClick = { viewModel.selectSection(AppSection.SETTINGS.name) },
                    icon = { Icon(Icons.Outlined.Settings, contentDescription = null) },
                    label = { Text("Pengaturan") },
                )
            }
        },
    ) { padding ->
        Surface(
            modifier = Modifier
                .padding(padding)
                .fillMaxSize(),
            color = MaterialTheme.colorScheme.background,
        ) {
            when (state.sectionValue) {
                AppSection.DRIVE -> DriveScreen(
                    state = state,
                    viewModel = viewModel,
                    onOpenEntry = openEntry,
                    onDownload = downloadEntry,
                    onPickFile = { picker.launch(arrayOf("*/*")) },
                )
                AppSection.TRANSFERS -> TransferScreen(
                    state = state,
                    downloads = downloads,
                    onPauseOrResumeUpload = viewModel::pauseOrResumeUpload,
                    onCancelUpload = viewModel::cancelUpload,
                    onPauseDownload = DownloadTransfers::pause,
                    onResumeDownload = DownloadTransfers::resume,
                    onCancelDownload = DownloadTransfers::cancel,
                )
                AppSection.SYNC -> SyncScreen(
                    state = state,
                    onSave = { photos, videos, folder ->
                        requestMediaAccess(photos, videos) {
                            viewModel.saveSyncOptions(photos, videos, folder)
                        }
                    },
                    onRunNow = { photos, videos, folder ->
                        requestMediaAccess(photos, videos) {
                            viewModel.saveSyncOptions(photos, videos, folder)
                        }
                    },
                )
                AppSection.SETTINGS -> SettingsScreen(
                    state = state,
                    forceConnection = false,
                    onSaveConnection = viewModel::saveTelegramConfig,
                    onRequestSync = { requestMediaAccess(photos = true, videos = true) },
                    onRunSync = viewModel::runSyncNow,
                    onSaveCacheOptions = viewModel::saveCacheOptions,
                    onClearCache = viewModel::clearCache,
                )
            }
        }
    }
}

@Composable
private fun DriveScreen(
    state: DriveUiState,
    viewModel: DriveViewModel,
    onOpenEntry: (DriveEntry) -> Unit,
    onDownload: (DriveEntry) -> Unit,
    onPickFile: () -> Unit,
) {
    var showFolderDialog by rememberSaveable { mutableStateOf(false) }
    var renameEntry by remember { mutableStateOf<DriveEntry?>(null) }
    var deleteEntry by remember { mutableStateOf<DriveEntry?>(null) }
    var showBatchDeleteDialog by rememberSaveable { mutableStateOf(false) }
    val selectionActive = state.selectedPaths.isNotEmpty()
    val selectedFileCount = state.entries.count { !it.isDirectory && it.path in state.selectedPaths }
    val handleEntryClick: (DriveEntry) -> Unit = { entry ->
        if (selectionActive) viewModel.toggleSelection(entry.path) else onOpenEntry(entry)
    }

    Box(modifier = Modifier.fillMaxSize()) {
        Column(modifier = Modifier.fillMaxSize()) {
            if (selectionActive) {
                SelectionBar(
                    selectedCount = state.selectedPaths.size,
                    totalCount = state.entries.size,
                    onSelectAll = viewModel::selectAllVisible,
                    onClear = viewModel::clearSelection,
                    onDownload = {
                        state.entries
                            .filter { !it.isDirectory && it.path in state.selectedPaths }
                            .forEach(onDownload)
                        viewModel.clearSelection()
                    },
                    downloadableCount = selectedFileCount,
                    onDelete = { showBatchDeleteDialog = true },
                )
            }
            DriveHeader(
                state = state,
                viewModel = viewModel,
                showGrid = state.settings.gridView,
                onToggleLayout = viewModel::toggleLayout,
                onSelectSort = viewModel::setSortOption,
                onCreateFolder = { showFolderDialog = true },
            )

            if (state.isLoading) {
                LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
            }
            if (state.entries.isEmpty() && !state.isLoading) {
                EmptyDrive(
                    isSearch = state.searchQuery.isNotBlank(),
                    onCreateFolder = { showFolderDialog = true },
                    onPickFile = onPickFile,
                )
            } else if (state.settings.gridView) {
                LazyVerticalGrid(
                    columns = GridCells.Fixed(2),
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 112.dp),
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    gridItems(state.sortedEntries, key = { it.path }) { entry ->
                        DriveGridCard(
                            entry = entry,
                            thumbnailUrl = if (entry.hasThumbnail) {
                                viewModel.thumbnailUrl(entry.path)
                            } else {
                                null
                            },
                            videoUrl = if (entry.isVideo()) {
                                viewModel.contentUrl(entry.path)
                            } else {
                                null
                            },
                            loadVideoFrame = true,
                            isSelected = entry.path in state.selectedPaths,
                            onClick = { handleEntryClick(entry) },
                            onLongClick = { viewModel.toggleSelection(entry.path) },
                            onDownload = { onDownload(entry) },
                            onRename = { renameEntry = entry },
                            onDelete = { deleteEntry = entry },
                        )
                    }
                }
            } else {
                LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 112.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    columnItems(state.sortedEntries, key = { it.path }) { entry ->
                        DriveListRow(
                            entry = entry,
                            thumbnailUrl = if (entry.hasThumbnail) {
                                viewModel.thumbnailUrl(entry.path)
                            } else {
                                null
                            },
                            videoUrl = if (entry.isVideo()) {
                                viewModel.contentUrl(entry.path)
                            } else {
                                null
                            },
                            loadVideoFrame = true,
                            isSelected = entry.path in state.selectedPaths,
                            onClick = { handleEntryClick(entry) },
                            onLongClick = { viewModel.toggleSelection(entry.path) },
                            onDownload = { onDownload(entry) },
                            onRename = { renameEntry = entry },
                            onDelete = { deleteEntry = entry },
                        )
                    }
                }
            }
        }

        ExtendedFloatingActionButton(
            onClick = onPickFile,
            modifier = Modifier
                .align(Alignment.BottomEnd)
                .padding(20.dp)
                .navigationBarsPadding(),
            containerColor = MaterialTheme.colorScheme.primary,
            contentColor = MaterialTheme.colorScheme.onPrimary,
            icon = {
                if (state.isUploading) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(20.dp),
                        color = MaterialTheme.colorScheme.onPrimary,
                        strokeWidth = 2.dp,
                    )
                } else {
                    Icon(Icons.Outlined.CloudUpload, contentDescription = null)
                }
            },
            text = { Text(if (state.isUploading) "Mengunggah" else "Upload") },
        )
    }

    if (showBatchDeleteDialog) {
        AlertDialog(
            onDismissRequest = { showBatchDeleteDialog = false },
            title = { Text("Hapus ${state.selectedPaths.size} item?") },
            text = { Text("Semua file dan folder yang dipilih akan dihapus dari Telegram.") },
            confirmButton = {
                Button(
                    onClick = {
                        showBatchDeleteDialog = false
                        viewModel.deleteSelected()
                    },
                    enabled = !state.isDeletingSelected,
                ) { Text(if (state.isDeletingSelected) "Menghapus…" else "Hapus semua") }
            },
            dismissButton = { TextButton(onClick = { showBatchDeleteDialog = false }) { Text("Batal") } },
        )
    }

    if (showFolderDialog) {
        NameDialog(
            title = "Buat folder",
            confirmLabel = "Buat folder",
            onDismiss = { showFolderDialog = false },
            onConfirm = {
                showFolderDialog = false
                viewModel.createFolder(it)
            },
        )
    }
    renameEntry?.let { entry ->
        NameDialog(
            title = "Ubah nama",
            initialValue = entry.name,
            confirmLabel = "Simpan",
            onDismiss = { renameEntry = null },
            onConfirm = {
                renameEntry = null
                viewModel.rename(entry, it)
            },
        )
    }
    deleteEntry?.let { entry ->
        AlertDialog(
            onDismissRequest = { deleteEntry = null },
            title = { Text("Hapus ${entry.name}?") },
            text = {
                Text(
                    if (entry.isDirectory) {
                        "Folder dan seluruh isinya akan dihapus dari Telegram dan tidak dapat dipulihkan."
                    } else {
                        "File akan dihapus dari Telegram dan tidak dapat dipulihkan."
                    },
                )
            },
            confirmButton = {
                Button(
                    onClick = {
                        deleteEntry = null
                        viewModel.delete(entry)
                    },
                ) { Text("Hapus") }
            },
            dismissButton = { TextButton(onClick = { deleteEntry = null }) { Text("Batal") } },
        )
    }
}

@Composable
private fun DriveHeader(
    state: DriveUiState,
    viewModel: DriveViewModel,
    showGrid: Boolean,
    onToggleLayout: () -> Unit,
    onSelectSort: (SortOption) -> Unit,
    onCreateFolder: () -> Unit,
) {
    var sortExpanded by remember { mutableStateOf(false) }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.background)
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (state.currentPath != ROOT_PATH && !state.isSearching) {
                IconButton(onClick = viewModel::goUp) {
                    Icon(Icons.Outlined.ArrowBack, contentDescription = "Kembali")
                }
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = if (state.isSearching) "Hasil pencarian" else "My Drive",
                    style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    text = if (state.isSearching) {
                        if (state.searchQuery.isBlank()) "Cari file dan folder" else "${state.searchQuery} · ${state.entries.size} hasil"
                    } else {
                        if (state.currentPath == ROOT_PATH) "Tersimpan aman di Telegram" else state.currentPath
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            IconButton(onClick = onToggleLayout) {
                Icon(
                    if (showGrid) Icons.Outlined.List else Icons.Outlined.GridView,
                    contentDescription = if (showGrid) "Tampilan daftar" else "Tampilan grid",
                )
            }
            Box {
                IconButton(onClick = { sortExpanded = true }) {
                    Icon(Icons.Outlined.Sort, contentDescription = "Urutkan")
                }
                DropdownMenu(
                    expanded = sortExpanded,
                    onDismissRequest = { sortExpanded = false },
                ) {
                    SortOption.values().forEach { option ->
                        DropdownMenuItem(
                            text = {
                                Text(
                                    if (option == state.sortOption) "✓ ${option.label}" else option.label,
                                )
                            },
                            onClick = {
                                sortExpanded = false
                                onSelectSort(option)
                            },
                        )
                    }
                }
            }
            if (!state.isSearching) {
                IconButton(onClick = viewModel::startSearch) {
                    Icon(Icons.Outlined.Search, contentDescription = "Cari")
                }
                IconButton(onClick = onCreateFolder) {
                    Icon(Icons.Outlined.CreateNewFolder, contentDescription = "Folder baru")
                }
                IconButton(onClick = viewModel::refresh) {
                    Icon(Icons.Outlined.Refresh, contentDescription = "Muat ulang")
                }
            } else {
                IconButton(onClick = viewModel::clearSearch) {
                    Icon(Icons.Outlined.ArrowBack, contentDescription = "Tutup pencarian")
                }
            }
        }
        if (state.isSearching) {
            OutlinedTextField(
                value = state.searchQuery,
                onValueChange = viewModel::updateSearchQuery,
                modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                singleLine = true,
                placeholder = { Text("Cari nama file atau folder") },
                leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                trailingIcon = {
                    if (state.searchQuery.isNotBlank()) {
                        TextButton(onClick = viewModel::submitSearch) { Text("Cari") }
                    }
                },
                keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = ImeAction.Search),
                shape = RoundedCornerShape(16.dp),
            )
        }
        if (!state.isSearching && state.currentPath != ROOT_PATH) {
            Text(
                text = "Drive / ${state.currentPath.trim('/').replace('/', '·')} ",
                modifier = Modifier.padding(start = 4.dp, top = 10.dp),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.primary,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun DriveGridCard(
    entry: DriveEntry,
    thumbnailUrl: String?,
    videoUrl: String?,
    loadVideoFrame: Boolean,
    isSelected: Boolean,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
    onDownload: () -> Unit,
    onRename: () -> Unit,
    onDelete: () -> Unit,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    Card(
        modifier = Modifier.fillMaxWidth().combinedClickable(onClick = onClick, onLongClick = onLongClick),
        shape = RoundedCornerShape(18.dp),
        colors = CardDefaults.cardColors(
            containerColor = if (isSelected) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surface,
        ),
    ) {
        Column {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(132.dp)
                    .clip(RoundedCornerShape(topStart = 18.dp, topEnd = 18.dp))
                    .background(MaterialTheme.colorScheme.surfaceVariant),
                contentAlignment = Alignment.Center,
            ) {
                FilePreview(entry, thumbnailUrl, videoUrl, loadVideoFrame, Modifier.fillMaxSize())
                if (entry.isVideo()) {
                    Box(
                        modifier = Modifier
                            .align(Alignment.Center)
                            .size(42.dp)
                            .clip(CircleShape)
                            .background(Color.Black.copy(alpha = 0.62f)),
                        contentAlignment = Alignment.Center,
                    ) {
                        Icon(Icons.Outlined.PlayArrow, contentDescription = "Putar", tint = Color.White)
                    }
                }
                IconButton(
                    onClick = { menuExpanded = true },
                    modifier = Modifier.align(Alignment.TopEnd),
                ) {
                    Icon(Icons.Outlined.MoreVert, contentDescription = "Aksi ${entry.name}")
                }
                DropdownMenu(expanded = menuExpanded, onDismissRequest = { menuExpanded = false }) {
                    EntryMenu(
                        entry = entry,
                        onDownload = { menuExpanded = false; onDownload() },
                        onRename = { menuExpanded = false; onRename() },
                        onDelete = { menuExpanded = false; onDelete() },
                    )
                }
            }
            Column(modifier = Modifier.padding(12.dp)) {
                Text(entry.name, maxLines = 1, overflow = TextOverflow.Ellipsis, fontWeight = FontWeight.SemiBold)
                Text(
                    if (entry.isDirectory) "Folder" else "${entry.size.readableFileSize()} · ${entry.updatedAt.readableDate()}",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

@Composable
private fun DriveListRow(
    entry: DriveEntry,
    thumbnailUrl: String?,
    videoUrl: String?,
    loadVideoFrame: Boolean,
    isSelected: Boolean,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
    onDownload: () -> Unit,
    onRename: () -> Unit,
    onDelete: () -> Unit,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    Card(
        modifier = Modifier.fillMaxWidth().combinedClickable(onClick = onClick, onLongClick = onLongClick),
        shape = RoundedCornerShape(14.dp),
        colors = CardDefaults.cardColors(
            containerColor = if (isSelected) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surface,
        ),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            FilePreview(entry, thumbnailUrl, videoUrl, loadVideoFrame, Modifier.size(54.dp))
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(entry.name, maxLines = 1, overflow = TextOverflow.Ellipsis, fontWeight = FontWeight.SemiBold)
                Text(
                    if (entry.isDirectory) "Folder" else "${entry.size.readableFileSize()} · ${entry.updatedAt.readableDate()}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Box {
                IconButton(onClick = { menuExpanded = true }) {
                    Icon(Icons.Outlined.MoreVert, contentDescription = "Aksi ${entry.name}")
                }
                DropdownMenu(expanded = menuExpanded, onDismissRequest = { menuExpanded = false }) {
                    EntryMenu(
                        entry = entry,
                        onDownload = { menuExpanded = false; onDownload() },
                        onRename = { menuExpanded = false; onRename() },
                        onDelete = { menuExpanded = false; onDelete() },
                    )
                }
            }
        }
    }
}

@Composable
private fun EntryMenu(
    entry: DriveEntry,
    onDownload: () -> Unit,
    onRename: () -> Unit,
    onDelete: () -> Unit,
) {
    if (!entry.isDirectory) {
        DropdownMenuItem(
            text = { Text("Download") },
            leadingIcon = { Icon(Icons.Outlined.Download, contentDescription = null) },
            onClick = onDownload,
        )
    }
    DropdownMenuItem(
        text = { Text("Ubah nama") },
        leadingIcon = { Icon(Icons.Outlined.DriveFileRenameOutline, contentDescription = null) },
        onClick = onRename,
    )
    DropdownMenuItem(
        text = { Text("Hapus") },
        leadingIcon = { Icon(Icons.Outlined.DeleteOutline, contentDescription = null) },
        onClick = onDelete,
    )
}

@Composable
private fun FilePreview(
    entry: DriveEntry,
    thumbnailUrl: String?,
    videoUrl: String?,
    loadVideoFrame: Boolean,
    modifier: Modifier,
) {
    Box(modifier = modifier.clip(RoundedCornerShape(12.dp)), contentAlignment = Alignment.Center) {
        when {
            entry.isImage() && thumbnailUrl != null -> {
                AsyncImage(
                    model = thumbnailUrl,
                    contentDescription = "Thumbnail ${entry.name}",
                    modifier = Modifier.fillMaxSize(),
                    contentScale = ContentScale.Crop,
                )
            }
            entry.isVideo() -> VideoThumbnail(videoUrl, entry.contentHash, entry.name, loadVideoFrame)
            else -> {
                Box(
                    modifier = Modifier.fillMaxSize().background(fileColor(entry)),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(fileIcon(entry), contentDescription = null, tint = Color.White, modifier = Modifier.size(32.dp))
                }
            }
        }
    }
}

@Composable
private fun VideoThumbnail(
    streamUrl: String?,
    contentHash: String,
    title: String,
    loadVideoFrame: Boolean,
) {
    val context = LocalContext.current.applicationContext
    val cacheFile = remember(streamUrl, contentHash) {
        videoThumbnailFile(context.cacheDir, contentHash.ifBlank { streamUrl.orEmpty() })
    }
    val frame by produceState<Bitmap?>(initialValue = null, streamUrl, contentHash, loadVideoFrame) {
        value = if (!loadVideoFrame || streamUrl.isNullOrBlank()) {
            null
        } else {
            loadCachedVideoThumbnail(cacheFile) ?: loadVideoFrame(streamUrl, cacheFile)
        }
    }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(fileColor(FileCategory.VIDEO)),
        contentAlignment = Alignment.Center,
    ) {
        Icon(Icons.Outlined.Movie, contentDescription = null, tint = Color.White, modifier = Modifier.size(34.dp))
        if (frame == null && loadVideoFrame) {
            CircularProgressIndicator(
                modifier = Modifier.size(28.dp),
                strokeWidth = 3.dp,
                color = Color.White,
            )
        }
        if (frame != null) {
            androidx.compose.foundation.Image(
                bitmap = frame!!.asImageBitmap(),
                contentDescription = "Thumbnail video $title",
                modifier = Modifier.fillMaxSize(),
                contentScale = ContentScale.Crop,
            )
        }
    }
}

private const val VIDEO_THUMBNAIL_MAX_DIMENSION = 512

private fun videoThumbnailFile(cacheDir: File, key: String): File {
    val digest = MessageDigest.getInstance("SHA-256").digest(key.toByteArray())
    val name = digest.joinToString("") { byte -> "%02x".format(byte.toInt() and 0xff) }
    return File(File(cacheDir, "tgdrive-video-thumbnails"), "$name.png")
}

private suspend fun loadVideoFrame(streamUrl: String, cacheFile: File): Bitmap? {
    val frame = withContext(Dispatchers.IO) {
        val retriever = MediaMetadataRetriever()
        try {
            retriever.setDataSource(streamUrl, emptyMap())
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O_MR1) {
                val (width, height) = videoThumbnailSize(retriever)
                retriever.getScaledFrameAtTime(
                    0L,
                    MediaMetadataRetriever.OPTION_CLOSEST_SYNC,
                    width,
                    height,
                )
            } else {
                retriever.getFrameAtTime(0L, MediaMetadataRetriever.OPTION_CLOSEST_SYNC)
            }
        } catch (_: RuntimeException) {
            null
        } finally {
            retriever.release()
        }
    }
    if (frame != null) saveVideoThumbnail(cacheFile, frame)
    return frame
}

private fun videoThumbnailSize(retriever: MediaMetadataRetriever): Pair<Int, Int> {
    val width = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_WIDTH)?.toIntOrNull()
    val height = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_HEIGHT)?.toIntOrNull()
    if (width == null || height == null || width <= 0 || height <= 0) {
        return VIDEO_THUMBNAIL_MAX_DIMENSION to VIDEO_THUMBNAIL_MAX_DIMENSION
    }
    val scale = minOf(1.0, VIDEO_THUMBNAIL_MAX_DIMENSION.toDouble() / maxOf(width, height))
    return maxOf(1, (width * scale).toInt()) to maxOf(1, (height * scale).toInt())
}

private fun loadCachedVideoThumbnail(cacheFile: File): Bitmap? {
    if (!cacheFile.isFile) return null
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    BitmapFactory.decodeFile(cacheFile.absolutePath, bounds)
    val largestDimension = maxOf(bounds.outWidth, bounds.outHeight)
    var sampleSize = 1
    while (largestDimension / sampleSize > VIDEO_THUMBNAIL_MAX_DIMENSION) {
        sampleSize *= 2
    }
    return BitmapFactory.decodeFile(
        cacheFile.absolutePath,
        BitmapFactory.Options().apply { inSampleSize = sampleSize },
    )
}

private fun saveVideoThumbnail(cacheFile: File, bitmap: Bitmap) {
    val directory = cacheFile.parentFile ?: return
    if (!directory.exists() && !directory.mkdirs()) return
    val temporary = File(directory, "${cacheFile.name}.tmp")
    try {
        FileOutputStream(temporary).use { output ->
            if (!bitmap.compress(Bitmap.CompressFormat.PNG, 100, output)) return
        }
        if (!temporary.renameTo(cacheFile)) temporary.delete()
    } catch (_: java.io.IOException) {
        temporary.delete()
    }
}

@Composable
private fun SelectionBar(
    selectedCount: Int,
    totalCount: Int,
    downloadableCount: Int,
    onSelectAll: () -> Unit,
    onClear: () -> Unit,
    onDownload: () -> Unit,
    onDelete: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.primaryContainer)
            .padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = onClear) {
            Icon(Icons.Outlined.ArrowBack, contentDescription = "Keluar dari mode pilih")
        }
        Text(
            text = "$selectedCount dipilih",
            modifier = Modifier.weight(1f),
            fontWeight = FontWeight.SemiBold,
        )
        if (selectedCount < totalCount) {
            TextButton(onClick = onSelectAll) { Text("Semua") }
        }
        if (downloadableCount > 0) {
            IconButton(onClick = onDownload) {
                Icon(Icons.Outlined.Download, contentDescription = "Download $downloadableCount file")
            }
        }
        IconButton(onClick = onDelete) {
            Icon(Icons.Outlined.DeleteOutline, contentDescription = "Hapus pilihan")
        }
    }
}

@Composable
private fun TransferScreen(
    state: DriveUiState,
    downloads: List<DownloadTransfer>,
    onPauseOrResumeUpload: () -> Unit,
    onCancelUpload: () -> Unit,
    onPauseDownload: (Long) -> Unit,
    onResumeDownload: (Long) -> Unit,
    onCancelDownload: (Long) -> Unit,
) {
    Column(modifier = Modifier.fillMaxSize()) {
        ScreenTitle(
            title = "Transfer",
            subtitle = "Progress upload & download aktif",
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (state.isUploading) {
                item(key = "upload") {
                    UploadProgressCard(
                        state = state,
                        onPauseOrResume = onPauseOrResumeUpload,
                        onCancel = onCancelUpload,
                    )
                }
            }
            columnItems(downloads, key = { it.id }) { download ->
                DownloadProgressCard(
                    transfer = download,
                    onPause = { onPauseDownload(download.id) },
                    onResume = { onResumeDownload(download.id) },
                    onCancel = { onCancelDownload(download.id) },
                )
            }
            if (!state.isUploading && downloads.isEmpty()) {
                item {
                    EmptyTransfers()
                }
            }
        }
    }
}

@Composable
private fun UploadProgressCard(
    state: DriveUiState,
    onPauseOrResume: () -> Unit,
    onCancel: () -> Unit,
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(14.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
    ) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.CloudUpload, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                Spacer(Modifier.width(12.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        if (state.isUploadFinalizing) "Menyelesaikan upload" else "Mengunggah",
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.primary,
                    )
                    Text(
                        state.uploadFileName.orEmpty(),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        fontWeight = FontWeight.SemiBold,
                    )
                }
                Text(
                    state.uploadProgress?.let { "$it%" } ?: "…",
                    fontWeight = FontWeight.Bold,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
            Spacer(Modifier.height(12.dp))
            if (state.uploadProgress == null) {
                LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
            } else {
                LinearProgressIndicator(
                    progress = state.uploadProgress / 100f,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            if (state.uploadTotalBytes > 0L) {
                Text(
                    "${state.uploadSentBytes.readableFileSize()} / ${state.uploadTotalBytes.readableFileSize()}",
                    modifier = Modifier.padding(top = 6.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Row(
                modifier = Modifier.fillMaxWidth().padding(top = 6.dp),
                horizontalArrangement = Arrangement.End,
            ) {
                TextButton(onClick = onPauseOrResume, enabled = !state.isUploadFinalizing) {
                    Text(if (state.isUploadPaused) "Lanjutkan" else "Jeda")
                }
                TextButton(onClick = onCancel) { Text("Batalkan") }
            }
        }
    }
}

@Composable
private fun DownloadProgressCard(
    transfer: DownloadTransfer,
    onPause: () -> Unit,
    onResume: () -> Unit,
    onCancel: () -> Unit,
) {
    val statusLabel = when (transfer.status) {
        DownloadStatus.QUEUED -> "Menunggu download"
        DownloadStatus.DOWNLOADING -> "Mengunduh"
        DownloadStatus.PAUSED -> "Download dijeda"
        DownloadStatus.COMPLETED -> "Download selesai"
        DownloadStatus.FAILED -> "Download gagal"
        DownloadStatus.CANCELED -> "Download dibatalkan"
    }
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(14.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
    ) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.CloudDownload, contentDescription = null, tint = MaterialTheme.colorScheme.secondary)
                Spacer(Modifier.width(12.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(statusLabel, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.secondary)
                    Text(
                        transfer.fileName,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        fontWeight = FontWeight.SemiBold,
                    )
                }
                Text(
                    transfer.progress?.let { "$it%" } ?: "…",
                    fontWeight = FontWeight.Bold,
                    color = MaterialTheme.colorScheme.secondary,
                )
            }
            Spacer(Modifier.height(12.dp))
            if (transfer.progress == null && transfer.isActive) {
                LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
            } else {
                LinearProgressIndicator(
                    progress = (transfer.progress ?: 0) / 100f,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            val detail = transfer.errorMessage
                ?: if (transfer.totalBytes > 0L) {
                    "${transfer.downloadedBytes.readableFileSize()} / ${transfer.totalBytes.readableFileSize()}"
                } else {
                    null
                }
            if (detail != null) {
                Text(
                    detail,
                    modifier = Modifier.padding(top = 6.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = if (transfer.status == DownloadStatus.FAILED) {
                        MaterialTheme.colorScheme.error
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (transfer.isActive) {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(top = 6.dp),
                    horizontalArrangement = Arrangement.End,
                ) {
                    if (transfer.status == DownloadStatus.PAUSED) {
                        TextButton(onClick = onResume) { Text("Lanjutkan") }
                    } else if (transfer.status == DownloadStatus.DOWNLOADING) {
                        TextButton(onClick = onPause) { Text("Jeda") }
                    }
                    TextButton(onClick = onCancel) { Text("Batalkan") }
                }
            }
        }
    }
}

@Composable
private fun EmptyTransfers() {
    Column(
        modifier = Modifier.fillMaxWidth().padding(top = 72.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(
            Icons.Outlined.CloudDone,
            contentDescription = null,
            modifier = Modifier.size(52.dp),
            tint = MaterialTheme.colorScheme.primary,
        )
        Spacer(Modifier.height(14.dp))
        Text("Tidak ada transfer", style = MaterialTheme.typography.titleMedium)
        Text(
            "Upload dan download aktif akan tampil di sini.",
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun EmptyDrive(isSearch: Boolean, onCreateFolder: () -> Unit, onPickFile: () -> Unit) {
    Column(
        modifier = Modifier.fillMaxSize().padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            if (isSearch) Icons.Outlined.Search else Icons.Outlined.CloudDone,
            contentDescription = null,
            modifier = Modifier.size(58.dp),
            tint = MaterialTheme.colorScheme.primary,
        )
        Spacer(Modifier.height(16.dp))
        Text(if (isSearch) "Tidak ada hasil" else "Belum ada file atau folder", style = MaterialTheme.typography.titleLarge)
        Spacer(Modifier.height(8.dp))
        Text(
            text = if (isSearch) "Coba kata kunci lain." else "Unggah file atau buat folder untuk memulai.",
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        if (!isSearch) {
            Spacer(Modifier.height(20.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = onPickFile) { Text("Upload file") }
                OutlinedButton(onClick = onCreateFolder) { Text("Buat folder") }
            }
        }
    }
}

private fun fileIcon(entry: DriveEntry): ImageVector = when (entry.fileCategory()) {
    FileCategory.FOLDER -> Icons.Outlined.Folder
    FileCategory.IMAGE -> Icons.Outlined.Image
    FileCategory.VIDEO -> Icons.Outlined.Movie
    FileCategory.AUDIO -> Icons.Outlined.MusicNote
    FileCategory.PDF -> Icons.Outlined.PictureAsPdf
    FileCategory.TEXT -> Icons.Outlined.Description
    FileCategory.ARCHIVE -> Icons.Outlined.Description
    FileCategory.OTHER -> Icons.Outlined.InsertDriveFile
}

private fun fileColor(entry: DriveEntry): Color = fileColor(entry.fileCategory())

private fun fileColor(category: FileCategory): Color = when (category) {
    FileCategory.FOLDER -> Color(0xFFF59E0B)
    FileCategory.IMAGE -> Color(0xFF10B981)
    FileCategory.VIDEO -> Color(0xFF7651D9)
    FileCategory.AUDIO -> Color(0xFFE04465)
    FileCategory.PDF -> Color(0xFFE5484D)
    FileCategory.TEXT -> Color(0xFF64748B)
    FileCategory.ARCHIVE -> Color(0xFF0EA5A4)
    FileCategory.OTHER -> Color(0xFF475569)
}

@Composable
private fun NameDialog(
    title: String,
    initialValue: String = "",
    confirmLabel: String,
    onDismiss: () -> Unit,
    onConfirm: (String) -> Unit,
) {
    var value by rememberSaveable(title, initialValue) { mutableStateOf(initialValue) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            OutlinedTextField(
                value = value,
                onValueChange = { value = it },
                singleLine = true,
                label = { Text("Nama") },
            )
        },
        confirmButton = {
            Button(onClick = { onConfirm(value.trim()) }, enabled = value.isNotBlank()) { Text(confirmLabel) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Batal") } },
    )
}

@Composable
private fun SyncScreen(
    state: DriveUiState,
    onSave: (Boolean, Boolean, String) -> Unit,
    onRunNow: (Boolean, Boolean, String) -> Unit,
) {
    var syncPhotos by remember(state.settings.syncPhotos) { mutableStateOf(state.settings.syncPhotos) }
    var syncVideos by remember(state.settings.syncVideos) { mutableStateOf(state.settings.syncVideos) }
    var syncFolder by remember(state.settings.syncFolder) { mutableStateOf(state.settings.syncFolder) }

    Column(modifier = Modifier.fillMaxSize()) {
        ScreenTitle(title = "Sinkronisasi", subtitle = "Backup media ponsel ke Telegram")
        Column(
            modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            SyncCard(
                title = "Foto",
                description = "Sinkronkan foto baru dari galeri",
                checked = syncPhotos,
                onCheckedChange = { syncPhotos = it },
                icon = Icons.Outlined.Image,
                color = Color(0xFF10B981),
            )
            SyncCard(
                title = "Video",
                description = "Sinkronkan video baru dari galeri",
                checked = syncVideos,
                onCheckedChange = { syncVideos = it },
                icon = Icons.Outlined.Movie,
                color = Color(0xFF8B5CF6),
            )
            OutlinedTextField(
                value = syncFolder,
                onValueChange = { syncFolder = it },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
                label = { Text("Folder tujuan") },
                supportingText = { Text("Media masuk langsung ke folder Foto atau Video.") },
                shape = RoundedCornerShape(14.dp),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Button(
                    onClick = { onSave(syncPhotos, syncVideos, syncFolder) },
                    modifier = Modifier.weight(1f),
                ) { Text("Simpan") }
                OutlinedButton(
                    onClick = { onRunNow(syncPhotos, syncVideos, syncFolder) },
                    enabled = syncPhotos || syncVideos,
                    modifier = Modifier.weight(1f),
                ) { Text("Sync sekarang") }
            }
            Card(
                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
                shape = RoundedCornerShape(14.dp),
            ) {
                Row(modifier = Modifier.padding(14.dp), verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Outlined.CloudDone, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                    Spacer(Modifier.width(12.dp))
                    Text(
                        "Sync otomatis berjalan setiap 12 jam saat perangkat terhubung internet.",
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun SyncCard(
    title: String,
    description: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    icon: ImageVector,
    color: Color,
) {
    Card(
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
        shape = RoundedCornerShape(14.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier.size(42.dp).clip(RoundedCornerShape(12.dp)).background(color.copy(alpha = 0.18f)),
                contentAlignment = Alignment.Center,
            ) { Icon(icon, contentDescription = null, tint = color) }
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(title, fontWeight = FontWeight.SemiBold)
                Text(description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Switch(checked = checked, onCheckedChange = onCheckedChange)
        }
    }
}

@Composable
private fun SettingsScreen(
    state: DriveUiState,
    forceConnection: Boolean,
    onSaveConnection: (String, String, String, String, String) -> Unit,
    onRequestSync: () -> Unit,
    onRunSync: () -> Unit,
    onSaveCacheOptions: (String, String) -> Unit,
    onClearCache: () -> Unit,
) {
    var botToken by remember(state.settings.botToken) { mutableStateOf(state.settings.botToken) }
    var apiId by remember(state.settings.apiId) { mutableStateOf(state.settings.apiId) }
    var apiHash by remember(state.settings.apiHash) { mutableStateOf(state.settings.apiHash) }
    var chatId by remember(state.settings.chatId) { mutableStateOf(state.settings.chatId) }
    var encryptionKey by remember(state.settings.encryptionKey) { mutableStateOf(state.settings.encryptionKey) }
    var maxCacheMb by remember(state.settings.maxCacheMb) { mutableStateOf(state.settings.maxCacheMb.toString()) }
    var cacheTtlHours by remember(state.settings.cacheTtlHours) { mutableStateOf(state.settings.cacheTtlHours.toString()) }
    var showClearCacheDialog by rememberSaveable { mutableStateOf(false) }

    Column(modifier = Modifier.fillMaxSize()) {
        ScreenTitle(
            title = if (forceConnection) "Siapkan TgDrive" else "Pengaturan",
            subtitle = if (forceConnection) "Hubungkan storage Telegram kamu" else "Koneksi dan keamanan",
        )
        Column(
            modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            SecretField("Bot token", botToken, { botToken = it }, "Token dari BotFather")
            OutlinedTextField(
                value = apiId,
                onValueChange = { apiId = it },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
                label = { Text("Telegram API ID") },
                shape = RoundedCornerShape(14.dp),
            )
            SecretField("Telegram API hash", apiHash, { apiHash = it }, "API hash dari my.telegram.org")
            OutlinedTextField(
                value = chatId,
                onValueChange = { chatId = it },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
                label = { Text("Storage chat ID") },
                supportingText = { Text("Contoh channel: -1001234567890") },
                shape = RoundedCornerShape(14.dp),
            )
            SecretField("Encryption key", encryptionKey, { encryptionKey = it }, "Gunakan string rahasia panjang")
            Button(
                onClick = { onSaveConnection(botToken, apiId, apiHash, chatId, encryptionKey) },
                enabled = botToken.isNotBlank() && apiId.isNotBlank() && apiHash.isNotBlank() &&
                    chatId.isNotBlank() && encryptionKey.isNotBlank(),
                modifier = Modifier.fillMaxWidth().height(52.dp),
                shape = RoundedCornerShape(14.dp),
            ) { Text(if (forceConnection) "Simpan dan hubungkan" else "Simpan konfigurasi") }
            if (!forceConnection) {
                Divider(modifier = Modifier.padding(vertical = 8.dp))
                Text("Akses media", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                OutlinedButton(onClick = onRequestSync, modifier = Modifier.fillMaxWidth()) { Text("Minta izin galeri") }
                OutlinedButton(onClick = onRunSync, modifier = Modifier.fillMaxWidth()) { Text("Jalankan sync sekarang") }
                Divider(modifier = Modifier.padding(vertical = 8.dp))
                Text("Cache lokal", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                OutlinedTextField(
                    value = maxCacheMb,
                    onValueChange = { maxCacheMb = it.filter(Char::isDigit) },
                    modifier = Modifier.fillMaxWidth(),
                    singleLine = true,
                    label = { Text("Maksimum cache (MB)") },
                    supportingText = { Text("50–64000 MB. Cache lama dihapus otomatis saat penuh.") },
                    keyboardOptions = KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Number),
                )
                OutlinedTextField(
                    value = cacheTtlHours,
                    onValueChange = { cacheTtlHours = it.filter(Char::isDigit) },
                    modifier = Modifier.fillMaxWidth(),
                    singleLine = true,
                    label = { Text("Masa cache (jam)") },
                    supportingText = { Text("0 berarti tidak kedaluwarsa; file berubah tetap memakai cache baru.") },
                    keyboardOptions = KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Number),
                )
                Button(
                    onClick = { onSaveCacheOptions(maxCacheMb, cacheTtlHours) },
                    modifier = Modifier.fillMaxWidth(),
                ) { Text("Simpan pengaturan cache") }
                Divider(modifier = Modifier.padding(vertical = 8.dp))
                Text("Penyimpanan lokal", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                Text(
                    "Hapus data sementara di perangkat. File di Telegram tidak terpengaruh.",
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodySmall,
                )
                OutlinedButton(
                    onClick = { showClearCacheDialog = true },
                    modifier = Modifier.fillMaxWidth(),
                ) { Text("Bersihkan cache") }
            }
        }
    }
    if (showClearCacheDialog) {
        AlertDialog(
            onDismissRequest = { showClearCacheDialog = false },
            title = { Text("Bersihkan cache?") },
            text = { Text("Data sementara di perangkat akan dihapus. File di Telegram tetap aman.") },
            confirmButton = {
                Button(
                    onClick = {
                        showClearCacheDialog = false
                        onClearCache()
                    },
                ) { Text("Bersihkan") }
            },
            dismissButton = { TextButton(onClick = { showClearCacheDialog = false }) { Text("Batal") } },
        )
    }
}

@Composable
private fun SecretField(label: String, value: String, onValueChange: (String) -> Unit, supportingText: String) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = Modifier.fillMaxWidth(),
        singleLine = true,
        label = { Text(label) },
        supportingText = { Text(supportingText) },
        visualTransformation = PasswordVisualTransformation(),
        shape = RoundedCornerShape(14.dp),
    )
}

@Composable
private fun ScreenTitle(title: String, subtitle: String) {
    Column(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 16.dp)) {
        Text(title, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
        Text(subtitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}
