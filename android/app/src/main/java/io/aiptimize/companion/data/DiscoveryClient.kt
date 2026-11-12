package io.aiptimize.companion.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.NetworkInterface
import java.net.SocketTimeoutException

/**
 * Sends the LAN discovery probe and collects unicast replies.
 *
 * The server answers on UDP 8099 with its instance id and the API URL for the
 * interface the probe arrived on, so no multicast lock is required.
 */
class DiscoveryClient(private val port: Int = 8099) {

    private val json = Json { ignoreUnknownKeys = true }

    suspend fun discover(timeoutMs: Long = 2500): List<DiscoveredServer> =
        withContext(Dispatchers.IO) {
            val found = linkedMapOf<String, DiscoveredServer>()
            DatagramSocket().use { socket ->
                socket.broadcast = true
                socket.soTimeout = 300
                val probe = """{"aiptimize":"discover","v":1}""".toByteArray()
                broadcastAddresses().forEach { address ->
                    runCatching {
                        socket.send(DatagramPacket(probe, probe.size, address, port))
                    }
                }
                val deadline = System.currentTimeMillis() + timeoutMs
                val buffer = ByteArray(2048)
                while (System.currentTimeMillis() < deadline) {
                    val packet = DatagramPacket(buffer, buffer.size)
                    try {
                        socket.receive(packet)
                    } catch (_: SocketTimeoutException) {
                        continue
                    }
                    val body = String(packet.data, 0, packet.length)
                    val reply = runCatching { json.decodeFromString<DiscoveryReply>(body) }
                        .getOrNull() ?: continue
                    if (reply.aiptimize != "server") continue
                    found[reply.instanceId] =
                        DiscoveredServer(reply, packet.address?.hostAddress.orEmpty())
                }
            }
            found.values.toList()
        }

    private fun broadcastAddresses(): List<InetAddress> {
        val addresses = mutableListOf<InetAddress>()
        runCatching { addresses += InetAddress.getByName("255.255.255.255") }
        runCatching {
            NetworkInterface.getNetworkInterfaces().toList().forEach { nif ->
                nif.interfaceAddresses.forEach { entry ->
                    entry.broadcast?.let { addresses += it }
                }
            }
        }
        return addresses.distinct()
    }
}
