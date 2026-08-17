package com.tgdrive.app

import android.content.pm.ActivityInfo
import android.graphics.Color
import android.os.Bundle
import android.view.Gravity
import android.view.View
import android.widget.FrameLayout
import android.widget.ImageButton
import android.widget.TextView
import android.widget.Toast
import kotlin.math.roundToInt
import androidx.activity.ComponentActivity
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.ui.PlayerView

class VideoPlayerActivity : ComponentActivity() {
    private var player: ExoPlayer? = null
    private var isFullscreen = false
    private lateinit var fullscreenButton: ImageButton

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.statusBarColor = Color.BLACK
        window.navigationBarColor = Color.BLACK
        window.addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_PORTRAIT

        val url = intent.getStringExtra(EXTRA_URL)
        if (url.isNullOrBlank()) {
            finish()
            return
        }

        val title = intent.getStringExtra(EXTRA_TITLE).orEmpty()
        val mimeType = intent.getStringExtra(EXTRA_MIME_TYPE).orEmpty()
        val token = intent.getStringExtra(EXTRA_TOKEN).orEmpty()
        val requestProperties = if (token.isBlank()) emptyMap() else mapOf("Authorization" to "Bearer $token")
        val dataSourceFactory = DefaultHttpDataSource.Factory()
            .setConnectTimeoutMs(15_000)
            .setReadTimeoutMs(30_000)
            .setAllowCrossProtocolRedirects(false)
            .setDefaultRequestProperties(requestProperties)
        val exoPlayer = try {
            ExoPlayer.Builder(this)
                .setMediaSourceFactory(DefaultMediaSourceFactory(dataSourceFactory))
                .build()
                .also { instance ->
                    instance.addListener(object : Player.Listener {
                        override fun onPlayerError(error: PlaybackException) {
                            Toast.makeText(
                                this@VideoPlayerActivity,
                                "Video tidak dapat diputar",
                                Toast.LENGTH_LONG,
                            ).show()
                        }
                    })
                    val mediaItem = MediaItem.Builder()
                        .setUri(url)
                        .apply {
                            if (mimeType.isNotBlank()) setMimeType(mimeType)
                        }
                        .build()
                    instance.setMediaItem(mediaItem)
                    instance.prepare()
                    instance.playWhenReady = true
                }
        } catch (_: Exception) {
            Toast.makeText(this, "Video tidak dapat dibuka", Toast.LENGTH_LONG).show()
            finish()
            return
        }
        player = exoPlayer

        val playerView = PlayerView(this).apply {
            player = exoPlayer
            useController = true
            controllerAutoShow = true
            controllerHideOnTouch = true
            controllerShowTimeoutMs = 3_000
            setShowBuffering(PlayerView.SHOW_BUFFERING_WHEN_PLAYING)
            setBackgroundColor(Color.BLACK)
            layoutParams = FrameLayout.LayoutParams(-1, -1)
        }
        val topBar = createTopBar(title)
        val root = FrameLayout(this).apply {
            setBackgroundColor(Color.BLACK)
            addView(playerView)
            addView(topBar)
        }
        ViewCompat.setOnApplyWindowInsetsListener(root) { _, insets ->
            val statusBarHeight = insets.getInsets(WindowInsetsCompat.Type.statusBars()).top
            topBar.setPadding(dp(8), statusBarHeight + dp(8), dp(12), dp(8))
            topBar.layoutParams = (topBar.layoutParams as FrameLayout.LayoutParams).apply {
                height = statusBarHeight + dp(64)
            }
            topBar.requestLayout()
            insets
        }
        setContentView(root)
        ViewCompat.requestApplyInsets(root)
    }

    private fun createTopBar(title: String): View {
        val bar = FrameLayout(this).apply {
            setBackgroundColor(Color.argb(170, 0, 0, 0))
            setPadding(dp(8), dp(8), dp(12), dp(8))
            layoutParams = FrameLayout.LayoutParams(-1, dp(64)).apply {
                gravity = Gravity.TOP
            }
        }
        val back = ImageButton(this).apply {
            setImageResource(R.drawable.ic_close)
            setColorFilter(Color.WHITE)
            setBackgroundColor(Color.TRANSPARENT)
            contentDescription = "Kembali"
            setOnClickListener {
                if (isFullscreen) toggleFullscreen() else finish()
            }
            layoutParams = FrameLayout.LayoutParams(dp(52), dp(48)).apply { gravity = Gravity.START or Gravity.CENTER_VERTICAL }
        }
        fullscreenButton = ImageButton(this).apply {
            setImageResource(R.drawable.ic_fullscreen)
            setColorFilter(Color.WHITE)
            setBackgroundColor(Color.TRANSPARENT)
            contentDescription = "Layar penuh"
            setOnClickListener { toggleFullscreen() }
            layoutParams = FrameLayout.LayoutParams(dp(52), dp(48)).apply {
                gravity = Gravity.END or Gravity.CENTER_VERTICAL
            }
        }
        val titleView = TextView(this).apply {
            text = title
            textSize = 16f
            setTextColor(Color.WHITE)
            maxLines = 1
            ellipsize = android.text.TextUtils.TruncateAt.END
            gravity = Gravity.CENTER_VERTICAL
            layoutParams = FrameLayout.LayoutParams(-1, dp(48)).apply {
                gravity = Gravity.CENTER_VERTICAL
                leftMargin = dp(52)
                rightMargin = dp(52)
            }
        }
        bar.addView(titleView)
        bar.addView(back)
        bar.addView(fullscreenButton)
        return bar
    }

    private fun toggleFullscreen() {
        isFullscreen = !isFullscreen
        val controller = WindowInsetsControllerCompat(window, window.decorView)
        if (isFullscreen) {
            requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE
            fullscreenButton.setImageResource(R.drawable.ic_fullscreen_exit)
            fullscreenButton.contentDescription = "Keluar dari layar penuh"
            controller.hide(WindowInsetsCompat.Type.systemBars())
            controller.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        } else {
            requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
            fullscreenButton.setImageResource(R.drawable.ic_fullscreen)
            fullscreenButton.contentDescription = "Layar penuh"
            controller.show(WindowInsetsCompat.Type.systemBars())
        }
        ViewCompat.requestApplyInsets(window.decorView)
    }

    @Deprecated("Use the fullscreen button or system back navigation")
    override fun onBackPressed() {
        if (isFullscreen) {
            toggleFullscreen()
        } else {
            super.onBackPressed()
        }
    }

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).roundToInt()

    override fun onStop() {
        player?.pause()
        super.onStop()
    }

    override fun onDestroy() {
        player?.release()
        player = null
        window.clearFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        super.onDestroy()
    }

    companion object {
        const val EXTRA_URL = "video_url"
        const val EXTRA_TOKEN = "video_token"
        const val EXTRA_TITLE = "video_title"
        const val EXTRA_MIME_TYPE = "video_mime_type"
    }
}
