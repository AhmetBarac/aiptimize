package io.aiptimize.companion.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** One Aiptimize installation, identified by the server's instance id. */
@Serializable
data class ServerProfile(
    val instanceId: String,
    val label: String,
    val lanUrl: String? = null,
    val wanUrl: String? = null,
    val preferred: Transport = Transport.AUTO,
    val version: String? = null,
    val lastSeenEpochMs: Long = 0L,
)

@Serializable
enum class Transport { AUTO, LAN, WAN }

/** `GET /api/v1/instance` — the identity every URL is validated against. */
@Serializable
data class InstanceInfo(
    val id: String,
    val name: String,
    val version: String,
    @SerialName("api_version") val apiVersion: Int = 1,
    @SerialName("setup_required") val setupRequired: Boolean = false,
)

/** One UDP discovery reply. */
@Serializable
data class DiscoveryReply(
    val aiptimize: String,
    val v: Int,
    @SerialName("instance_id") val instanceId: String,
    val name: String,
    val version: String,
    val api: String,
    @SerialName("setup_required") val setupRequired: Boolean = false,
)

data class DiscoveredServer(
    val reply: DiscoveryReply,
    val sourceHost: String,
)

data class ResolvedEndpoint(
    val baseUrl: String,
    val transport: Transport,
)
