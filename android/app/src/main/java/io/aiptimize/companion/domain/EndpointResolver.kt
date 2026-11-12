package io.aiptimize.companion.domain

import io.aiptimize.companion.data.ApiClient
import io.aiptimize.companion.data.ResolvedEndpoint
import io.aiptimize.companion.data.ServerProfile
import io.aiptimize.companion.data.Transport

/**
 * Picks the endpoint for a profile: LAN first, then WAN, unless the user pinned
 * one. Every candidate is validated against the profile's instance id, so a
 * stale IP or a different server behind the same URL is rejected.
 */
class EndpointResolver(private val api: ApiClient) {

    suspend fun resolve(profile: ServerProfile): ResolvedEndpoint? {
        for ((url, transport) in candidates(profile)) {
            val info = api.instance(url, remote = transport == Transport.WAN) ?: continue
            if (info.id == profile.instanceId) {
                return ResolvedEndpoint(url, transport)
            }
        }
        return null
    }

    fun candidates(profile: ServerProfile): List<Pair<String, Transport>> = buildList {
        if (profile.preferred != Transport.WAN) {
            profile.lanUrl?.let { add(it to Transport.LAN) }
        }
        if (profile.preferred != Transport.LAN) {
            profile.wanUrl?.let { add(it to Transport.WAN) }
        }
    }
}
