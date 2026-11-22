package io.github.mosheshaul.homestreamer.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder

data class Subtitle(
    val id: String,
    val label: String,
    val language: String?,
    val forced: Boolean,
    val url: String,
) {
    fun toJson(): JSONObject = JSONObject()
        .put("id", id).put("label", label).put("language", language ?: "")
        .put("forced", forced).put("url", url)

    companion object {
        fun fromJson(o: JSONObject) = Subtitle(
            id = o.getString("id"),
            label = o.optString("label", "Subtitles"),
            language = o.optString("language").ifEmpty { null },
            forced = o.optBoolean("forced", false),
            url = o.getString("url"),
        )
    }
}

data class Item(
    val type: String,
    val id: String,
    val name: String,
    val fileName: String? = null,
    val size: Long = 0,
    val mimeType: String? = null,
    val streamUrl: String? = null,
    val subtitles: List<Subtitle> = emptyList(),
) {
    val isFolder get() = type == "folder"

    fun toJson(): JSONObject = JSONObject()
        .put("type", type).put("id", id).put("name", name).put("size", size)
        .put("fileName", fileName).put("mimeType", mimeType).put("streamUrl", streamUrl)
        .put("subtitles", JSONArray(subtitles.map { it.toJson() }))

    companion object {
        fun fromJson(o: JSONObject): Item {
            val subs = o.optJSONArray("subtitles")
            return Item(
                type = o.getString("type"),
                id = o.getString("id"),
                name = o.getString("name"),
                fileName = o.optString("fileName").ifEmpty { null },
                size = o.optLong("size", 0),
                mimeType = o.optString("mimeType").ifEmpty { null },
                streamUrl = o.optString("streamUrl").ifEmpty { null },
                subtitles = if (subs == null) emptyList()
                else (0 until subs.length()).map { Subtitle.fromJson(subs.getJSONObject(it)) },
            )
        }
    }
}

/** A video in "Continue watching", as saved on the PC. */
data class RemoteProgress(val item: Item, val positionMs: Long, val durationMs: Long)

data class Listing(val id: String, val title: String, val items: List<Item>)

data class ServerInfo(val name: String, val version: String)

data class EmbeddedSubtitle(
    val index: Int,
    val label: String,
    val language: String?,
    val forced: Boolean,
    val isDefault: Boolean,
    val text: Boolean,
    val ready: Boolean,
    val url: String?,
)

/** What's inside a video file, from the server's FFprobe. */
data class MediaInfo(
    val duration: Double,
    val bitrate: Long,
    val videoCodec: String,
    val width: Int,
    val height: Int,
    val hdr: Boolean,
    val dolbyVision: Boolean,
    val defaultAudio: Int,
    val embeddedSubtitles: List<EmbeddedSubtitle>,
    val canTranscode: Boolean,
    /** Percent done while the PC extracts subtitles, or -1. */
    val subtitlesProgress: Int,
)

/** Talks to the Home Streamer server on the PC. [baseUrl] looks like http://192.168.1.20:8095 */
class Api(val baseUrl: String) {

    fun absolute(path: String): String =
        if (path.startsWith("http")) path else baseUrl.trimEnd('/') + path

    suspend fun info(timeoutMs: Int = 3000): ServerInfo {
        val o = JSONObject(get("/api/info", timeoutMs = timeoutMs))
        return ServerInfo(o.optString("name", "PC"), o.optString("version", ""))
    }

    suspend fun browse(id: String): Listing {
        val o = JSONObject(get("/api/browse?id=" + URLEncoder.encode(id, "UTF-8")))
        val arr = o.getJSONArray("items")
        return Listing(
            id = o.optString("id", ""),
            title = o.optString("title", ""),
            items = (0 until arr.length()).map { Item.fromJson(arr.getJSONObject(it)) },
        )
    }

    suspend fun media(id: String): MediaInfo {
        val o = JSONObject(get("/api/media/$id", timeoutMs = 60000))
        val audio = o.optJSONArray("audio")
        var defaultAudio = 0
        if (audio != null) {
            for (i in 0 until audio.length()) {
                if (audio.getJSONObject(i).optBoolean("default")) { defaultAudio = i; break }
            }
        }
        val subs = o.optJSONArray("embeddedSubtitles")
        return MediaInfo(
            duration = o.optDouble("duration", 0.0),
            bitrate = o.optLong("bitrate", 0),
            videoCodec = o.optString("videoCodec", ""),
            width = o.optInt("width", 0),
            height = o.optInt("height", 0),
            hdr = o.optBoolean("hdr", false),
            dolbyVision = o.optBoolean("dolbyVision", false),
            defaultAudio = defaultAudio,
            embeddedSubtitles = if (subs == null) emptyList() else (0 until subs.length()).map {
                val e = subs.getJSONObject(it)
                EmbeddedSubtitle(
                    index = e.optInt("index"),
                    label = e.optString("label", "Subtitles"),
                    language = e.optString("language").ifEmpty { null },
                    forced = e.optBoolean("forced", false),
                    isDefault = e.optBoolean("default", false),
                    text = e.optBoolean("text", false),
                    ready = e.optBoolean("ready", false),
                    url = e.optString("url").ifEmpty { null },
                )
            },
            canTranscode = o.optBoolean("canTranscode", false),
            subtitlesProgress = o.optInt("subtitlesProgress", -1),
        )
    }

