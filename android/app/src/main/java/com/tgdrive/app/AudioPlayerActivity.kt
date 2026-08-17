package com.tgdrive.app

import android.graphics.Color
import android.os.Bundle
import android.view.Gravity
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import kotlin.math.roundToInt
import androidx.activity.ComponentActivity
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.ui.PlayerView

class AudioPlayerActivity : ComponentActivity() {
    private var player: ExoPlayer? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.statusBarColor = Color.BLACK
        window.navigationBarColor = Color.BLACK
        window.addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        val url = intent.getStringExtra(EXTRA_URL)
        if (url.isNullOrBlank()) {
            finish()
            return
        }
        val title = intent.getStringExtra(EXTRA_TITLE).orEmpty()
        val token = intent.getStringExtra(EXTRA_TOKEN).orEmpty()
        val headers = if (token.isBlank()) emptyMap() else mapOf("Authorization" to "Bearer $token")
        val dataSourceFactory = DefaultHttpDataSource.Factory()
            .setConnectTimeoutMs(15_000)
            .setReadTimeoutMs(30_000)
            .setDefaultRequestProperties(headers)
        val exoPlayer = ExoPlayer.Builder(this)
            .setMediaSourceFactory(DefaultMediaSourceFactory(dataSourceFactory))
            .build()
            .also { instance ->
                instance.addListener(object : Player.Listener {
                    override fun onPlayerError(error: PlaybackException) {
                        Toast.makeText(this@AudioPlayerActivity, "Audio tidak dapat diputar", Toast.LENGTH_LONG).show()
                    }
                })
                instance.setMediaItem(MediaItem.fromUri(url))
                instance.prepare()
                instance.playWhenReady = true
            }
        player = exoPlayer

        val playerView = PlayerView(this).apply {
            player = exoPlayer
            useController = true
            controllerAutoShow = true
            controllerShowTimeoutMs = 3_000
            setShowBuffering(PlayerView.SHOW_BUFFERING_WHEN_PLAYING)
            setBackgroundColor(Color.TRANSPARENT)
            layoutParams = LinearLayout.LayoutParams(-1, 150)
        }
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_HORIZONTAL
            setBackgroundColor(Color.rgb(13, 15, 20))
            addView(createTopBar(title), LinearLayout.LayoutParams(-1, dp(64)))
            addView(createArtwork(), LinearLayout.LayoutParams(dp(220), dp(220)).apply { topMargin = dp(56) })
            addView(createTitle(title), LinearLayout.LayoutParams(-1, -2).apply {
                topMargin = dp(28)
                leftMargin = dp(24)
                rightMargin = dp(24)
            })
            addView(playerView, LinearLayout.LayoutParams(-1, dp(150)).apply { topMargin = dp(24) })
        }
        setContentView(root)
    }

    private fun createTopBar(title: String): TextView = TextView(this).apply {
        text = "‹   $title"
        textSize = 17f
        setTextColor(Color.WHITE)
        gravity = Gravity.CENTER_VERTICAL
        setPadding(dp(20), 0, dp(20), 0)
        maxLines = 1
        ellipsize = android.text.TextUtils.TruncateAt.END
        setOnClickListener { finish() }
    }

    private fun createArtwork(): ImageView = ImageView(this).apply {
        setImageResource(android.R.drawable.ic_media_play)
        setColorFilter(Color.WHITE)
        setBackgroundColor(Color.rgb(228, 68, 101))
        scaleType = ImageView.ScaleType.CENTER
        contentDescription = "Audio"
    }

    private fun createTitle(title: String): TextView = TextView(this).apply {
        text = title
        textSize = 20f
        setTextColor(Color.WHITE)
        gravity = Gravity.CENTER
        maxLines = 2
        ellipsize = android.text.TextUtils.TruncateAt.END
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
        const val EXTRA_URL = "audio_url"
        const val EXTRA_TOKEN = "audio_token"
        const val EXTRA_TITLE = "audio_title"
    }
}
