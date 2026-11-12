package io.aiptimize.companion.domain

import io.aiptimize.companion.data.ApiClient
import io.aiptimize.companion.data.ProbeResult
import io.aiptimize.companion.data.ResolvedEndpoint
import io.aiptimize.companion.data.ServerProfile
import io.aiptimize.companion.data.Transport

/**
 * Picks the endpoint for a profile: LAN first, then WAN, unless the user pinned
 * one.
 *
 * A LAN candidate must answer `/api/v1/instance` with the profile's instance id.
 * A WAN candidate may instead be gated by Cloudflare Access: that proves the
 * hostname is alive, and the WebView performs the sign-in afterwards, so the id
 * cannot be checked up front.
 */
class EndpointResolver(private val api: ApiClient) {

    suspend fun resolve(profile: ServerProfile): ResolvedEndpoint? {
        for ((url, transport) in candidates(profile)) {
            when (val result = api.probe(url, remote = transport == Transport.WAN)) {
                is ProbeResult.Verified -> {
                    if (result.info.id == profile.instanceId) {
                        return ResolvedEndpoint(url, transport)
                    }
                }
                is ProbeResult.Gated -> {
                    if (transport == Transport.WAN) {
                        return ResolvedEndpoint(url, transport)
                    }
                }
                ProbeResult.Unreachable -> Unit
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
