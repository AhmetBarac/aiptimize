package io.aiptimize.companion.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import io.aiptimize.companion.data.ApiClient
import io.aiptimize.companion.data.DiscoveredServer
import io.aiptimize.companion.data.DiscoveryClient
import io.aiptimize.companion.data.ProbeResult
import io.aiptimize.companion.data.ResolvedEndpoint
import io.aiptimize.companion.data.ServerProfile
import io.aiptimize.companion.data.ServerStore
import io.aiptimize.companion.domain.EndpointResolver
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/** Shared state for the launcher: saved servers, discovery results, resolution. */
class MainViewModel(app: Application) : AndroidViewModel(app) {

    private val store = ServerStore(app)
    private val discovery = DiscoveryClient()
    private val api = ApiClient()
    private val resolver = EndpointResolver(api)

    val servers: StateFlow<List<ServerProfile>> = store.servers
        .stateIn(viewModelScope, SharingStarted.Eagerly, emptyList())

    private val _discovered = MutableStateFlow<List<DiscoveredServer>>(emptyList())
    val discovered: StateFlow<List<DiscoveredServer>> = _discovered.asStateFlow()

    private val _scanning = MutableStateFlow(false)
    val scanning: StateFlow<Boolean> = _scanning.asStateFlow()

    fun scan() {
        viewModelScope.launch {
            _scanning.value = true
            _discovered.value = discovery.discover()
            _scanning.value = false
        }
    }

    suspend fun test(rawUrl: String): Pair<ProbeResult, String>? {
        val url = ApiClient.normalize(rawUrl) ?: return null
        return api.probe(url, remote = ApiClient.isRemote(url)) to url
    }

    fun save(profile: ServerProfile) {
        viewModelScope.launch { store.upsert(profile) }
    }

    fun remove(instanceId: String) {
        viewModelScope.launch { store.remove(instanceId) }
    }

    suspend fun resolve(profile: ServerProfile): ResolvedEndpoint? = resolver.resolve(profile)

    fun profile(instanceId: String): ServerProfile? =
        servers.value.firstOrNull { it.instanceId == instanceId }
}
