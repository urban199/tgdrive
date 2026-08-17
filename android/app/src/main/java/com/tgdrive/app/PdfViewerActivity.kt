package com.tgdrive.app

import android.graphics.Bitmap
import android.graphics.Color
import android.graphics.pdf.PdfRenderer
import android.os.Bundle
import android.os.ParcelFileDescriptor
import android.view.Gravity
import android.view.View
import android.widget.ImageButton
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.ScrollView
import android.widget.TextView
import androidx.activity.ComponentActivity
import androidx.lifecycle.lifecycleScope
import com.tgdrive.app.data.DriveApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File

class PdfViewerActivity : ComponentActivity() {
    private lateinit var pageContainer: LinearLayout
    private lateinit var statusText: TextView
    private lateinit var progress: ProgressBar

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val serverUrl = intent.getStringExtra(EXTRA_SERVER_URL)
        val token = intent.getStringExtra(EXTRA_TOKEN).orEmpty()
        val path = intent.getStringExtra(EXTRA_PATH)
        val documentTitle = intent.getStringExtra(EXTRA_TITLE).orEmpty()
        if (serverUrl.isNullOrBlank() || path.isNullOrBlank()) {
            finish()
            return
        }

        window.statusBarColor = Color.rgb(13, 15, 20)
        window.navigationBarColor = Color.rgb(13, 15, 20)
        setContentView(createContent(documentTitle))

        lifecycleScope.launch {
            val file = File.createTempFile("tgdrive-pdf-", ".pdf", cacheDir)
            try {
                statusText.text = "Mengunduh dokumen..."
                DriveApi(serverUrl, token).downloadTo(path, file)
                renderPdf(file)
                statusText.text = "PDF · siap dibaca"
            } catch (error: Exception) {
                statusText.text = error.message ?: "PDF tidak bisa dibuka"
            } finally {
                progress.visibility = View.GONE
                file.delete()
            }
        }
    }

    private fun createContent(documentTitle: String): LinearLayout {
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(Color.rgb(13, 15, 20))
        }
        val topBar = LinearLayout(this).apply {
            gravity = Gravity.CENTER_VERTICAL
            setPadding(8, 8, 16, 8)
            setBackgroundColor(Color.rgb(22, 25, 35))
        }
        topBar.addView(ImageButton(this).apply {
            setImageResource(android.R.drawable.ic_menu_revert)
            setColorFilter(Color.WHITE)
            setBackgroundColor(Color.TRANSPARENT)
            contentDescription = "Kembali"
            setOnClickListener { finish() }
        }, LinearLayout.LayoutParams(52, 52))
        topBar.addView(TextView(this).apply {
            text = documentTitle
            textSize = 16f
            setTextColor(Color.WHITE)
            maxLines = 1
            ellipsize = android.text.TextUtils.TruncateAt.END
            gravity = Gravity.CENTER_VERTICAL
        }, LinearLayout.LayoutParams(0, 52, 1f))
        root.addView(topBar)

        statusText = TextView(this).apply {
            text = "Menyiapkan PDF..."
            textSize = 13f
            setTextColor(Color.rgb(154, 164, 182))
            setPadding(20, 16, 20, 10)
        }
        root.addView(statusText)
        progress = ProgressBar(this)
        root.addView(progress, LinearLayout.LayoutParams(-1, 4))

        val scrollView = ScrollView(this)
        pageContainer = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_HORIZONTAL
            setPadding(12, 16, 12, 32)
        }
        scrollView.addView(pageContainer)
        root.addView(scrollView, LinearLayout.LayoutParams(-1, 0, 1f))
        return root
    }

    private suspend fun renderPdf(file: File) {
        val displayWidth = (resources.displayMetrics.widthPixels - 24).coerceAtLeast(320)
        withContext(Dispatchers.IO) {
            ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY).use { descriptor ->
                PdfRenderer(descriptor).use { renderer ->
                    for (index in 0 until renderer.pageCount) {
                        val page = renderer.openPage(index)
                        try {
                            val scale = displayWidth.toFloat() / page.width.toFloat()
                            val bitmapWidth = displayWidth
                            val bitmapHeight = (page.height * scale).toInt().coerceAtLeast(1)
                            val bitmap = Bitmap.createBitmap(bitmapWidth, bitmapHeight, Bitmap.Config.RGB_565)
                            bitmap.eraseColor(Color.WHITE)
                            page.render(bitmap, null, null, PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY)
                            withContext(Dispatchers.Main) { addPage(bitmap, index + 1) }
                        } finally {
                            page.close()
                        }
                    }
                }
            }
        }
    }

    private fun addPage(bitmap: Bitmap, pageNumber: Int) {
        val pageLayout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(Color.WHITE)
        }
        pageLayout.addView(ImageView(this).apply {
            setImageBitmap(bitmap)
            adjustViewBounds = true
            scaleType = ImageView.ScaleType.FIT_CENTER
            contentDescription = "Halaman $pageNumber"
        }, LinearLayout.LayoutParams(-1, -2))
        pageLayout.addView(TextView(this).apply {
            text = "Halaman $pageNumber"
            textSize = 12f
            setTextColor(Color.DKGRAY)
            gravity = Gravity.CENTER
            setPadding(0, 8, 0, 8)
        }, LinearLayout.LayoutParams(-1, -2))
        pageContainer.addView(pageLayout, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = 16 })
    }

    companion object {
        const val EXTRA_SERVER_URL = "pdf_server_url"
        const val EXTRA_TOKEN = "pdf_token"
        const val EXTRA_PATH = "pdf_path"
        const val EXTRA_TITLE = "pdf_title"
    }
}
