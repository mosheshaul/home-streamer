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
import io.github.mosheshaul.homestreamer.R
import io.github.mosheshaul.homestreamer.data.Api
import io.github.mosheshaul.homestreamer.data.Item
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
        prefs = Prefs(this)
        item = Item.fromJson(JSONObject(intent.getStringExtra(EXTRA_ITEM)!!))
        api = Api(intent.getStringExtra(EXTRA_SERVER)!!)
        playerView = findViewById(R.id.player_view)
        styleSubtitles(playerView.subtitleView)
        initPlayer()
    }

    private fun initPlayer() {
        // Prefer the TV's own decoders and fall back to the bundled FFmpeg audio
        // decoders for formats like DTS or TrueHD that many TVs can't play.
        val renderers = DefaultRenderersFactory(this)
            .setExtensionRendererMode(DefaultRenderersFactory.EXTENSION_RENDERER_MODE_ON)
            .setEnableDecoderFallback(true)
        val p = ExoPlayer.Builder(this, renderers).build()
        player = p
        playerView.player = p
        playerView.keepScreenOn = true

        p.trackSelectionParameters = buildTrackParams(p.trackSelectionParameters)

        val subs = item.subtitles.map { s ->
            MediaItem.SubtitleConfiguration.Builder(Uri.parse(api.absolute(s.url)))
                .setMimeType(MimeTypes.APPLICATION_SUBRIP)
                .setLanguage(s.language)
                .setLabel(s.label)
                .setSelectionFlags(if (s.forced) C.SELECTION_FLAG_FORCED else 0)
                .setId("sidecar:${s.id}")
                .build()
        }
        val mediaItem = MediaItem.Builder()
            .setUri(api.absolute(item.streamUrl ?: ""))
            .setMimeType(item.mimeType)
            .setSubtitleConfigurations(subs)
            .setMediaId(item.id)
            .build()
        p.setMediaItem(mediaItem)

        p.addListener(object : Player.Listener {
            override fun onTracksChanged(tracks: Tracks) = rememberSubtitleChoice(tracks)

            override fun onPlaybackStateChanged(state: Int) {
                if (state == Player.STATE_ENDED) {
                    prefs.saveResume(item.id, p.duration, p.duration, item)
                    finish()
                }
            }

            override fun onPlayerError(error: PlaybackException) {
                Toast.makeText(
                    this@PlayerActivity,
                    "Can't play this video: ${error.errorCodeName}",
                    Toast.LENGTH_LONG,
                ).show()
            }
        })

        val resume = prefs.resumePosition(item.id)
        if (resume > 0) {
            AlertDialog.Builder(this)
                .setTitle(item.name)
                .setItems(arrayOf("Resume from ${formatTime(resume)}", "Start from the beginning")) { _, which ->
                    if (which == 0) p.seekTo(resume)
                    startPlayback(p)
                }
                .setOnCancelListener { finish() }
                .show()
        } else {
            startPlayback(p)
        }
    }

    private fun startPlayback(p: ExoPlayer) {
        p.prepare()
        p.playWhenReady = true
        handler.postDelayed(saveTicker, 10_000)
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
        player?.release()
        player = null
        super.onDestroy()
    }

    companion object {
        private const val EXTRA_ITEM = "item"
        private const val EXTRA_SERVER = "server"

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
