package dev.mygo

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.widget.RemoteViews
import org.json.JSONObject

/**
 * The app's widget on the home screen: Go sets what it shows, and a press
 * comes back to it as an event.
 *
 * The provider is the system's, declared in the app's manifest (this plugin's
 * application fragment); what it draws is this module's layout, so that the
 * resources a plugin brings are its own.
 */
class WidgetPlugin : MyGoPlugin {
    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "set" -> {
                WidgetState.title = args.optString("Title")
                WidgetState.text = args.optString("Text")
                WidgetState.active = args.optBoolean("Active")
                WidgetState.refresh()
                reply.success(null)
            }
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }
}

/** What the widget shows, which Go sets and the provider draws. */
object WidgetState {
    @Volatile var title = ""
    @Volatile var text = ""
    @Volatile var active = false

    const val ACTION_CLICK = "dev.mygo.widget.CLICK"

    fun refresh() {
        val ctx = PluginHost.context ?: return
        val manager = AppWidgetManager.getInstance(ctx) ?: return
        val ids = manager.getAppWidgetIds(ComponentName(ctx, WidgetProvider::class.java))
        for (id in ids) manager.updateAppWidget(id, views(ctx))
    }

    fun views(ctx: Context): RemoteViews =
        RemoteViews(ctx.packageName, R.layout.mygo_widget).apply {
            setTextViewText(R.id.mygo_widget_title, title)
            setTextViewText(R.id.mygo_widget_text, text)
            setOnClickPendingIntent(R.id.mygo_widget_root, click(ctx))
        }

    private fun click(ctx: Context): PendingIntent {
        val intent = Intent(ctx, WidgetProvider::class.java).setAction(ACTION_CLICK)
        val flags = PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        return PendingIntent.getBroadcast(ctx, 0, intent, flags)
    }
}

/** The widget the system binds, as the manifest names it. */
class WidgetProvider : AppWidgetProvider() {
    override fun onUpdate(ctx: Context, manager: AppWidgetManager, ids: IntArray) {
        for (id in ids) manager.updateAppWidget(id, WidgetState.views(ctx))
    }

    override fun onReceive(ctx: Context, intent: Intent) {
        super.onReceive(ctx, intent)
        if (intent.action == WidgetState.ACTION_CLICK) {
            PluginHost.emit("widget", "click", null)
            WidgetState.refresh()
        }
    }
}
