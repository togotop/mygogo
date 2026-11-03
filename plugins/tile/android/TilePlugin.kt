package dev.mygo

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.RectF
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import org.json.JSONObject

/**
 * The app's tile in the quick settings. Go sets what it shows, and a press
 * comes back to it as an event.
 *
 * A TileService is the system's, declared in the app's manifest (this plugin's
 * application fragment), so the state the plugin keeps is shared with it here.
 */
class TilePlugin : MyGoPlugin {
    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "set" -> {
                TileState.label = args.optString("Label")
                TileState.subtitle = args.optString("Subtitle")
                TileState.active = args.optBoolean("Active")
                TileState.unavailable = args.optBoolean("Unavailable")
                TileState.service?.refresh()
                reply.success(null)
            }
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }
}

/** What the tile shows, which Go sets and the service draws. */
object TileState {
    @Volatile var label = ""
    @Volatile var subtitle = ""
    @Volatile var active = false
    @Volatile var unavailable = false
    @Volatile var service: MyGoTileService? = null

    fun apply(t: android.service.quicksettings.Tile) {
        t.label = label
        if (Build.VERSION.SDK_INT >= 29) {
            t.subtitle = subtitle
            t.state = when {
                unavailable -> Tile.STATE_UNAVAILABLE
                active -> Tile.STATE_ACTIVE
                else -> Tile.STATE_INACTIVE
            }
        } else {
            t.state = if (active) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE
        }
        // A tile needs an icon; the system shows the app's own when it has
        // none to draw.
        t.icon = icon()
        t.updateTile()
    }

    /** A dot, white over the tile's own colors, drawn once. */
    private var drawn: Bitmap? = null
    private fun icon(): android.graphics.drawable.Icon {
        val b = drawn ?: Bitmap.createBitmap(96, 96, Bitmap.Config.ARGB_8888).also {
            val c = Canvas(it)
            val p = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = Color.WHITE }
            c.drawRoundRect(RectF(18f, 18f, 78f, 78f), 18f, 18f, p)
            drawn = it
        }
        return android.graphics.drawable.Icon.createWithBitmap(b)
    }
}

/** The tile, which the system binds as the manifest names it. */
class MyGoTileService : TileService() {
    override fun onStartListening() {
        super.onStartListening()
        TileState.service = this
        refresh()
    }

    override fun onStopListening() {
        if (TileState.service === this) TileState.service = null
        super.onStopListening()
    }

    override fun onClick() {
        super.onClick()
        PluginHost.emit("tile", "click", null)
        refresh()
    }

    fun refresh() {
        qsTile?.let { TileState.apply(it) }
    }
}
