package io.github.mosheshaul.homestreamer.ui

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.runtime.DisposableEffect
import androidx.tv.material3.Button
import androidx.tv.material3.ListItem
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.OutlinedButton
import androidx.tv.material3.Surface
import androidx.tv.material3.SurfaceDefaults
import androidx.tv.material3.Text
import androidx.tv.material3.darkColorScheme
import io.github.mosheshaul.homestreamer.data.Api
import io.github.mosheshaul.homestreamer.data.Discovery
import io.github.mosheshaul.homestreamer.data.FoundServer
import io.github.mosheshaul.homestreamer.data.Item
import io.github.mosheshaul.homestreamer.data.Listing
import io.github.mosheshaul.homestreamer.data.Prefs
import io.github.mosheshaul.homestreamer.player.PlayerActivity
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val prefs = Prefs(this)
        setContent {
            MaterialTheme(colorScheme = darkColorScheme()) {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    colors = SurfaceDefaults.colors(containerColor = Color(0xFF0F1318), contentColor = Color(0xFFE8EAED)),
                ) {
                    App(prefs)
                }
            }
        }
    }
}

/** Screen-edge padding: roomy on TVs, tighter on phones. */
@Composable
private fun edge(tv: Dp): Dp = if (LocalConfiguration.current.screenWidthDp < 600) 16.dp else tv

private enum class Screen { Setup, Browse, Settings }

@Composable
private fun App(prefs: Prefs) {
    var screen by remember { mutableStateOf(if (prefs.serverUrl == null) Screen.Setup else Screen.Browse) }
    when (screen) {
        Screen.Setup -> SetupScreen(prefs, onConnected = { screen = Screen.Browse })
        Screen.Browse -> BrowseScreen(prefs, onSettings = { screen = Screen.Settings })
        Screen.Settings -> SettingsScreen(
            prefs,
            onBack = { screen = Screen.Browse },
            onChangeServer = { screen = Screen.Setup },
        )
    }
}

// ---------- Setup: find the PC ----------

@Composable
private fun SetupScreen(prefs: Prefs, onConnected: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var searching by remember { mutableStateOf(true) }
    var found by remember { mutableStateOf<List<FoundServer>>(emptyList()) }
    var manual by remember { mutableStateOf(prefs.serverUrl?.removePrefix("http://") ?: "") }
    var error by remember { mutableStateOf<String?>(null) }
    var searchRound by remember { mutableIntStateOf(0) }
    val firstFocus = remember { FocusRequester() }

    fun connect(url: String, name: String?) {
        scope.launch {
            error = null
            try {
                val info = Api(url).info()
                prefs.serverUrl = url
                prefs.serverName = name ?: info.name
                onConnected()
            } catch (e: Exception) {
                error = "Couldn't reach $url. Check that the server is running on the PC and that both are on the same network."
            }
        }
    }

    LaunchedEffect(searchRound) {
        searching = true
        found = Discovery.find(context)
        searching = false
        runCatching { firstFocus.requestFocus() }
    }

    Column(Modifier.fillMaxSize().padding(horizontal = edge(64.dp), vertical = edge(40.dp))) {
        Text("Connect to your PC", fontSize = 32.sp)
        Spacer(Modifier.height(8.dp))
        Text(
            "Start the Home Streamer server on the PC, then pick it below.",
            color = Color(0xFF9AA0A6), fontSize = 16.sp,
        )
        Spacer(Modifier.height(24.dp))
        when {
            searching -> Text("Searching the network…", fontSize = 18.sp)
            found.isEmpty() -> Text("No PC found automatically. Type its address below.", fontSize = 18.sp)
        }
        LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.weight(1f, fill = false)) {
            items(found, key = { it.url }) { s ->
                val idx = found.indexOf(s)
                ListItem(
                    selected = false,
                    onClick = { connect(s.url, s.name) },
                    headlineContent = { Text(s.name) },
                    supportingContent = { Text(s.url.removePrefix("http://")) },
                    leadingContent = { Text("🖥", fontSize = 22.sp) },
                    modifier = if (idx == 0) Modifier.focusRequester(firstFocus) else Modifier,
                )
            }
        }
        Spacer(Modifier.height(24.dp))
        Text("PC address (for example 192.168.1.20)", fontSize = 16.sp, color = Color(0xFF9AA0A6))
        Spacer(Modifier.height(8.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            BasicTextField(
                value = manual,
                onValueChange = { manual = it },
                singleLine = true,
                textStyle = TextStyle(color = Color.White, fontSize = 20.sp),
                cursorBrush = SolidColor(Color.White),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go),
                keyboardActions = KeyboardActions(onGo = { if (manual.isNotBlank()) connect(Api.normalize(manual), null) }),
                modifier = Modifier
                    .width(if (LocalConfiguration.current.screenWidthDp < 600) 180.dp else 360.dp)
                    .border(BorderStroke(2.dp, Color(0xFF5F6368)), RoundedCornerShape(8.dp))
                    .padding(horizontal = 14.dp, vertical = 10.dp)
                    .then(if (found.isEmpty()) Modifier.focusRequester(firstFocus) else Modifier),
            )
            Spacer(Modifier.width(16.dp))
            Button(onClick = { if (manual.isNotBlank()) connect(Api.normalize(manual), null) }) { Text("Connect") }
            Spacer(Modifier.width(12.dp))
            OutlinedButton(onClick = { searchRound++ }, enabled = !searching) { Text("Search again") }
        }
        error?.let {
            Spacer(Modifier.height(16.dp))
            Text(it, color = Color(0xFFF28B82), fontSize = 16.sp)
        }
    }
}

