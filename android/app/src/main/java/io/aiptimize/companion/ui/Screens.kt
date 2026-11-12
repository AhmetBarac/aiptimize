package io.aiptimize.companion.ui

import android.annotation.SuppressLint
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import io.aiptimize.companion.BuildConfig
import io.aiptimize.companion.data.DiscoveredServer
import io.aiptimize.companion.data.InstanceInfo
import io.aiptimize.companion.data.ProbeResult
import io.aiptimize.companion.data.ResolvedEndpoint
import io.aiptimize.companion.data.ServerProfile
import io.aiptimize.companion.data.Transport
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServersScreen(
    servers: List<ServerProfile>,
    onAdd: () -> Unit,
    onOpen: (ServerProfile) -> Unit,
    onSettings: (ServerProfile) -> Unit,
) {
    Scaffold(
        topBar = { TopAppBar(title = { Text("Aiptimize") }) },
        floatingActionButton = {
            FloatingActionButton(onClick = onAdd) {
                Text("+", style = MaterialTheme.typography.headlineMedium)
            }
        },
    ) { padding ->
        if (servers.isEmpty()) {
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(padding)
                    .padding(32.dp),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text("No servers yet", style = MaterialTheme.typography.titleMedium)
                Spacer(Modifier.height(8.dp))
                Text(
                    "Tap + to discover a server on this network or enter its address.",
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            return@Scaffold
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            items(servers, key = { it.instanceId }) { profile ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(16.dp)) {
                        Text(profile.label, style = MaterialTheme.typography.titleMedium)
                        Text(
                            buildString {
                                append(profile.version ?: "unknown version")
                                profile.lanUrl?.let { append("\nLAN  ").append(it) }
                                profile.wanUrl?.let { append("\nWAN  ").append(it) }
                            },
                            style = MaterialTheme.typography.bodySmall,
                            fontFamily = FontFamily.Monospace,
                        )
                        Spacer(Modifier.height(8.dp))
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Button(onClick = { onOpen(profile) }) { Text("Open") }
                            OutlinedButton(onClick = { onSettings(profile) }) { Text("Settings") }
                        }
                    }
                }
            }
            item {
                Text(
                    "Companion ${BuildConfig.VERSION_NAME} (${BuildConfig.VERSION_CODE})",
                    style = MaterialTheme.typography.labelSmall,
                    fontFamily = FontFamily.Monospace,
                    modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                )
            }
        }
    }
}

