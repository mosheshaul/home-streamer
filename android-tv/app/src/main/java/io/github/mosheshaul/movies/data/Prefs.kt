package io.github.mosheshaul.movies.data

import android.content.Context
import android.content.SharedPreferences
import org.json.JSONArray
import org.json.JSONObject

/** Small on-device settings: server address, subtitle language, resume points. */
class Prefs(context: Context) {
    private val sp: SharedPreferences =
        context.applicationContext.getSharedPreferences("movies", Context.MODE_PRIVATE)

    var serverUrl: String?
        get() = sp.getString("server_url", null)
        set(v) = sp.edit().putString("server_url", v).apply()

    var serverName: String?
        get() = sp.getString("server_name", null)
        set(v) = sp.edit().putString("server_name", v).apply()

    /** ISO 639-1 code like "he" or "en"; "off" means no subtitles; null means not chosen yet. */
    var subtitleLanguage: String?
        get() = sp.getString("subtitle_language", null)
        set(v) = sp.edit().putString("subtitle_language", v).apply()

    /** Subtitle text size multiplier. */
    var subtitleScale: Float
        get() = sp.getFloat("subtitle_scale", 1.0f)
        set(v) = sp.edit().putFloat("subtitle_scale", v).apply()

    fun resumePosition(videoId: String): Long = sp.getLong("pos_$videoId", 0L)

    fun saveResume(videoId: String, positionMs: Long, durationMs: Long, item: Item) {
        val nearEnd = durationMs > 0 && positionMs > durationMs - 60_000 && positionMs > durationMs * 0.93
        if (nearEnd || positionMs < 15_000) {
            sp.edit().remove("pos_$videoId").apply()
            removeRecent(videoId)
        } else {
            sp.edit().putLong("pos_$videoId", positionMs).apply()
            addRecent(item, positionMs, durationMs)
        }
    }

    /** "Continue watching" list, most recent first. */
    fun recent(): List<Pair<Item, Float>> {
        val arr = runCatching { JSONArray(sp.getString("recent", "[]")) }.getOrDefault(JSONArray())
        return (0 until arr.length()).mapNotNull { i ->
            runCatching {
                val o = arr.getJSONObject(i)
                if (o.optString("server") != serverUrl) return@runCatching null
                Item.fromJson(o.getJSONObject("item")) to o.optDouble("progress", 0.0).toFloat()
            }.getOrNull()
        }
    }

    private fun addRecent(item: Item, pos: Long, dur: Long) {
        val arr = runCatching { JSONArray(sp.getString("recent", "[]")) }.getOrDefault(JSONArray())
        val out = JSONArray()
        out.put(
            JSONObject().put("server", serverUrl).put("item", item.toJson())
                .put("progress", if (dur > 0) pos.toDouble() / dur else 0.0)
        )
        for (i in 0 until arr.length()) {
            val o = arr.getJSONObject(i)
            if (o.getJSONObject("item").getString("id") != item.id && out.length() < 20) out.put(o)
        }
        sp.edit().putString("recent", out.toString()).apply()
    }

    private fun removeRecent(videoId: String) {
        val arr = runCatching { JSONArray(sp.getString("recent", "[]")) }.getOrDefault(JSONArray())
        val out = JSONArray()
        for (i in 0 until arr.length()) {
            val o = arr.getJSONObject(i)
            if (o.getJSONObject("item").getString("id") != videoId) out.put(o)
        }
        sp.edit().putString("recent", out.toString()).apply()
    }
}
