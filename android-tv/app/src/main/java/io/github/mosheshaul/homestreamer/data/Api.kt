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
    val size: Long = 0,
    val mimeType: String? = null,
    val streamUrl: String? = null,
    val subtitles: List<Subtitle> = emptyList(),
) {
    val isFolder get() = type == "folder"

    fun toJson(): JSONObject = JSONObject()
        .put("type", type).put("id", id).put("name", name).put("size", size)
        .put("mimeType", mimeType ?: "").put("streamUrl", streamUrl ?: "")
        .put("subtitles", JSONArray(subtitles.map { it.toJson() }))

    companion object {
        fun fromJson(o: JSONObject): Item {
            val subs = o.optJSONArray("subtitles")
            return Item(
                type = o.getString("type"),
                id = o.getString("id"),
                name = o.getString("name"),
                size = o.optLong("size", 0),
                mimeType = o.optString("mimeType").ifEmpty { null },
                streamUrl = o.optString("streamUrl").ifEmpty { null },
                subtitles = if (subs == null) emptyList()
                else (0 until subs.length()).map { Subtitle.fromJson(subs.getJSONObject(it)) },
            )
        }
    }
}

data class Listing(val id: String, val title: String, val items: List<Item>)

data class ServerInfo(val name: String, val version: String)

/** Talks to the Home Streamer server on the PC. [baseUrl] looks like http://192.168.1.20:8095 */
class Api(val baseUrl: String) {

    fun absolute(path: String): String =
        if (path.startsWith("http")) path else baseUrl.trimEnd('/') + path

    suspend fun info(): ServerInfo {
        val o = JSONObject(get("/api/info", timeoutMs = 3000))
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