    /** "Continue watching", shared by every device. Most recent first. */
    suspend fun progress(): List<RemoteProgress> {
        val arr = JSONArray(get("/api/progress", timeoutMs = 6000))
        return (0 until arr.length()).mapNotNull { i ->
            runCatching {
                val o = arr.getJSONObject(i)
                RemoteProgress(
                    item = Item.fromJson(o.getJSONObject("item")),
                    positionMs = (o.optDouble("position", 0.0) * 1000).toLong(),
                    durationMs = (o.optDouble("duration", 0.0) * 1000).toLong(),
                )
            }.getOrNull()
        }
    }

    suspend fun putProgress(item: Item, positionMs: Long, durationMs: Long) = send(
        "PUT", "/api/progress/${item.id}",
        JSONObject().put("position", positionMs / 1000.0).put("duration", durationMs / 1000.0).put("item", item.toJson()),
    )

    suspend fun deleteProgress(id: String) = send("DELETE", "/api/progress/$id", null)

    private suspend fun send(method: String, path: String, body: JSONObject?) = withContext(Dispatchers.IO) {
        val conn = URL(absolute(path)).openConnection() as HttpURLConnection
        try {
            conn.connectTimeout = 8000
            conn.readTimeout = 8000
            conn.requestMethod = method
            if (body != null) {
                conn.doOutput = true
                conn.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                conn.outputStream.use { it.write(body.toString().toByteArray(Charsets.UTF_8)) }
            }
            if (conn.responseCode !in 200..299) throw java.io.IOException("Server answered ${conn.responseCode}")
        } finally {
            conn.disconnect()
        }
    }

    /** Whether the PC shows Israeli titles in Hebrew (it has a TMDB key). */
    suspend fun hebrewTitles(): Boolean =
        JSONObject(get("/api/settings", timeoutMs = 6000)).optBoolean("hebrewTitles", false)

    /** Sends a TMDB key to the PC, which checks and saves it. Returns null on success, else the PC's message. */
    suspend fun setTmdbKey(key: String): String? = withContext(Dispatchers.IO) {
        val conn = URL(absolute("/api/settings/tmdb")).openConnection() as HttpURLConnection
        try {
            conn.connectTimeout = 8000
            conn.readTimeout = 20000
            conn.requestMethod = "PUT"
            conn.doOutput = true
            conn.setRequestProperty("Content-Type", "application/json; charset=utf-8")
            conn.outputStream.use { it.write(JSONObject().put("key", key.trim()).toString().toByteArray(Charsets.UTF_8)) }
            val code = conn.responseCode
            if (code in 200..299) null
            else conn.errorStream?.bufferedReader(Charsets.UTF_8)?.use { it.readText().trim() }?.ifBlank { null }
                ?: "The PC answered $code. Update HomeStreamer.exe on the PC to the latest version."
        } finally {
            conn.disconnect()
        }
    }

    private suspend fun get(path: String, timeoutMs: Int = 15000): String = withContext(Dispatchers.IO) {
        val conn = URL(absolute(path)).openConnection() as HttpURLConnection
        conn.connectTimeout = timeoutMs
        conn.readTimeout = timeoutMs
        try {
            val code = conn.responseCode
            if (code !in 200..299) throw java.io.IOException("Server answered $code")
            conn.inputStream.bufferedReader(Charsets.UTF_8).use { it.readText() }
        } finally {
            conn.disconnect()
        }
    }

    companion object {
        /**
         * Picks the address that works right now: the home address first (fast
         * when on the home network), then the away address (Tailscale).
         */
        suspend fun resolve(home: String?, away: String?): String? {
            val candidates = listOfNotNull(home, away).distinct()
            for ((i, url) in candidates.withIndex()) {
                val ok = runCatching { Api(url).info(timeoutMs = if (i == 0 && candidates.size > 1) 1500 else 5000) }.isSuccess
                if (ok) return url
            }
            return null
        }

        /** True for an address in Tailscale's 100.64.0.0/10 range. */
        fun isTailscale(url: String): Boolean {
            val host = url.substringAfter("://").substringBefore(':').substringBefore('/')
            val parts = host.split('.').mapNotNull { it.toIntOrNull() }
            return parts.size == 4 && parts[0] == 100 && parts[1] in 64..127
        }

        /** Accepts "192.168.1.20", "192.168.1.20:8095" or a full URL. */
        fun normalize(input: String): String {
            var s = input.trim().trimEnd('/')
            if (!s.startsWith("http://") && !s.startsWith("https://")) s = "http://$s"
            val hostPart = s.substringAfter("://")
            if (!hostPart.contains(':')) s = "$s:8095"
            return s
        }
    }
}
