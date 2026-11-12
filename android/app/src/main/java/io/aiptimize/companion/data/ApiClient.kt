package io.aiptimize.companion.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import okhttp3.OkHttpClient
import okhttp3.Request
import java.net.URI
import java.util.concurrent.TimeUnit

/** Small read-only client: it only ever calls `/api/v1/instance`. */
class ApiClient {

    private val json = Json { ignoreUnknownKeys = true }
    private val fast = OkHttpClient.Builder()
        .callTimeout(3, TimeUnit.SECONDS)
        .retryOnConnectionFailure(false)
        .build()
    private val slow = OkHttpClient.Builder()
        .callTimeout(10, TimeUnit.SECONDS)
        .build()

    suspend fun instance(baseUrl: String, remote: Boolean = false): InstanceInfo? =
        withContext(Dispatchers.IO) {
            val request = runCatching {
                Request.Builder()
                    .url(baseUrl.trimEnd('/') + "/api/v1/instance")
                    .header("Accept", "application/json")
                    .build()
            }.getOrNull() ?: return@withContext null

            runCatching {
                (if (remote) slow else fast).newCall(request).execute().use { response ->
                    if (!response.isSuccessful) return@use null
                    val body = response.body?.string() ?: return@use null
                    json.decodeFromString<InstanceInfo>(body)
                }
            }.getOrNull()
        }

    companion object {
        /** Accepts "192.168.0.154:8080", "host", "http://…" and "https://…". */
        fun normalize(raw: String): String? {
            val trimmed = raw.trim().trimEnd('/')
            if (trimmed.isEmpty()) return null
            val withScheme = if (trimmed.startsWith("http://") || trimmed.startsWith("https://")) {
                trimmed
            } else {
                "http://$trimmed"
            }
            val uri = runCatching { URI(withScheme) }.getOrNull() ?: return null
            if (uri.host.isNullOrBlank()) return null
            return withScheme
        }

        fun isRemote(url: String): Boolean = url.startsWith("https://")
    }
}
