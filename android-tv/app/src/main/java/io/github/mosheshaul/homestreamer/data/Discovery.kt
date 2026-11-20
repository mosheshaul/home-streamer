package io.github.mosheshaul.homestreamer.data

import android.content.Context
import android.net.wifi.WifiManager
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.SocketTimeoutException

data class FoundServer(val name: String, val url: String)

/** Finds Home Streamer servers on the local network with a UDP broadcast. */
object Discovery {
    private const val PORT = 47820
    private const val QUERY = "HOMESTREAMER_DISCOVER"

    suspend fun find(context: Context, timeoutMs: Int = 2500): List<FoundServer> = withContext(Dispatchers.IO) {
        val wifi = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
        val lock = try {
            wifi?.createMulticastLock("home-streamer-discovery")?.apply { setReferenceCounted(false); acquire() }
        } catch (e: SecurityException) {
            null
        }
        val found = linkedMapOf<String, FoundServer>()
        try {
            DatagramSocket().use { socket ->
                socket.broadcast = true
                socket.soTimeout = 400
                val data = QUERY.toByteArray()
                val targets = buildList {
                    add(InetAddress.getByName("255.255.255.255"))
                    subnetBroadcast(wifi)?.let { add(it) }
                }
                val deadline = System.currentTimeMillis() + timeoutMs
                var nextSend = 0L
                val buf = ByteArray(1024)
                while (System.currentTimeMillis() < deadline) {
                    if (System.currentTimeMillis() >= nextSend) {
                        for (t in targets) {
                            runCatching { socket.send(DatagramPacket(data, data.size, t, PORT)) }
                        }
                        nextSend = System.currentTimeMillis() + 800
                    }
                    val packet = DatagramPacket(buf, buf.size)
                    try {
                        socket.receive(packet)
                        val o = JSONObject(String(packet.data, 0, packet.length))
                        if (o.optString("app") == "home-streamer") {
                            val host = packet.address.hostAddress ?: continue
                            val url = "http://$host:${o.optInt("port", 8095)}"
                            found[url] = FoundServer(o.optString("name", host), url)
                        }
                    } catch (_: SocketTimeoutException) {
                    } catch (_: org.json.JSONException) {
                    }
                }
            }
        } catch (_: Exception) {
        } finally {
            runCatching { lock?.release() }
        }
        found.values.toList()
    }

    @Suppress("DEPRECATION")
    private fun subnetBroadcast(wifi: WifiManager?): InetAddress? {
        val dhcp = wifi?.dhcpInfo ?: return null
        if (dhcp.ipAddress == 0) return null
        val bc = (dhcp.ipAddress and dhcp.netmask) or dhcp.netmask.inv()
        val bytes = ByteArray(4) { i -> (bc shr (i * 8) and 0xFF).toByte() }
        return runCatching { InetAddress.getByAddress(bytes) }.getOrNull()
    }
}