private sealed interface TestState {
    data object Idle : TestState
    data object Running : TestState
    data class Ok(val info: InstanceInfo, val url: String) : TestState
    data class Gated(val url: String) : TestState
    data class Failed(val message: String) : TestState
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddServerScreen(
    vm: MainViewModel,
    scanning: Boolean,
    discovered: List<DiscoveredServer>,
    onScan: () -> Unit,
    onSave: (ServerProfile) -> Unit,
    onBack: () -> Unit,
) {
    var tab by remember { mutableIntStateOf(0) }
    var url by remember { mutableStateOf("") }
    var state by remember { mutableStateOf<TestState>(TestState.Idle) }
    val scope = rememberCoroutineScope()

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Add server") },
                navigationIcon = { TextButton(onClick = onBack) { Text("Back") } },
            )
        },
    ) { padding ->
        Column(modifier = Modifier.fillMaxSize().padding(padding).padding(16.dp)) {
            TabRow(selectedTabIndex = tab) {
                Tab(selected = tab == 0, onClick = { tab = 0 }, text = { Text("Discovered") })
                Tab(selected = tab == 1, onClick = { tab = 1 }, text = { Text("Address") })
            }
            Spacer(Modifier.height(16.dp))

            if (tab == 0) {
                Button(onClick = onScan, enabled = !scanning) {
                    Text(if (scanning) "Scanning…" else "Scan network")
                }
                Spacer(Modifier.height(12.dp))
                if (scanning) {
                    CircularProgressIndicator()
                    return@Column
                }
                if (discovered.isEmpty()) {
                    Text(
                        "Nothing found yet. Scanning needs the phone on the same Wi-Fi as the server; " +
                            "guest networks often block it — use the Address tab instead.",
                        style = MaterialTheme.typography.bodySmall,
                    )
                    return@Column
                }
                LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    items(discovered, key = { it.reply.instanceId }) { server ->
                        Card(modifier = Modifier.fillMaxWidth()) {
                            Column(Modifier.padding(16.dp)) {
                                Text(server.reply.name, style = MaterialTheme.typography.titleMedium)
                                Text(
                                    "${server.reply.version}  ·  ${server.reply.api}",
                                    style = MaterialTheme.typography.bodySmall,
                                    fontFamily = FontFamily.Monospace,
                                )
                                Spacer(Modifier.height(8.dp))
                                Button(onClick = {
                                    onSave(
                                        ServerProfile(
                                            instanceId = server.reply.instanceId,
                                            label = server.reply.name,
                                            lanUrl = server.reply.api,
                                            version = server.reply.version,
                                        ),
                                    )
                                }) { Text("Save") }
                            }
                        }
                    }
                }
            } else {
                OutlinedTextField(
                    value = url,
                    onValueChange = {
                        url = it
                        state = TestState.Idle
                    },
                    label = { Text("192.168.0.154:8080 or https://home.aiptimize.io") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(12.dp))
                Button(
                    onClick = {
                        state = TestState.Running
                        scope.launch {
                            val result = vm.test(url)
                            val probe = result?.first
                            state = when {
                                result == null -> TestState.Failed("That address is not valid.")
                                probe is ProbeResult.Verified -> TestState.Ok(probe.info, result.second)
                                probe is ProbeResult.Gated -> TestState.Gated(result.second)
                                else -> TestState.Failed("No Aiptimize server answered at that address.")
                            }
                        }
                    },
                    enabled = url.isNotBlank() && state !is TestState.Running,
                ) { Text(if (state is TestState.Running) "Testing…" else "Test") }

                Spacer(Modifier.height(12.dp))
                when (val current = state) {
                    is TestState.Ok -> {
                        Text(
                            "${current.info.name} · ${current.info.version}",
                            style = MaterialTheme.typography.titleMedium,
                        )
                        Spacer(Modifier.height(8.dp))
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Button(onClick = {
                                onSave(
                                    ServerProfile(
                                        instanceId = current.info.id,
                                        label = current.info.name,
                                        lanUrl = current.url,
                                        version = current.info.version,
                                    ),
                                )
                            }) { Text("Save as LAN") }
                            OutlinedButton(
                                onClick = {
                                    onSave(
                                        ServerProfile(
                                            instanceId = current.info.id,
                                            label = current.info.name,
                                            wanUrl = current.url,
                                            version = current.info.version,
                                        ),
                                    )
                                },
                                enabled = current.url.startsWith("https://"),
                            ) { Text("Save as WAN") }
                        }
                        if (!current.url.startsWith("https://")) {
                            Text(
                                "A remote address must use https.",
                                style = MaterialTheme.typography.bodySmall,
                            )
                        }
                    }
                    is TestState.Gated -> {
                        Text(
                            "Reachable, protected by Cloudflare Access.",
                            style = MaterialTheme.typography.titleMedium,
                        )
                        Text(
                            "Sign in with the one-time PIN when the page opens.",
                            style = MaterialTheme.typography.bodySmall,
                        )
                        Spacer(Modifier.height(8.dp))
                        OutlinedButton(
                            onClick = {
                                val host = runCatching { java.net.URI(current.url).host }
                                    .getOrNull().orEmpty().ifBlank { "Remote server" }
                                onSave(
                                    ServerProfile(
                                        // No instance id is visible through Access; the
                                        // hostname keeps the profile stable.
                                        instanceId = "wan:$host",
                                        label = host,
                                        wanUrl = current.url,
                                    ),
                                )
                            },
                            enabled = current.url.startsWith("https://"),
                        ) { Text("Save as WAN") }
                    }
                    is TestState.Failed -> Text(current.message, color = MaterialTheme.colorScheme.error)
                    else -> Unit
                }
            }
        }
    }
}

