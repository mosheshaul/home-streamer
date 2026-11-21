package io.github.mosheshaul.homestreamer.player

import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.graphics.Typeface
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.KeyEvent
import android.view.WindowManager
import android.widget.Toast
import androidx.annotation.OptIn
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.TrackSelectionParameters
import androidx.media3.common.Tracks
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.CaptionStyleCompat
import androidx.media3.ui.PlayerView
import androidx.media3.ui.SubtitleView
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import io.github.mosheshaul.homestreamer.R
import io.github.mosheshaul.homestreamer.data.Api
import io.github.mosheshaul.homestreamer.data.Item
import io.github.mosheshaul.homestreamer.data.MediaInfo
import kotlinx.coroutines.Job
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import io.github.mosheshaul.homestreamer.data.Prefs
import org.json.JSONObject

@OptIn(UnstableApi::class)
class PlayerActivity : Activity() {

    private lateinit var prefs: Prefs
    private lateinit var item: Item
    private lateinit var api: Api
    private lateinit var playerView: PlayerView
    private var player: ExoPlayer? = null
    private val handler = Handler(Looper.getMainLooper())
    private var lastTextSelection: String? = null
    private val scope = MainScope()
    private var media: MediaInfo? = null
    private var converting = false

    private val saveTicker = object : Runnable {
        override fun run() {
            saveProgress()
            handler.postDelayed(this, 10_000)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        setContentView(R.layout.activity_player)
        hideSystemBars()
        prefs = Prefs(this)
        item = Item.fromJson(JSONObject(intent.getStringExtra(EXTRA_ITEM)!!))
        api = Api(intent.getStringExtra(EXTRA_SERVER)!!)
        playerView = findViewById(R.id.player_view)
        styleSubtitles(playerView.subtitleView)
        initPlayer()
    }

    private fun initPlayer() {
        // Prefer the device's own decoders and fall back to the bundled FFmpeg audio
        // decoders for formats like DTS or TrueHD that many TVs can't play.
        val renderers = DefaultRenderersFactory(this)
            .setExtensionRendererMode(DefaultRenderersFactory.EXTENSION_RENDERER_MODE_ON)
            .setEnableDecoderFallback(true)
        val p = ExoPlayer.Builder(this, renderers).build()
        player = p
        playerView.player = p
        playerView.keepScreenOn = true
        p.trackSelectionParameters = buildTrackParams(p.trackSelectionParameters)

        p.addListener(object : Player.Listener {
            override fun onTracksChanged(tracks: Tracks) {
                rememberSubtitleChoice(tracks)
                // No decoder at all for the video: ExoPlayer would play sound only.
                val video = tracks.groups.filter { it.type == C.TRACK_TYPE_VIDEO }
                if (video.isNotEmpty() && video.none { it.isSelected } && video.none { it.isSupported }) {
                    handleCantDecode("This device has no decoder for this video")
                }
            }

            override fun onPlaybackStateChanged(state: Int) {
                if (state == Player.STATE_ENDED) {
                    prefs.saveResume(item.id, p.duration, p.duration, item)
                    finish()
                }
            }

            override fun onPlayerError(error: PlaybackException) = handleError(error)
        })

        scope.launch {
            // Ask the PC what's inside the file, to decide whether to convert it.
            media = runCatching { api.media(item.id) }.getOrNull()
            converting = shouldConvert(media)
            val resume = prefs.resumePosition(item.id)
            if (resume > 0) {
                AlertDialog.Builder(this@PlayerActivity)
                    .setTitle(item.name)
                    .setItems(arrayOf("Resume from ${formatTime(resume)}", "Start from the beginning")) { _, which ->
                        load(if (which == 0) resume else 0L)
                        handler.postDelayed(saveTicker, 10_000)
                    }
                    .setOnCancelListener { finish() }
                    .show()
            } else {
                load(0L)
                handler.postDelayed(saveTicker, 10_000)
            }
        }
    }

    private val isAway: Boolean
        get() = api.baseUrl == prefs.remoteUrl && api.baseUrl != prefs.serverUrl

    /** Decide between streaming the original file and converting it on the PC. */
    private fun shouldConvert(m: MediaInfo?): Boolean {
        if (m == null || !m.canTranscode) return false
        return when (prefs.quality) {
            "original" -> false
            "1080", "720" -> true
            else -> m.videoCodec in legacyCodecs ||
                !DeviceCodecs.canDecode(m) ||
                (isAway && m.bitrate > AWAY_MAX_BITRATE)
        }
    }

    private fun sidecarSubs(): MutableList<MediaItem.SubtitleConfiguration> = item.subtitles.map { s ->
        MediaItem.SubtitleConfiguration.Builder(Uri.parse(api.absolute(s.url)))
            .setMimeType(MimeTypes.APPLICATION_SUBRIP)
            .setLanguage(s.language)
            .setLabel(s.label)
            .setSelectionFlags(if (s.forced) C.SELECTION_FLAG_FORCED else 0)
            .setId("sidecar:${s.id}")
            .build()
    }.toMutableList()

    /** (Re)load the video at [positionMs], as the original file or converted. */
    private fun load(positionMs: Long) {
        val p = player ?: return
        val subs = sidecarSubs()
        val builder = MediaItem.Builder().setMediaId(item.id)
        val m = media
        if (converting && m != null) {
            val (h, br) = when (prefs.quality) {
                "720" -> 720 to 3000
                "1080" -> 1080 to 8000
                else -> if (isAway) 1080 to 6000 else 1080 to 8000
            }
            builder.setUri(api.absolute("/hls/${item.id}/index.m3u8?h=$h&br=$br&a=${m.defaultAudio}"))
                .setMimeType(MimeTypes.APPLICATION_M3U8)
            // Converted video has no subtitle tracks, so add the video's own
            // text subtitles as separate files (the PC extracts them once).
            var pending = false
            for (e in m.embeddedSubtitles) {
                if (!e.text || e.url == null) continue
                if (!e.ready) { pending = true; continue }
                subs += MediaItem.SubtitleConfiguration.Builder(Uri.parse(api.absolute(e.url)))
                    .setMimeType(MimeTypes.APPLICATION_SUBRIP)
                    .setLanguage(e.language)
                    .setLabel(e.label)
                    .setSelectionFlags(if (e.forced) C.SELECTION_FLAG_FORCED else 0)
                    .setId("embedded:${e.index}")
                    .build()
            }
            if (pending) waitForEmbeddedSubs()
        } else {
            builder.setUri(api.absolute(item.streamUrl ?: "")).setMimeType(item.mimeType)
        }
        builder.setSubtitleConfigurations(subs)
        lastTextSelection = null
        p.setMediaItem(builder.build(), positionMs)
        p.prepare()
        p.playWhenReady = true
    }

    private var subsJob: Job? = null

    /** Poll until the PC finishes extracting subtitles, then add them. */
    private fun waitForEmbeddedSubs() {
        if (subsJob?.isActive == true) return
        Toast.makeText(this, "Preparing the video's subtitles on the PC…", Toast.LENGTH_SHORT).show()
        subsJob = scope.launch {
            repeat(120) {
                delay(5_000)
                val m = runCatching { api.media(item.id) }.getOrNull() ?: return@repeat
                if (m.embeddedSubtitles.none { it.text && !it.ready }) {
                    media = m
                    val pos = player?.currentPosition ?: 0L
                    if (converting) {
                        load(pos)
                        Toast.makeText(this@PlayerActivity, "Subtitles are ready.", Toast.LENGTH_SHORT).show()
                    }
                    return@launch
                }
            }
        }
    }

    private fun handleError(error: PlaybackException) {
        val decodeProblem = error.errorCode in decodeErrors
        when {
            decodeProblem && !converting -> handleCantDecode(error.errorCodeName)
            error.errorCode in networkErrors ->
                showFatal("Lost the connection to the PC. Check that HomeStreamer is running and, away from home, that Tailscale is on.")
            converting ->
                showFatal("The PC couldn't convert this video (${error.errorCodeName}). Check the HomeStreamer window on the PC for details.")
            else -> showFatal("Can't play this video (${error.errorCodeName}).")
        }
    }

    private var triedConverting = false

    /** The device can't decode the original: switch to conversion on the PC. */
    private fun handleCantDecode(reason: String) {
        if (converting || triedConverting) return
        triedConverting = true
        val pos = player?.currentPosition ?: 0L
        scope.launch {
            val m = media ?: runCatching { api.media(item.id) }.getOrNull()
            media = m
            if (m != null && m.canTranscode) {
                Toast.makeText(
                    this@PlayerActivity,
                    "This device can't play this video directly. Converting it on the PC…",
                    Toast.LENGTH_LONG,
                ).show()
                converting = true
                load(pos)
            } else {
                showFatal(
                    "This device can't play this video ($reason${m?.let { ", " + describeVideo(it) } ?: ""}).\n\n" +
                        "HomeStreamer can convert it on the PC if FFmpeg is installed there. On the PC, open a terminal and run:\n" +
                        "winget install Gyan.FFmpeg\nThen restart HomeStreamer."
                )
            }
        }
    }

    private fun describeVideo(m: MediaInfo): String {
        val res = when {
            m.height >= 1600 || m.width >= 3000 -> "4K"
            m.height >= 700 -> "HD"
            else -> "${m.height}p"
        }
        val hdr = if (m.dolbyVision) " Dolby Vision" else if (m.hdr) " HDR" else ""
        return "$res$hdr ${m.videoCodec.uppercase()}"
    }

    private var fatalShown = false

    private fun showFatal(message: String) {
        if (fatalShown || isFinishing) return
        fatalShown = true
        player?.pause()
        AlertDialog.Builder(this)
            .setTitle(item.name)
            .setMessage(message)
            .setPositiveButton("OK") { _, _ -> finish() }
            .setOnCancelListener { finish() }
            .show()
    }

    /** Full screen on phones and tablets: hide the status and navigation bars. */
    private fun hideSystemBars() {
        WindowCompat.setDecorFitsSystemWindows(window, false)
        WindowInsetsControllerCompat(window, window.decorView).apply {
            hide(WindowInsetsCompat.Type.systemBars())
            systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (hasFocus) hideSystemBars()
    }

    /** Apply the saved subtitle language, or turn subtitles off. */
    private fun buildTrackParams(base: TrackSelectionParameters): TrackSelectionParameters {
        val b = base.buildUpon()
        when (val lang = prefs.subtitleLanguage) {
            null -> {}
            "off" -> b.setIgnoredTextSelectionFlags(C.SELECTION_FLAG_DEFAULT)
            else -> b.setPreferredTextLanguage(lang).setSelectUndeterminedTextLanguage(false)
        }
        return b.build()
    }

    /**
     * When the viewer picks a subtitle track in the player menu, remember its
     * language for next time. The first track report after loading is the
     * automatic choice, so it is not saved.
     */
    private fun rememberSubtitleChoice(tracks: Tracks) {
        val textGroups = tracks.groups.filter { it.type == C.TRACK_TYPE_TEXT }
        if (textGroups.isEmpty()) return
        val selected = textGroups.firstNotNullOfOrNull { g ->
            (0 until g.length).firstOrNull { g.isTrackSelected(it) }?.let { g.getTrackFormat(it) }
        }
        val key = selected?.let { "${it.id}|${it.language}" } ?: "none"
        val previous = lastTextSelection
        lastTextSelection = key
        // The first report is the automatic choice; only a change is the viewer's choice.
        if (previous == null || previous == key) return
        if (selected != null) {
            val lang = selected.language?.lowercase()
            if (!lang.isNullOrEmpty() && lang != "und" && (selected.selectionFlags and C.SELECTION_FLAG_FORCED) == 0) {
                prefs.subtitleLanguage = normalizeLang(lang)
            }
        } else {
            prefs.subtitleLanguage = "off"
        }
    }

    private fun normalizeLang(l: String) = when (l) {
        "iw", "heb" -> "he"
        "eng" -> "en"
        else -> l
    }

    private fun styleSubtitles(view: SubtitleView?) {
        view ?: return
        view.setApplyEmbeddedStyles(true)
        view.setApplyEmbeddedFontSizes(false)
        view.setStyle(
            CaptionStyleCompat(
                Color.WHITE, Color.TRANSPARENT, Color.TRANSPARENT,
                CaptionStyleCompat.EDGE_TYPE_OUTLINE, Color.BLACK, Typeface.DEFAULT_BOLD,
            )
        )
        view.setFractionalTextSize(SubtitleView.DEFAULT_TEXT_SIZE_FRACTION * 1.15f * prefs.subtitleScale)
        view.setBottomPaddingFraction(0.06f)
    }

    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        val p = player
        if (p != null && event.action == KeyEvent.ACTION_DOWN && !playerView.isControllerFullyVisible) {
            when (event.keyCode) {
                // Left/right skip while the controls are hidden.
                KeyEvent.KEYCODE_DPAD_LEFT, KeyEvent.KEYCODE_MEDIA_REWIND -> {
                    p.seekTo((p.currentPosition - 10_000).coerceAtLeast(0)); return true
                }
                KeyEvent.KEYCODE_DPAD_RIGHT, KeyEvent.KEYCODE_MEDIA_FAST_FORWARD -> {
                    p.seekTo(p.currentPosition + 30_000); return true
                }
                KeyEvent.KEYCODE_CAPTIONS -> {
                    playerView.showController(); return true
                }
            }
        }
        return playerView.dispatchKeyEvent(event) || super.dispatchKeyEvent(event)
    }

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        if (playerView.isControllerFullyVisible) playerView.hideController()
        else super.onBackPressed()
    }

