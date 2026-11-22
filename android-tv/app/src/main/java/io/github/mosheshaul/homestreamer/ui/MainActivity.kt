package io.github.mosheshaul.homestreamer.ui

import android.app.UiModeManager
import android.content.Context
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
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
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.darkColorScheme as phoneDarkColors
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
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
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import io.github.mosheshaul.homestreamer.data.Api
import io.github.mosheshaul.homestreamer.data.Discovery
import io.github.mosheshaul.homestreamer.data.FoundServer
import io.github.mosheshaul.homestreamer.data.Item
import io.github.mosheshaul.homestreamer.data.Listing
import io.github.mosheshaul.homestreamer.data.ProgressSync
import io.github.mosheshaul.homestreamer.data.Prefs
import io.github.mosheshaul.homestreamer.player.PlayerActivity
import kotlinx.coroutines.launch
import androidx.compose.material3.MaterialTheme as PhoneTheme
import androidx.tv.material3.MaterialTheme as TvTheme
import androidx.tv.material3.darkColorScheme as tvDarkColors

private val Muted = Color(0xFF9AA0A6)
private val ErrorColor = Color(0xFFF28B82)
private val Accent = Color(0xFF8AB4F8)

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val prefs = Prefs(this)
        val isTv = isTelevision(this)
        setContent {
            CompositionLocalProvider(LocalIsTv provides isTv) {
                PhoneTheme(colorScheme = phoneDarkColors(primary = Accent)) {
                    TvTheme(colorScheme = tvDarkColors()) {
                        Surface(
                            modifier = Modifier.fillMaxSize(),
                            color = Color(0xFF0F1318),
                            contentColor = Color(0xFFE8EAED),
                        ) {
                            Box(Modifier.fillMaxSize().safeDrawingPadding()) { App(prefs) }
                        }
                    }
                }
            }
        }
    }
}

private fun isTelevision(context: Context): Boolean {
    val ui = context.getSystemService(Context.UI_MODE_SERVICE) as? UiModeManager
    return ui?.currentModeType == Configuration.UI_MODE_TYPE_TELEVISION ||
        context.packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK)
}

/** Screen-edge padding: roomy on TVs, tighter on phones. */
@Composable
private fun edge(tv: Dp): Dp = if (LocalConfiguration.current.screenWidthDp < 600) 16.dp else tv

private enum class Screen { Setup, Browse, Settings }

