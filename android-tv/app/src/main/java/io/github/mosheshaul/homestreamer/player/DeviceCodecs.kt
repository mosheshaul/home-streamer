package io.github.mosheshaul.homestreamer.player

import android.media.MediaCodecInfo
import android.media.MediaCodecList
import android.os.Build
import io.github.mosheshaul.homestreamer.data.MediaInfo

/** Checks whether this device has a video decoder that can handle a file. */
object DeviceCodecs {
    private val mimes = mapOf(
        "hevc" to "video/hevc",
        "h264" to "video/avc",
        "av1" to "video/av01",
        "vp9" to "video/x-vnd.on2.vp9",
        "vp8" to "video/x-vnd.on2.vp8",
        "mpeg4" to "video/mp4v-es",
        "mpeg2video" to "video/mpeg2",
        "h263" to "video/3gpp",
    )

    private val decoders: List<MediaCodecInfo> by lazy {
        runCatching { MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos.filter { !it.isEncoder } }
            .getOrDefault(emptyList())
    }

    fun canDecode(m: MediaInfo): Boolean {
        if (m.width <= 0 || m.height <= 0) return true
        val mime = mimes[m.videoCodec] ?: return true // unknown: let the player try
        val tenBit = m.hdr
        val big = m.width > 1920 || m.height > 1088
        if (decoders.isEmpty()) return true
        return decoders.any { info ->
            if (info.supportedTypes.none { it.equals(mime, ignoreCase = true) }) return@any false
            // Software decoders can't keep up with 4K.
            if (big && Build.VERSION.SDK_INT >= 29 && !info.isHardwareAccelerated) return@any false
            val caps = runCatching { info.getCapabilitiesForType(mime) }.getOrNull() ?: return@any false
            val vc = caps.videoCapabilities ?: return@any false
            val sizeOk = runCatching { vc.areSizeAndRateSupported(m.width, m.height, 24.0) }.getOrDefault(false) ||
                runCatching { vc.areSizeAndRateSupported(m.height, m.width, 24.0) }.getOrDefault(false)
            if (!sizeOk) return@any false
            if (m.bitrate > 0 && vc.bitrateRange.upper < m.bitrate * 0.8) return@any false
            if (tenBit && mime == "video/hevc") {
                caps.profileLevels.any {
                    it.profile == MediaCodecInfo.CodecProfileLevel.HEVCProfileMain10 ||
                        it.profile == MediaCodecInfo.CodecProfileLevel.HEVCProfileMain10HDR10 ||
                        it.profile == MediaCodecInfo.CodecProfileLevel.HEVCProfileMain10HDR10Plus
                }
            } else {
                true
            }
        }
    }
}
