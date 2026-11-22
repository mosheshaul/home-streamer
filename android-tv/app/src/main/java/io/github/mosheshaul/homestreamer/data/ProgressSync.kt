package io.github.mosheshaul.homestreamer.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

/**
 * Keeps "Continue watching" on the PC, so the phone, TV and laptop all share it.
 * Changes are sent in order in the background; [pull] waits for them first, so
 * coming back from the player never shows an older position.
 */
object ProgressSync {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var last: Job? = null

    private fun enqueue(block: suspend () -> Unit) = synchronized(this) {
        val prev = last
        last = scope.launch {
            prev?.join()
            runCatching { block() }
        }
    }

    fun push(server: String, item: Item, positionMs: Long, durationMs: Long) =
        enqueue { Api(server).putProgress(item, positionMs, durationMs) }

    fun forget(server: String, videoId: String) = enqueue { Api(server).deleteProgress(videoId) }

    /** Replaces the device's list with the PC's. Keeps the device's list if the PC can't be reached. */
    suspend fun pull(server: String, prefs: Prefs) {
        synchronized(this) { last }?.join()
        val list = runCatching { Api(server).progress() }.getOrNull() ?: return
        prefs.replaceRecent(list)
    }
}