    private fun saveProgress() {
        val p = player ?: return
        if (p.duration > 0) prefs.saveResume(item.id, p.currentPosition, p.duration, item)
    }

    override fun onStop() {
        super.onStop()
        saveProgress()
        player?.pause()
    }

    override fun onDestroy() {
        handler.removeCallbacks(saveTicker)
        scope.cancel()
        player?.release()
        player = null
        super.onDestroy()
    }

    companion object {
        private const val EXTRA_ITEM = "item"
        private const val EXTRA_SERVER = "server"

        /** Away from home, files above this are converted to fit the connection. */
        private const val AWAY_MAX_BITRATE = 15_000_000L

        /** Old formats phones and TVs often can't play (DivX/Xvid AVIs, WMV, MPEG-2...). */
        private val legacyCodecs = setOf(
            "mpeg4", "msmpeg4v1", "msmpeg4v2", "msmpeg4v3", "wmv1", "wmv2", "wmv3", "vc1",
            "mpeg1video", "mpeg2video", "h263", "flv1", "vp6f", "rv30", "rv40", "theora", "mjpeg",
        )

        private val decodeErrors = setOf(
            PlaybackException.ERROR_CODE_DECODING_FAILED,
            PlaybackException.ERROR_CODE_DECODER_INIT_FAILED,
            PlaybackException.ERROR_CODE_DECODER_QUERY_FAILED,
            PlaybackException.ERROR_CODE_DECODING_FORMAT_EXCEEDS_CAPABILITIES,
            PlaybackException.ERROR_CODE_DECODING_FORMAT_UNSUPPORTED,
            PlaybackException.ERROR_CODE_PARSING_CONTAINER_UNSUPPORTED,
            PlaybackException.ERROR_CODE_PARSING_CONTAINER_MALFORMED,
            PlaybackException.ERROR_CODE_AUDIO_TRACK_INIT_FAILED,
        )

        private val networkErrors = setOf(
            PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_FAILED,
            PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_TIMEOUT,
            PlaybackException.ERROR_CODE_IO_BAD_HTTP_STATUS,
        )

        fun start(context: Context, server: String, item: Item) {
            context.startActivity(
                Intent(context, PlayerActivity::class.java)
                    .putExtra(EXTRA_SERVER, server)
                    .putExtra(EXTRA_ITEM, item.toJson().toString())
            )
        }

        fun formatTime(ms: Long): String {
            val s = ms / 1000
            return if (s >= 3600) "%d:%02d:%02d".format(s / 3600, s / 60 % 60, s % 60)
            else "%d:%02d".format(s / 60, s % 60)
        }
    }
}