// ---------- Browse folders and videos ----------

@Composable
private fun BrowseScreen(prefs: Prefs, onSettings: () -> Unit) {
    val context = LocalContext.current
    if (prefs.serverUrl == null) return
    var server by remember { mutableStateOf<String?>(null) }
    val stack = remember { mutableStateListOf("") }
    val titles = remember { mutableStateListOf(prefs.serverName ?: "Library") }
    var listing by remember { mutableStateOf<Listing?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    var recent by remember { mutableStateOf(prefs.recent()) }
    val firstFocus = remember { FocusRequester() }
    val current = stack.last()

    // Refresh "continue watching" when coming back from the player.
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    DisposableEffect(lifecycle) {
        val obs = LifecycleEventObserver { _, e -> if (e == Lifecycle.Event.ON_RESUME) recent = prefs.recent() }
        lifecycle.addObserver(obs)
        onDispose { lifecycle.removeObserver(obs) }
    }

    LaunchedEffect(current, reload) {
        listing = null
        error = null
        try {
            // Find a working address (home or away) the first time, or after an error.
            val active = server ?: Api.resolve(prefs.serverUrl, prefs.remoteUrl)
            if (active == null) {
                error = if (prefs.remoteUrl == null)
                    "Couldn't reach ${prefs.serverName ?: "the PC"}. Is the server running? To watch away from home, add the PC's Tailscale address in Settings."
                else
                    "Couldn't reach ${prefs.serverName ?: "the PC"} at home or through Tailscale. Check that the server is running and Tailscale is on."
            } else {
                server = active
                listing = Api(active).browse(current)
            }
        } catch (e: Exception) {
            server = null
            error = "Lost the connection to ${prefs.serverName ?: "the PC"}. Is the server running on the PC?"
        }
        runCatching { firstFocus.requestFocus() }
    }

    BackHandler(enabled = stack.size > 1) {
        stack.removeAt(stack.lastIndex)
        titles.removeAt(titles.lastIndex)
    }

    fun open(item: Item) {
        if (item.isFolder) {
            stack.add(item.id)
            titles.add(item.name)
        } else {
            server?.let { PlayerActivity.start(context, it, item) }
        }
    }

    Column(Modifier.fillMaxSize().padding(horizontal = edge(48.dp), vertical = edge(28.dp))) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            Text(
                titles.joinToString("  ›  "),
                fontSize = 26.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            if (server != null && server == prefs.remoteUrl && server != prefs.serverUrl) {
                Text("Away (Tailscale)", fontSize = 14.sp, color = Color(0xFF8AB4F8))
                Spacer(Modifier.width(12.dp))
            }
            OutlinedButton(onClick = onSettings) { Text("Settings") }
        }
        Spacer(Modifier.height(16.dp))

        val l = listing
        when {
            error != null -> {
                Text(error!!, color = Color(0xFFF28B82), fontSize = 18.sp)
                Spacer(Modifier.height(16.dp))
                Button(onClick = { reload++ }, modifier = Modifier.focusRequester(firstFocus)) { Text("Try again") }
            }
            l == null -> Text("Loading…", fontSize = 18.sp)
            else -> LazyColumn(
                verticalArrangement = Arrangement.spacedBy(6.dp),
                contentPadding = PaddingValues(bottom = 32.dp),
            ) {
                if (stack.size == 1 && recent.isNotEmpty()) {
                    item(key = "recent") {
                        Column {
                            Text("Continue watching", fontSize = 18.sp, color = Color(0xFF9AA0A6))
                            Spacer(Modifier.height(8.dp))
                            LazyRow(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                items(recent, key = { "r" + it.first.id }) { (item, progress) ->
                                    RecentCard(item, progress) { open(item) }
                                }
                            }
                            Spacer(Modifier.height(16.dp))
                        }
                    }
                }
                if (l.items.isEmpty()) {
                    item { Text("No videos here.", fontSize = 18.sp) }
                }
                items(l.items, key = { it.id }) { item ->
                    val first = item == l.items.first()
                    ListItem(
                        selected = false,
                        onClick = { open(item) },
                        headlineContent = { Text(item.name, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        supportingContent = if (item.isFolder) null else ({ Text(describe(item, prefs)) }),
                        leadingContent = { Text(if (item.isFolder) "📁" else "🎬", fontSize = 22.sp) },
                        modifier = if (first) Modifier.focusRequester(firstFocus) else Modifier,
                    )
                }
            }
        }
    }
}

private fun describe(item: Item, prefs: Prefs): String {
    val parts = mutableListOf<String>()
    if (item.size > 0) parts += "%.1f GB".format(item.size / 1_073_741_824.0)
    if (item.subtitles.isNotEmpty()) parts += "Subtitle files: " + item.subtitles.joinToString(", ") { it.label.substringBefore(" ·") }
    val pos = prefs.resumePosition(item.id)
    if (pos > 0) parts += "Watched to " + PlayerActivity.formatTime(pos)
    return parts.joinToString("  ·  ")
}

@Composable
private fun RecentCard(item: Item, progress: Float, onClick: () -> Unit) {
    Surface(
        onClick = onClick,
        modifier = Modifier.width(260.dp).height(96.dp),
    ) {
        Column(Modifier.fillMaxSize().padding(12.dp)) {
            Text(item.name, maxLines = 2, overflow = TextOverflow.Ellipsis, fontSize = 16.sp, modifier = Modifier.weight(1f))
            Box(Modifier.fillMaxWidth().height(4.dp).background(Color(0x33FFFFFF), RoundedCornerShape(2.dp))) {
                Box(
                    Modifier.fillMaxWidth(progress.coerceIn(0.02f, 1f)).height(4.dp)
                        .background(Color(0xFF8AB4F8), RoundedCornerShape(2.dp))
                )
            }
        }
    }
}

// ---------- Settings ----------

private val subtitleChoices = listOf(
    null to "Use the video's default",
    "off" to "Off",
    "he" to "Hebrew",
    "en" to "English",
    "ar" to "Arabic",
    "ru" to "Russian",
    "fr" to "French",
    "es" to "Spanish",
)

private val sizeChoices = listOf(0.8f to "Small", 1.0f to "Medium", 1.25f to "Large", 1.5f to "Extra large")

@Composable
private fun SettingsScreen(prefs: Prefs, onBack: () -> Unit, onChangeServer: () -> Unit) {
    var lang by remember { mutableStateOf(prefs.subtitleLanguage) }
    var scale by remember { mutableStateOf(prefs.subtitleScale) }
    val firstFocus = remember { FocusRequester() }
    BackHandler(onBack = onBack)
    LaunchedEffect(Unit) { runCatching { firstFocus.requestFocus() } }

    LazyColumn(
        Modifier.fillMaxSize().padding(horizontal = edge(64.dp), vertical = edge(32.dp)),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        item { Text("Settings", fontSize = 30.sp); Spacer(Modifier.height(16.dp)) }
        item {
            Text("Subtitle language", fontSize = 18.sp, color = Color(0xFF9AA0A6))
            Text(
                "Picking subtitles in the player also updates this.",
                fontSize = 14.sp, color = Color(0xFF9AA0A6),
            )
        }
        items(subtitleChoices) { (code, label) ->
            ListItem(
                selected = lang == code,
                onClick = { lang = code; prefs.subtitleLanguage = code },
                headlineContent = { Text(label) },
                trailingContent = if (lang == code) ({ Text("✓") }) else null,
                modifier = if (code == null) Modifier.focusRequester(firstFocus) else Modifier,
            )
        }
        item { Spacer(Modifier.height(16.dp)); Text("Subtitle size", fontSize = 18.sp, color = Color(0xFF9AA0A6)) }
        items(sizeChoices) { (value, label) ->
            ListItem(
                selected = scale == value,
                onClick = { scale = value; prefs.subtitleScale = value },
                headlineContent = { Text(label) },
                trailingContent = if (scale == value) ({ Text("✓") }) else null,
            )
        }
        item {
            Spacer(Modifier.height(16.dp))
            Text("Server", fontSize = 18.sp, color = Color(0xFF9AA0A6))
        }
        item {
            AwayAddressRow(prefs)
        }
        item {
            ListItem(
                selected = false,
                onClick = onChangeServer,
                headlineContent = { Text("Change PC") },
                supportingContent = { Text("Connected to ${prefs.serverName ?: ""} (${prefs.serverUrl?.removePrefix("http://") ?: ""})") },
            )
        }
    }
}

@Composable
private fun AwayAddressRow(prefs: Prefs) {
    val scope = rememberCoroutineScope()
    var text by remember { mutableStateOf(prefs.remoteUrl?.removePrefix("http://") ?: "") }
    var status by remember { mutableStateOf<String?>(null) }
    fun save() {
        if (text.isBlank()) {
            prefs.remoteUrl = null
            status = "Away address removed."
            return
        }
        val url = Api.normalize(text)
        status = "Checking…"
        scope.launch {
            val ok = runCatching { Api(url).info(timeoutMs = 6000) }.isSuccess
            prefs.remoteUrl = url
            status = if (ok) "Saved. The PC answered at this address."
            else "Saved, but the PC didn't answer right now. Check that Tailscale is on, on both devices."
        }
    }
    Column(Modifier.padding(vertical = 8.dp)) {
        Text("Away from home (Tailscale address of the PC, like 100.101.102.103)", fontSize = 16.sp)
        Spacer(Modifier.height(8.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            BasicTextField(
                value = text,
                onValueChange = { text = it },
                singleLine = true,
                textStyle = TextStyle(color = Color.White, fontSize = 20.sp),
                cursorBrush = SolidColor(Color.White),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = { save() }),
                modifier = Modifier
                    .width(if (LocalConfiguration.current.screenWidthDp < 600) 180.dp else 360.dp)
                    .border(BorderStroke(2.dp, Color(0xFF5F6368)), RoundedCornerShape(8.dp))
                    .padding(horizontal = 14.dp, vertical = 10.dp),
            )
            Spacer(Modifier.width(16.dp))
            Button(onClick = { save() }) { Text("Save") }
        }
        status?.let { Spacer(Modifier.height(6.dp)); Text(it, fontSize = 14.sp, color = Color(0xFF9AA0A6)) }
    }
}
