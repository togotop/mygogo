package dev.mygo

import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import android.media.AudioManager
import android.provider.Settings
import android.view.KeyEvent
import org.json.JSONObject
import kotlin.math.abs
import kotlin.math.roundToInt

/**
 * The phone's media volume, its ringer and the transport keys: AudioManager's
 * STREAM_MUSIC, so the alarm and the ringer keep their own levels.
 *
 * Silent is the mode the system guards: it needs the do not disturb access,
 * which the user grants on the page openSilenceSettings shows, and without it
 * the ringer answers DndAccess.
 */
class AudioPlugin : MyGoPlugin {
    private val manager: AudioManager?
        get() = PluginHost.context.getSystemService(Context.AUDIO_SERVICE) as? AudioManager

    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        val am = manager
        if (am == null) {
            if (method == "available") {
                reply.success(JSONObject().put("available", false))
            } else {
                reply.failure(PluginFailure("Unavailable", "no audio manager"))
            }
            return
        }
        when (method) {
            "available" -> reply.success(JSONObject().put("available", true))
            "current" -> reply.success(report(am))
            "set" -> set(am, args.optInt("percent"), reply)
            "adjust" -> adjust(am, args.optInt("steps"), reply)
            "mute" -> {
                @Suppress("DEPRECATION")
                am.adjustStreamVolume(
                    AudioManager.STREAM_MUSIC,
                    if (args.optBoolean("on", true)) AudioManager.ADJUST_MUTE else AudioManager.ADJUST_UNMUTE,
                    0,
                )
                reply.success(null)
            }
            "ringer" -> ringer(am, args.optString("mode"), reply)
            "silenceAllowed" -> {
                val nm = PluginHost.context.getSystemService(Context.NOTIFICATION_SERVICE) as? NotificationManager
                reply.success(JSONObject().put("allowed", nm?.isNotificationPolicyAccessGranted == true))
            }
            "openSilenceSettings" -> openSilenceSettings(reply)
            "press" -> press(am, args.optString("key"), reply)
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }

    private fun set(am: AudioManager, percent: Int, reply: PluginReply) {
        val p = percent.coerceIn(0, 100)
        val max = am.getStreamMaxVolume(AudioManager.STREAM_MUSIC).coerceAtLeast(1)
        // A muted stream comes back at the volume it had: asking for any
        // volume at all unmutes it.
        if (p > 0 && am.isStreamMute(AudioManager.STREAM_MUSIC)) {
            @Suppress("DEPRECATION")
            am.adjustStreamVolume(AudioManager.STREAM_MUSIC, AudioManager.ADJUST_UNMUTE, 0)
        }
        am.setStreamVolume(
            AudioManager.STREAM_MUSIC,
            (p * max / 100.0).roundToInt().coerceIn(0, max),
            0,
        )
        reply.success(null)
    }

    private fun adjust(am: AudioManager, steps: Int, reply: PluginReply) {
        if (steps == 0) {
            reply.success(null)
            return
        }
        val direction = if (steps > 0) AudioManager.ADJUST_RAISE else AudioManager.ADJUST_LOWER
        @Suppress("DEPRECATION")
        repeat(abs(steps)) { am.adjustStreamVolume(AudioManager.STREAM_MUSIC, direction, 0) }
        reply.success(null)
    }

    private fun ringer(am: AudioManager, mode: String, reply: PluginReply) {
        val target = when (mode) {
            "normal" -> AudioManager.RINGER_MODE_NORMAL
            "vibrate" -> AudioManager.RINGER_MODE_VIBRATE
            "silent" -> AudioManager.RINGER_MODE_SILENT
            else -> {
                reply.failure(PluginFailure("BadArgument", "mode $mode"))
                return
            }
        }
        try {
            am.ringerMode = target
        } catch (e: SecurityException) {
            // Silent wants the do not disturb access the user grants.
            reply.failure(PluginFailure("DndAccess", e.toString()))
            return
        }
        reply.success(null)
    }

    private fun openSilenceSettings(reply: PluginReply) {
        val intent = Intent(Settings.ACTION_NOTIFICATION_POLICY_ACCESS_SETTINGS)
        val activity = PluginHost.activity
        try {
            if (activity != null) {
                activity.startActivity(intent)
            } else {
                intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                PluginHost.context.startActivity(intent)
            }
        } catch (e: Exception) {
            reply.failure(PluginFailure("Unavailable", e.toString()))
            return
        }
        reply.success(null)
    }

    private fun press(am: AudioManager, key: String, reply: PluginReply) {
        val code = when (key) {
            "play-pause" -> KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE
            "play" -> KeyEvent.KEYCODE_MEDIA_PLAY
            "pause" -> KeyEvent.KEYCODE_MEDIA_PAUSE
            "next" -> KeyEvent.KEYCODE_MEDIA_NEXT
            "previous" -> KeyEvent.KEYCODE_MEDIA_PREVIOUS
            "stop" -> KeyEvent.KEYCODE_MEDIA_STOP
            "forward" -> KeyEvent.KEYCODE_MEDIA_FAST_FORWARD
            "rewind" -> KeyEvent.KEYCODE_MEDIA_REWIND
            else -> {
                reply.failure(PluginFailure("BadArgument", "key $key"))
                return
            }
        }
        @Suppress("DEPRECATION")
        am.dispatchMediaKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, code))
        @Suppress("DEPRECATION")
        am.dispatchMediaKeyEvent(KeyEvent(KeyEvent.ACTION_UP, code))
        reply.success(null)
    }

    private fun report(am: AudioManager): JSONObject {
        val max = am.getStreamMaxVolume(AudioManager.STREAM_MUSIC).coerceAtLeast(1)
        val volume = am.getStreamVolume(AudioManager.STREAM_MUSIC)
        val ringer = when (am.ringerMode) {
            AudioManager.RINGER_MODE_VIBRATE -> "vibrate"
            AudioManager.RINGER_MODE_SILENT -> "silent"
            else -> "normal"
        }
        return JSONObject()
            .put("percent", (volume * 100.0 / max).roundToInt())
            .put("muted", am.isStreamMute(AudioManager.STREAM_MUSIC))
            .put("ringer", ringer)
    }
}