@Composable
private fun App(prefs: Prefs) {
    var screen by remember { mutableStateOf(if (prefs.serverUrl == null) Screen.Setup else Screen.Browse) }
    when (screen) {
        Screen.Setup -> SetupScreen(
            prefs,
            onConnected = { screen = Screen.Browse },
            onBack = if (prefs.serverUrl != null) ({ screen = Screen.Settings }) else null,
        )
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
private fun SetupScreen(prefs: Prefs, onConnected: () -> Unit, onBack: (() -> Unit)?) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val isTv = LocalIsTv.current
    var searching by remember { mutableStateOf(true) }
    var connecting by remember { mutableStateOf(false) }
    var found by remember { mutableStateOf<List<FoundServer>>(emptyList()) }
    var manual by remember { mutableStateOf(prefs.serverUrl?.removePrefix("http://") ?: "") }
    var away by remember { mutableStateOf(prefs.remoteUrl?.removePrefix("http://") ?: "") }
    var error by remember { mutableStateOf<String?>(null) }
    var searchRound by remember { mutableIntStateOf(0) }
    val firstFocus = remember { FocusRequester() }

    if (onBack != null) BackHandler(onBack = onBack)

    fun saveAway() {
        prefs.remoteUrl = away.takeIf { it.isNotBlank() }?.let { Api.normalize(it) }
    }

    fun connect(url: String, name: String?) {
        if (connecting) return
        scope.launch {
            connecting = true
            error = null
            saveAway()
            // Try the address given, then the away address if there is one.
            val working = Api.resolve(url, prefs.remoteUrl)
            if (working != null) {
                val info = runCatching { Api(working).info() }.getOrNull()
                prefs.serverUrl = url
                // A Tailscale address works at home too, so remember it as the away address as well.
                if (Api.isTailscale(url) && prefs.remoteUrl == null) prefs.remoteUrl = url
                prefs.serverName = name ?: info?.name ?: "PC"
                connecting = false
                onConnected()
            } else {
                connecting = false
                error = "Couldn't reach ${url.removePrefix("http://")}. Check that HomeStreamer is running on the PC. " +
                    "Away from home, use the PC's Tailscale address and make sure Tailscale is on, on this device too."
            }
        }
    }

    fun connectTyped() {
        when {
            manual.isNotBlank() -> connect(Api.normalize(manual), null)
            away.isNotBlank() -> connect(Api.normalize(away), null)
            else -> error = "Type the PC's address first."
        }
    }

    LaunchedEffect(searchRound) {
        searching = true
        found = Discovery.find(context)
        searching = false
        if (isTv) runCatching { firstFocus.requestFocus() }
    }

    LazyColumn(
        Modifier.fillMaxSize().padding(horizontal = edge(64.dp), vertical = edge(40.dp)),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item {
            Text("Connect to your PC", fontSize = 28.sp)
            Spacer(Modifier.height(6.dp))
            Text("Start HomeStreamer on the PC, then pick it below or type its address.", color = Muted, fontSize = 16.sp)
            Spacer(Modifier.height(16.dp))
            Text(
                when {
                    searching -> "Searching your home network…"
                    found.isEmpty() -> "No PC found on this network."
                    else -> "Found on your home network:"
                },
                fontSize = 18.sp,
            )
        }
        items(found, key = { it.url }) { s ->
            AppListItem(
                headline = s.name,
                supporting = s.url.removePrefix("http://"),
                leading = "🖥",
                onClick = { connect(s.url, s.name) },
                modifier = if (s == found.first()) Modifier.focusRequester(firstFocus) else Modifier,
            )
        }
        item {
            Spacer(Modifier.height(16.dp))
            Column(Modifier.widthIn(max = 520.dp)) {
                AppTextField(
                    value = manual,
                    onValueChange = { manual = it },
                    label = "Home address of the PC, like 192.168.1.20",
                    onDone = { connectTyped() },
                    modifier = Modifier.fillMaxWidth()
                        .then(if (found.isEmpty()) Modifier.focusRequester(firstFocus) else Modifier),
                )
                Spacer(Modifier.height(8.dp))
                AppTextField(
                    value = away,
                    onValueChange = { away = it },
                    label = "Away from home: Tailscale address, like 100.101.102.103",
                    onDone = { connectTyped() },
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(4.dp))
                Text(
                    "Fill in either one, or both. With both, the app switches between them automatically.",
                    color = Muted, fontSize = 14.sp,
                )
                Spacer(Modifier.height(12.dp))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    AppButton(if (connecting) "Connecting…" else "Connect", onClick = { connectTyped() }, enabled = !connecting)
                    Spacer(Modifier.width(12.dp))
                    AppButton("Search again", onClick = { searchRound++ }, enabled = !searching, primary = false)
                }
            }
            error?.let {
                Spacer(Modifier.height(16.dp))
                Text(it, color = ErrorColor, fontSize = 16.sp)
            }
        }
    }
}

// ---------- Browse folders and videos ----------

