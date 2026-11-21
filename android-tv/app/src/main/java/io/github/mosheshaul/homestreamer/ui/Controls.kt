package io.github.mosheshaul.homestreamer.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.sp
import androidx.compose.material3.Button as PhoneButton
import androidx.compose.material3.ListItem as PhoneListItem
import androidx.compose.material3.OutlinedButton as PhoneOutlinedButton
import androidx.tv.material3.Button as TvButton
import androidx.tv.material3.ListItem as TvListItem
import androidx.tv.material3.OutlinedButton as TvOutlinedButton
import androidx.tv.material3.Surface as TvSurface
import androidx.tv.material3.Text as TvText

/**
 * True on a TV (remote control), false on phones and tablets (touch).
 * TV-style controls are built for focus and the D-pad and don't behave well
 * with touch, so each control below picks the right kind for the device.
 */
val LocalIsTv = staticCompositionLocalOf { false }

@Composable
fun AppButton(
    label: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    primary: Boolean = true,
) {
    if (LocalIsTv.current) {
        if (primary) TvButton(onClick = onClick, modifier = modifier, enabled = enabled) { TvText(label) }
        else TvOutlinedButton(onClick = onClick, modifier = modifier, enabled = enabled) { TvText(label) }
    } else {
        if (primary) PhoneButton(onClick = onClick, modifier = modifier, enabled = enabled) { Text(label) }
        else PhoneOutlinedButton(onClick = onClick, modifier = modifier, enabled = enabled) { Text(label) }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun AppListItem(
    headline: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    supporting: String? = null,
    leading: String? = null,
    trailing: String? = null,
    selected: Boolean = false,
    onLongClick: (() -> Unit)? = null,
) {
    if (LocalIsTv.current) {
        TvListItem(
            selected = selected,
            onClick = onClick,
            onLongClick = onLongClick,
            headlineContent = { TvText(headline, maxLines = 1, overflow = TextOverflow.Ellipsis) },
            supportingContent = if (supporting != null) ({ TvText(supporting) }) else null,
            leadingContent = if (leading != null) ({ TvText(leading, fontSize = 22.sp) }) else null,
            trailingContent = if (trailing != null) ({ TvText(trailing) }) else null,
            modifier = modifier,
        )
    } else {
        PhoneListItem(
            headlineContent = { Text(headline, maxLines = 2, overflow = TextOverflow.Ellipsis) },
            supportingContent = if (supporting != null) ({ Text(supporting) }) else null,
            leadingContent = if (leading != null) ({ Text(leading, fontSize = 22.sp) }) else null,
            trailingContent = if (trailing != null) ({ Text(trailing) }) else null,
            colors = ListItemDefaults.colors(
                containerColor = if (selected) Color(0xFF263040) else Color(0xFF1A1F26),
            ),
            modifier = modifier.combinedClickable(onClick = onClick, onLongClick = onLongClick),
        )
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun AppCard(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    onLongClick: (() -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    if (LocalIsTv.current) {
        // On a TV, holding OK on the remote is a long press.
        TvSurface(onClick = onClick, onLongClick = onLongClick, modifier = modifier) {
            androidx.compose.foundation.layout.Column(content = content)
        }
    } else {
        Card(
            modifier = modifier.clip(CardDefaults.shape).combinedClickable(onClick = onClick, onLongClick = onLongClick),
            colors = CardDefaults.cardColors(containerColor = Color(0xFF1A1F26)),
            content = content,
        )
    }
}

/** A text box that works with touch keyboards and with the TV remote's on-screen keyboard. */
@Composable
fun AppTextField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    onDone: () -> Unit,
    modifier: Modifier = Modifier,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        label = { Text(label) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done),
        keyboardActions = KeyboardActions(onDone = { onDone() }),
        modifier = modifier,
    )
}
