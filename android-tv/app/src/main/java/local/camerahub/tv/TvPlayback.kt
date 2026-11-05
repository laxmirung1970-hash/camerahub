package local.camerahub.tv

import android.net.Uri
import android.os.Handler
import android.os.SystemClock
import android.view.ViewGroup
import android.widget.FrameLayout
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackException
import androidx.media3.common.PlaybackParameters
import androidx.media3.common.Player
import androidx.media3.common.VideoSize
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.datasource.HttpDataSource
import androidx.media3.exoplayer.DefaultLoadControl
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.exoplayer.source.ProgressiveMediaSource
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView

@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
class TvPlayback(
    private val tile: TvCameraTile,
    camera: TvCamera,
    private val server: String,
    token: String,
    /** "high", "low" or "small" (low scaled to 640 wide by the hub for the grid). */
    private val quality: String,
    private val demo: Boolean,
    private val ui: Handler,
    private val changed: () -> Unit,
    private val sessionExpired: () -> Unit,
    private val tooLarge: () -> Unit = {},
) {
    private var camera = camera
    private var disposed = false
    private var failed = false
    private var firstFrame = false
    private var attempt = 0
    private var retryAt = 0L
    private var startedAt = SystemClock.elapsedRealtime()
    private var lastProgressAt = startedAt
    private var lastPosition = -1L
    private var problemTitle = ""
    private var problemDetail = ""
    /** Continuous MP4 first; HLS from go2rtc buffers only about a second. Falls back for this tile on 404. */
    private var useMp4 = true
    private val playerView = PlayerView(tile.context).apply {
        useController = false; isFocusable = false; isFocusableInTouchMode = false
        descendantFocusability = ViewGroup.FOCUS_BLOCK_DESCENDANTS
        setShowBuffering(PlayerView.SHOW_BUFFERING_NEVER); setKeepContentOnPlayerReset(false)
        resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
        setShutterBackgroundColor(android.graphics.Color.BLACK)
    }
    // The hub answers once the camera is connected, which can take 20 s for Xiaomi P2P.
    private val http = DefaultHttpDataSource.Factory()
        .setDefaultRequestProperties(mapOf("Authorization" to "Bearer $token"))
        .setConnectTimeoutMs(8000).setReadTimeoutMs(25_000).setAllowCrossProtocolRedirects(false)
    private val hls = HlsMediaSource.Factory(http)
    private val mp4 = ProgressiveMediaSource.Factory(http)
    private val player = ExoPlayer.Builder(tile.context)
        .setRenderersFactory(DefaultRenderersFactory(tile.context).setEnableDecoderFallback(true))
        .setLoadControl(DefaultLoadControl.Builder().setBufferDurationsMs(3000, 10_000, 1000, 2500).build())
        .build()
    private val blackBuffer = Runnable {
        if (!disposed && !failed && player.playbackState == Player.STATE_BUFFERING) tile.showProblem("ภาพสะดุด", "กำลังรอข้อมูลจากกล้อง", true)
    }
    private val watchdog = object : Runnable {
        override fun run() {
            if (disposed) return
            val now = SystemClock.elapsedRealtime()
            when {
                retryAt > 0 && now >= retryAt -> start()
                retryAt > 0 -> tile.showProblem(problemTitle, "$problemDetail\nลองใหม่ใน ${((retryAt - now + 999) / 1000).coerceAtLeast(1)} วินาที")
                !failed && !camera.accountBlocked -> {
                    val position = player.currentPosition
                    if (position != lastPosition) { lastPosition = position; lastProgressAt = now }
                    if (useMp4 && firstFrame) catchUp()
                    if (!firstFrame && now - startedAt >= 20_000) fail("ยังไม่ได้รับภาพ", "เครื่องกลางหรือกล้องตอบช้า · ระบบจะลองใหม่")
                    else if (firstFrame && now - lastProgressAt >= 20_000) fail("ภาพหยุดหรือการเชื่อมต่อหลุด", "กำลังเชื่อมต่อกล้องใหม่")
                }
            }
            ui.postDelayed(this, 1000)
        }
    }
    init {
        tile.videoHost.addView(playerView, FrameLayout.LayoutParams(-1, -1)); playerView.player = player
        volume(false)
        player.addListener(object : Player.Listener {
            override fun onRenderedFirstFrame() {
                if (disposed || failed) return
                firstFrame = true; lastProgressAt = SystemClock.elapsedRealtime(); publishIfPlaying()
            }
            /** The stick cannot decode four 1080p streams; ask the hub for a scaled one. */
            override fun onVideoSizeChanged(videoSize: VideoSize) {
                if (!disposed && quality == "low" && videoSize.width > 1280) tooLarge()
            }
            override fun onIsPlayingChanged(isPlaying: Boolean) { if (!disposed && isPlaying) publishIfPlaying() }
            override fun onPlaybackStateChanged(state: Int) {
                if (disposed || failed) return
                if (state == Player.STATE_BUFFERING) {
                    tile.showBuffering(); changed(); ui.removeCallbacks(blackBuffer); ui.postDelayed(blackBuffer, 1200)
                } else if (state == Player.STATE_READY) { ui.removeCallbacks(blackBuffer); publishIfPlaying() }
                else if (state == Player.STATE_ENDED) {
                    // A long MP4 stream that ends was dropped by the hub or camera; reopen at once.
                    if (useMp4 && firstFrame && SystemClock.elapsedRealtime() - startedAt > 30_000) start()
                    else fail("ภาพหยุดแล้ว", "ระบบจะลองเปิดภาพสดใหม่")
                }
            }
            override fun onPlayerError(error: PlaybackException) {
                if (disposed) return
                var cause: Throwable? = error
                var http: Int? = null
                while (cause != null) {
                    if (cause is HttpDataSource.InvalidResponseCodeException) { http = cause.responseCode; break }
                    cause = cause.cause
                }
                when {
                    useMp4 && (http == 404 || error.errorCode in 3000..3999) -> { useMp4 = false; start() }
                    http == 401 || http == 403 -> { fail("สิทธิ์ทีวีใช้ไม่ได้", "กรุณาจับคู่ทีวีกับเครื่องกลางใหม่", false); sessionExpired() }
                    error.errorCode in 4000..4999 -> fail("ทีวีถอดรหัสช่องนี้ไม่ได้", "ลองดูเดี่ยวหรือคุณภาพปกติ · ตรวจรูปแบบภาพ/เสียงที่เครื่องกลาง")
                    http == 404 -> fail("ไม่พบกล้องในเครื่องกลาง", "กำลังปรับปรุงรายชื่อกล้อง")
                    http == 429 || http == 503 -> fail("เครื่องกลางยังไม่พร้อม", "กำลังรอช่องภาพ · ระบบจะลองใหม่")
                    else -> fail("ยังเปิดภาพไม่ได้", camera.problem.ifBlank { "ตรวจไฟกล้องและการเชื่อมต่อเครื่องกลาง" })
                }
            }
        })
        if (camera.accountBlocked) blocked()
        else if (camera.status == "offline") fail("กล้องออฟไลน์", camera.problem, initialDelay = 30_000)
        else start()
        ui.postDelayed(watchdog, 1000)
    }
    private fun publishIfPlaying() {
        if (disposed || failed || !firstFrame || !player.isPlaying) return
        val wasLive = tile.isLive
        tile.showLive(demo); attempt = 0; problemTitle = ""; problemDetail = ""
        if (!wasLive) changed()
    }
    private fun start() {
        if (disposed || camera.accountBlocked) return
        retryAt = 0; failed = false; firstFrame = false
        startedAt = SystemClock.elapsedRealtime(); lastProgressAt = startedAt; lastPosition = -1
        if (problemTitle.isBlank()) tile.showProblem("กำลังเปิดภาพ", "เชื่อมต่อผ่านเครื่องกลาง", true)
        else tile.showProblem(problemTitle, "$problemDetail\nกำลังลองเชื่อมต่อใหม่")
        changed()
        val base = "$server/api/cameras/${Uri.encode(camera.id)}"
        val source = if (useMp4) mp4.createMediaSource(MediaItem.fromUri("$base/mp4?quality=$quality"))
            else hls.createMediaSource(MediaItem.Builder().setUri("$base/hls?quality=$quality").setMimeType(MimeTypes.APPLICATION_M3U8).build())
        player.playbackParameters = PlaybackParameters.DEFAULT
        player.setMediaSource(source, true); player.prepare(); player.playWhenReady = true
    }
    /** MP4 arrives in real time, so what is buffered is how far behind live the tile is. */
    private fun catchUp() {
        val speed = TvLiveCatchUp.speed(player.totalBufferedDuration, player.playbackParameters.speed)
        if (speed != player.playbackParameters.speed) player.playbackParameters = PlaybackParameters(speed)
    }
    private fun fail(title: String, detail: String, retry: Boolean = true, initialDelay: Long? = null) {
        if (disposed) return
        failed = true; ui.removeCallbacks(blackBuffer)
        problemTitle = title; problemDetail = detail
        player.stop(); tile.showProblem(title, detail); changed()
        retryAt = if (retry && !camera.accountBlocked) SystemClock.elapsedRealtime() + (initialDelay ?: TvRetryPolicy.delayMs(attempt++)) else 0
    }
    private fun blocked() = fail("ต้องตั้งค่าบัญชีกล้อง", camera.problem, false)
    fun update(next: TvCamera) {
        val previous = camera; camera = next
        if (previous.status != next.status) {
            if (next.accountBlocked) blocked()
            else if (previous.accountBlocked || (previous.status == "offline" && next.status != "offline" && !tile.isLive)) start()
        }
    }
    fun retry() { if (camera.accountBlocked) blocked() else start() }
    /** Muted tiles skip audio decoding entirely; the stick's CPU is the limit with four tiles. */
    fun volume(audible: Boolean) {
        if (disposed) return
        player.volume = if (audible) 1f else 0f
        val params = player.trackSelectionParameters
        if (params.disabledTrackTypes.contains(C.TRACK_TYPE_AUDIO) == !audible) return
        player.trackSelectionParameters = params.buildUpon().setTrackTypeDisabled(C.TRACK_TYPE_AUDIO, !audible).build()
    }
    fun release() {
        disposed = true; ui.removeCallbacks(watchdog); ui.removeCallbacks(blackBuffer)
        playerView.player = null; player.release(); tile.videoHost.removeView(playerView)
    }
}