@Composable
private fun BrowseScreen(prefs: Prefs, onSettings: () -> Unit) {
    val context = LocalContext.current
    val isTv = LocalIsTv.current
    if (prefs.serverUrl == null) return
    var server by remember { mutableStateOf<String?>(null) }
    val stack = remember { mutableStateListOf("") }
    val titles = remember { mutableStateListOf(prefs.serverName ?: "Library") }
    var listing by remember { mutableStateOf<Listing?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    var recent by remember { mutableStateOf(prefs.recent()) }
    var toRemove by remember { mutableStateOf<Item?>(null) }
    var syncTick by remember { mutableIntStateOf(0) }
    val scope = rememberCoroutineScope()

    val firstFocus = remember { FocusRequester() }
    val current = stack.last()

    toRemove?.let { victim ->
        AlertDialog(
            onDismissRequest = { toRemove = null },
            title = { Text("Remove from Continue watching?") },
            text = { Text("\"${victim.name}\" will start from the beginning next time.") },
            confirmButton = {
                AppButton("Remove", onClick = {
                    prefs.forget(victim.id)
                    server?.let { ProgressSync.forget(it, victim.id) }
                    recent = prefs.recent()
                    toRemove = null
                })
            },
            dismissButton = { AppButton("Cancel", onClick = { toRemove = null }, primary = false) },
        )
    }

    // Refresh "continue watching" when coming back from the player.
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    DisposableEffect(lifecycle) {
        val obs = LifecycleEventObserver { _, e ->
            if (e == Lifecycle.Event.ON_RESUME) {
                recent = prefs.recent()
                syncTick++
            }
        }
        lifecycle.addObserver(obs)
        onDispose { lifecycle.removeObserver(obs) }
    }

    // "Continue watching" is kept on the PC and shared by every device.
    LaunchedEffect(server, syncTick) {
        val active = server ?: return@LaunchedEffect
        ProgressSync.pull(active, prefs)
        recent = prefs.recent()
    }

    LaunchedEffect(current, reload) {
        listing = null
        error = null
        try {
            // Find a working address (home or away) the first time, or after an error.
            val active = server ?: Api.resolve(prefs.serverUrl, prefs.remoteUrl)
            if (active == null) {
                error = if (prefs.remoteUrl == null)
                    "Couldn't reach ${prefs.serverName ?: "the PC"}. Is HomeStreamer running? To watch away from home, add the PC's Tailscale address in Settings."
                else
                    "Couldn't reach ${prefs.serverName ?: "the PC"} at home or through Tailscale. Check that HomeStreamer is running and Tailscale is on."
            } else {
                server = active
                listing = Api(active).browse(current)
            }
        } catch (e: Exception) {
            server = null
            error = "Lost the connection to ${prefs.serverName ?: "the PC"}. Is HomeStreamer running on the PC?"
        }
        if (isTv) runCatching { firstFocus.requestFocus() }
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
            Column(Modifier.weight(1f)) {
                Text(titles.joinToString("  ›  "), fontSize = 24.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (server != null && server == prefs.remoteUrl && server != prefs.serverUrl) {
                    Text("Away from home (Tailscale)", fontSize = 13.sp, color = Accent)
                }
            }
            AppButton("Settings", onClick = onSettings, primary = false)
        }
        Spacer(Modifier.height(16.dp))

        val l = listing
        when {
            error != null -> Column {
                Text(error!!, color = ErrorColor, fontSize = 17.sp)
                Spacer(Modifier.height(16.dp))
                AppButton("Try again", onClick = { reload++ }, modifier = Modifier.focusRequester(firstFocus))
            }
            l == null -> Text("Loading…", fontSize = 18.sp)
            else -> LazyColumn(
                verticalArrangement = Arrangement.spacedBy(6.dp),
                contentPadding = PaddingValues(bottom = 32.dp),
            ) {
                if (stack.size == 1 && recent.isNotEmpty()) {
                    item(key = "recent") {
                        Column {
                            Text("Continue watching", fontSize = 17.sp, color = Muted)
                            Text(
                                if (isTv) "Hold OK on a title to remove it" else "Press and hold a title to remove it",
                                fontSize = 13.sp, color = Muted,
                            )
                            Spacer(Modifier.height(8.dp))
                            LazyRow(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                items(recent, key = { "r" + it.first.id }) { (item, progress) ->
                                    RecentCard(item, progress, onLongClick = { toRemove = item }) { open(item) }
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
                    AppListItem(
                        headline = item.name,
                        supporting = if (item.isFolder) null else describe(item, prefs),
                        leading = if (item.isFolder) "📁" else "🎬",
                        onClick = { open(item) },
                        modifier = if (item == l.items.first()) Modifier.focusRequester(firstFocus) else Modifier,
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
private fun RecentCard(item: Item, progress: Float, onLongClick: () -> Unit, onClick: () -> Unit) {
    AppCard(onClick = onClick, onLongClick = onLongClick, modifier = Modifier.width(240.dp).height(96.dp)) {
        Column(Modifier.fillMaxSize().padding(12.dp)) {
            Text(item.name, maxLines = 2, overflow = TextOverflow.Ellipsis, fontSize = 16.sp, color = Color(0xFFE8EAED), modifier = Modifier.weight(1f))
            Box(Modifier.fillMaxWidth().height(4.dp).background(Color(0x33FFFFFF), RoundedCornerShape(2.dp))) {
                Box(
                    Modifier.fillMaxWidth(progress.coerceIn(0.02f, 1f)).height(4.dp)
                        .background(Accent, RoundedCornerShape(2.dp))
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

private val qualityChoices = listOf(
    "auto" to ("Automatic" to "Plays the original file when this device can, and converts on the PC when it can't or the file is too big for the connection."),
    "original" to ("Always the original file" to "Best quality. Some 4K, HDR or old AVI files may not play."),
    "1080" to ("Convert to 1080p" to "About 8 Mb/s. Needs FFmpeg on the PC."),
    "720" to ("Convert to 720p" to "About 3 Mb/s, for slow connections. Needs FFmpeg on the PC."),
)

private val sizeChoices = listOf(0.8f to "Small", 1.0f to "Medium", 1.25f to "Large", 1.5f to "Extra large")

@Composable
private fun SettingsScreen(prefs: Prefs, onBack: () -> Unit, onChangeServer: () -> Unit) {
    val isTv = LocalIsTv.current
    var lang by remember { mutableStateOf(prefs.subtitleLanguage) }
    var scale by remember { mutableStateOf(prefs.subtitleScale) }
    var quality by remember { mutableStateOf(prefs.quality) }
    val firstFocus = remember { FocusRequester() }
    BackHandler(onBack = onBack)
    LaunchedEffect(Unit) { if (isTv) runCatching { firstFocus.requestFocus() } }

    LazyColumn(
        Modifier.fillMaxSize().padding(horizontal = edge(64.dp), vertical = edge(32.dp)),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        item {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("Settings", fontSize = 28.sp, modifier = Modifier.weight(1f))
                AppButton("Done", onClick = onBack, primary = false, modifier = Modifier.focusRequester(firstFocus))
            }
            Spacer(Modifier.height(12.dp))
        }
        item {
            Text("Away from home", fontSize = 18.sp, color = Muted)
            AwayAddressRow(prefs)
        }
        item {
            Spacer(Modifier.height(8.dp))
            Text("Hebrew titles", fontSize = 18.sp, color = Muted)
            HebrewTitlesRow(prefs)
        }
        item {
            Spacer(Modifier.height(8.dp))
            Text("Video quality", fontSize = 18.sp, color = Muted)
        }
        items(qualityChoices) { (code, texts) ->
            AppListItem(
                headline = texts.first,
                supporting = texts.second,
                selected = quality == code,
                trailing = if (quality == code) "✓" else null,
                onClick = { quality = code; prefs.quality = code },
            )
        }
        item {
            Spacer(Modifier.height(8.dp))
            Text("Subtitle language", fontSize = 18.sp, color = Muted)
            Text("Picking subtitles in the player also updates this.", fontSize = 14.sp, color = Muted)
        }
        items(subtitleChoices) { (code, label) ->
            AppListItem(
                headline = label,
                selected = lang == code,
                trailing = if (lang == code) "✓" else null,
                onClick = { lang = code; prefs.subtitleLanguage = code },
            )
        }
        item { Spacer(Modifier.height(16.dp)); Text("Subtitle size", fontSize = 18.sp, color = Muted) }
        items(sizeChoices) { (value, label) ->
            AppListItem(
                headline = label,
                selected = scale == value,
                trailing = if (scale == value) "✓" else null,
                onClick = { scale = value; prefs.subtitleScale = value },
            )
        }
        item {
            Spacer(Modifier.height(16.dp))
            Text("PC", fontSize = 18.sp, color = Muted)
        }
        item {
            AppListItem(
                headline = "Change PC or home address",
                supporting = "Connected to ${prefs.serverName ?: ""} (${prefs.serverUrl?.removePrefix("http://") ?: ""})",
                onClick = onChangeServer,
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
            else "Saved, but the PC didn't answer right now. Check that Tailscale is on, on both the PC and this device."
        }
    }
    Column(Modifier.padding(vertical = 8.dp).widthIn(max = 520.dp)) {
        AppTextField(
            value = text,
            onValueChange = { text = it },
            label = "PC's Tailscale address, like 100.101.102.103",
            onDone = { save() },
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(8.dp))
        AppButton("Save", onClick = { save() })
        status?.let { Spacer(Modifier.height(6.dp)); Text(it, fontSize = 14.sp, color = Muted) }
    }
}

/** The TMDB key that lets the PC show Israeli films and shows by their Hebrew names. It's saved on the PC. */
@Composable
private fun HebrewTitlesRow(prefs: Prefs) {
    val scope = rememberCoroutineScope()
    var text by remember { mutableStateOf("") }
    var status by remember { mutableStateOf<String?>(null) }
    // Once the PC has a key, every device uses it: show that instead of an empty box.
    var saved by remember { mutableStateOf(false) }
    var editing by remember { mutableStateOf(false) }
    fun savedText(end: String) =
        "✓ Key saved on the PC" + (if (end.isNotEmpty()) " (ends in $end)" else "") +
            ". All your devices use it, so there's nothing to enter here, even after updating the app."
    LaunchedEffect(Unit) {
        val url = runCatching { Api.resolve(prefs.serverUrl, prefs.remoteUrl) }.getOrNull()
        if (url == null) {
            status = "Couldn't reach the PC to check the key."
            return@LaunchedEffect
        }
        runCatching { Api(url).tmdbKeyEnd() }.onSuccess { e ->
            if (e != null) {
                saved = true
                status = savedText(e)
            } else {
                status = "Off until a key is added."
            }
        }
    }
    fun save() {
        if (text.isBlank()) return
        status = "Checking…"
        scope.launch {
            val url = Api.resolve(prefs.serverUrl, prefs.remoteUrl)
            val err = if (url == null) "Couldn't reach the PC." else runCatching { Api(url).setTmdbKey(text) }
                .getOrElse { "Couldn't reach the PC." }
            if (err == null) {
                val end = text.trim().takeLast(4)
                text = ""
                saved = true
                editing = false
                status = "Saved. " + savedText(end) + " Hebrew names appear within about a minute. Open a folder again to see them."
            } else {
                status = err
            }
        }
    }
    Column(Modifier.padding(vertical = 8.dp).widthIn(max = 520.dp)) {
        Text(
            "Israeli films and shows are shown by their Hebrew names, looked up on TMDB (themoviedb.org). " +
                "The free TMDB API key is saved on the PC, so you only enter it once.",
            fontSize = 14.sp, color = Muted,
        )
        status?.let { Spacer(Modifier.height(6.dp)); Text(it, fontSize = 14.sp) }
        Spacer(Modifier.height(8.dp))
        if (saved && !editing) {
            AppButton("Change key", onClick = { editing = true }, primary = false)
        } else {
            AppTextField(
                value = text,
                onValueChange = { text = it },
                label = "TMDB API key",
                onDone = { save() },
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))
            AppButton("Save", onClick = { save() })
        }
    }
}
