package io.aiptimize.companion.data

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.Json

private val Context.dataStore by preferencesDataStore(name = "aiptimize")

/** Persists the server list as JSON in DataStore. No secrets are kept here. */
class ServerStore(private val context: Context) {

    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
    }
    private val key = stringPreferencesKey("servers")

    val servers: Flow<List<ServerProfile>> = context.dataStore.data.map { prefs ->
        decode(prefs[key])
    }

    suspend fun upsert(profile: ServerProfile) {
        context.dataStore.edit { prefs ->
            val current = decode(prefs[key])
            val next = current.filterNot { it.instanceId == profile.instanceId } +
                profile.copy(lastSeenEpochMs = System.currentTimeMillis())
            prefs[key] = json.encodeToString(next)
        }
    }

    suspend fun remove(instanceId: String) {
        context.dataStore.edit { prefs ->
            val next = decode(prefs[key]).filterNot { it.instanceId == instanceId }
            prefs[key] = json.encodeToString(next)
        }
    }

    private fun decode(raw: String?): List<ServerProfile> {
        if (raw.isNullOrBlank()) return emptyList()
        return runCatching { json.decodeFromString<List<ServerProfile>>(raw) }.getOrDefault(emptyList())
    }
}