@SuppressLint("SetJavaScriptEnabled")
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ViewerScreen(
    vm: MainViewModel,
    instanceId: String,
    onBack: () -> Unit,
) {
    val profile = remember(instanceId) { vm.profile(instanceId) }
    var endpoint by remember { mutableStateOf<ResolvedEndpoint?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var webView by remember { mutableStateOf<WebView?>(null) }
    var attempt by remember { mutableIntStateOf(0) }
    var diagnostics by remember { mutableStateOf<String?>(null) }

    val metricsScript = remember {
        """(function(){
            var out=['vh='+window.innerHeight];
            if (window.visualViewport) out.push('vv='+Math.round(window.visualViewport.height));
            var panels=document.querySelectorAll('.sheet-panel');
            out.push('panels='+panels.length);
            for (var i=0;i<panels.length;i++){
              var p=panels[i], b=p.getBoundingClientRect(), cs=getComputedStyle(p);
              out.push('p'+i+'='+Math.round(b.top)+'+'+Math.round(b.height)+'/scroll'+p.scrollHeight+'/max'+cs.maxHeight);
            }
            return out.join(' ');
        })()""".trimIndent()
    }

    fun reportMetrics(view: WebView) {
        val metrics = view.resources.displayMetrics
        view.evaluateJavascript(metricsScript) { result ->
            diagnostics = "view ${view.width}x${view.height} screen ${metrics.widthPixels}x${metrics.heightPixels} " +
                result.trim('"')
        }
    }

    // Refresh while the viewer is open so a screenshot always reflects the
    // sheet that is currently on screen.
    LaunchedEffect(webView) {
        val view = webView ?: return@LaunchedEffect
        while (true) {
            reportMetrics(view)
            delay(700)
        }
    }

    LaunchedEffect(instanceId, attempt) {
        error = null
        endpoint = null
        val current = vm.profile(instanceId)
        if (current == null) {
            error = "Server not found."
            return@LaunchedEffect
        }
        val resolved = vm.resolve(current)
        if (resolved == null) {
            error = "No endpoint answered. Check that the server is on and the address is correct."
        }
        endpoint = resolved
    }

    LaunchedEffect(endpoint, webView) {
        val target = endpoint?.baseUrl ?: return@LaunchedEffect
        val view = webView ?: return@LaunchedEffect
        if (view.url != target) view.loadUrl(target)
    }

    BackHandler {
        val view = webView
        if (view != null && view.canGoBack()) view.goBack() else onBack()
    }

    Scaffold(
        contentWindowInsets = WindowInsets.safeDrawing,
        topBar = {
            TopAppBar(
                title = { Text(profile?.label ?: "Aiptimize") },
                navigationIcon = { TextButton(onClick = onBack) { Text("Servers") } },
                actions = {
                    TextButton(onClick = { webView?.clearCache(true); webView?.reload() }) { Text("Reload") }
                    TextButton(onClick = { webView?.let { reportMetrics(it) } }) { Text("Info") }
                },
            )
        },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            endpoint?.let { active ->
                Text(
                    "${active.transport.name}  ·  ${active.baseUrl}",
                    style = MaterialTheme.typography.labelSmall,
                    fontFamily = FontFamily.Monospace,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
                )
            }
            diagnostics?.let { text ->
                Text(
                    text,
                    style = MaterialTheme.typography.labelSmall,
                    fontFamily = FontFamily.Monospace,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp, vertical = 4.dp),
                )
            }
            when {
                error != null -> Column(
                    modifier = Modifier.fillMaxWidth().weight(1f).padding(32.dp),
                    verticalArrangement = Arrangement.Center,
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Text(error.orEmpty(), style = MaterialTheme.typography.bodyLarge)
                    Spacer(Modifier.height(16.dp))
                    Button(onClick = { attempt++ }) { Text("Retry") }
                }
                endpoint == null -> Column(
                    modifier = Modifier.fillMaxWidth().weight(1f),
                    verticalArrangement = Arrangement.Center,
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) { CircularProgressIndicator() }
                else -> AndroidView(
                    // weight, not fillMaxSize: inside a Column fillMaxSize would take
                    // the whole height and push the bottom of the page off-screen.
                    modifier = Modifier.fillMaxWidth().weight(1f),
                    factory = { context ->
                        WebView(context).apply {
                            settings.javaScriptEnabled = true
                            settings.domStorageEnabled = true
                            settings.allowFileAccess = false
                            settings.allowContentAccess = false
                            settings.useWideViewPort = true
                            settings.loadWithOverviewMode = false
                            settings.builtInZoomControls = false
                            settings.displayZoomControls = false
                            settings.mixedContentMode =
                                android.webkit.WebSettings.MIXED_CONTENT_NEVER_ALLOW
                            webViewClient = object : WebViewClient() {
                                override fun onPageFinished(view: WebView, url: String?) {
                                    reportMetrics(view)
                                }

                                override fun onReceivedError(
                                    view: WebView,
                                    request: WebResourceRequest,
                                    err: WebResourceError,
                                ) {
                                    if (request.isForMainFrame) {
                                        error = "Could not load the page (${err.description})."
                                    }
                                }
                            }
                            webView = this
                        }
                    },
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServerSettingsScreen(
    profile: ServerProfile?,
    onSave: (ServerProfile) -> Unit,
    onRemove: (ServerProfile) -> Unit,
    onBack: () -> Unit,
) {
    var lan by remember(profile) { mutableStateOf(profile?.lanUrl.orEmpty()) }
    var wan by remember(profile) { mutableStateOf(profile?.wanUrl.orEmpty()) }
    var preferred by remember(profile) { mutableStateOf(profile?.preferred ?: Transport.AUTO) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(profile?.label ?: "Server") },
                navigationIcon = { TextButton(onClick = onBack) { Text("Back") } },
            )
        },
    ) { padding ->
        if (profile == null) {
            Text("Server not found.", modifier = Modifier.padding(padding).padding(16.dp))
            return@Scaffold
        }
        Column(Modifier.fillMaxSize().padding(padding).padding(16.dp)) {
            OutlinedTextField(
                value = lan,
                onValueChange = { lan = it },
                label = { Text("LAN address") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(
                value = wan,
                onValueChange = { wan = it },
                label = { Text("Remote address") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(12.dp))
            Text("Preferred network", style = MaterialTheme.typography.labelLarge)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Transport.entries.forEach { option ->
                    if (option == preferred) {
                        Button(onClick = { preferred = option }) { Text(option.name) }
                    } else {
                        OutlinedButton(onClick = { preferred = option }) { Text(option.name) }
                    }
                }
            }
            Spacer(Modifier.height(16.dp))
            Button(onClick = {
                onSave(
                    profile.copy(
                        lanUrl = lan.trim().ifBlank { null },
                        wanUrl = wan.trim().ifBlank { null },
                        preferred = preferred,
                    ),
                )
            }) { Text("Save") }
            Spacer(Modifier.height(24.dp))
            OutlinedButton(onClick = { onRemove(profile) }) { Text("Remove server") }
            Spacer(Modifier.height(8.dp))
            Text(
                "Instance ${profile.instanceId}",
                style = MaterialTheme.typography.labelSmall,
                fontFamily = FontFamily.Monospace,
            )
        }
    }
}
