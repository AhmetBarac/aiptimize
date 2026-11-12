package io.aiptimize.companion.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.Json
import okhttp3.OkHttpClient
import okhttp3.Request
import java.net.URI
import java.util.concurrent.TimeUnit

/** Outcome of validating one candidate URL. */
sealed interface ProbeResult {
    /** Reachable and identified: `/api/v1/instance` answered. */
    data class Verified(val info: InstanceInfo) : ProbeResult

    /**
     * Reachable but intercepted by Cloudflare Access: the probe was answered
     * with a redirect to the Access login. The WebView performs that sign-in.
     */
    data class Gated(val loginUrl: String) : ProbeResult

    /** Nothing usable answered. */
    data object Unreachable : ProbeResult
}

/** Small read-only client: it only ever calls `/api/v1/instance`. */
class ApiClient {

    private val json = Json { ignoreUnknownKeys = true }
    private val fast = OkHttpClient.Builder()
        .callTimeout(3, TimeUnit.SECONDS)
        .retryOnConnectionFailure(false)
        .followRedirects(false)
        .build()
    private val slow = OkHttpClient.Builder()
        .callTimeout(10, TimeUnit.SECONDS)
        .followRedirects(false)
        .build()

    suspend fun probe(baseUrl: String, remote: Boolean = false): ProbeResult =
        withContext(Dispatchers.IO) {
            val request = runCatching {
                Request.Builder()
                    .url(baseUrl.trimEnd('/') + "/api/v1/instance")
                    .header("Accept", "application/json")
                    .build()
            }.getOrNull() ?: return@withContext ProbeResult.Unreachable

            runCatching {
                (if (remote) slow else fast).newCall(request).execute().use { response ->
                    when {
                        response.isSuccessful -> {
                            val body = response.body?.string().orEmpty()
                            val info = runCatching { json.decodeFromString<InstanceInfo>(body) }.getOrNull()
                            if (info == null) ProbeResult.Unreachable else ProbeResult.Verified(info)
                        }
                        response.code in 300..399 -> {
                            val location = response.header("Location").orEmpty()
                            if (location.contains("cloudflareaccess.com")) {
                                ProbeResult.Gated(location)
                            } else {
                                ProbeResult.Unreachable
                            }
                        }
                        else -> ProbeResult.Unreachable
                    }
                }
            }.getOrDefault(ProbeResult.Unreachable)
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
