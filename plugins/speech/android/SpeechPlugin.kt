package dev.mygo

import android.speech.tts.TextToSpeech
import org.json.JSONObject
import java.util.Locale

/**
 * The device's voice: what it says, how fast and high, and in which language.
 * The engine starts on the first call, which takes a moment, and is kept for
 * the next ones.
 *
 * A phone's system asks which service the app reaches for text to speech: the
 * plugin's manifest brings the <queries> entry Android 11 and later need.
 */
class SpeechPlugin : MyGoPlugin {
    private var engine: TextToSpeech? = null
    private var ready = false
    private var failed = false
    private var said = 0

    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "available" -> start(reply) { reply.success(JSONObject().put("available", true)) }
            "speak" -> start(reply) { t -> speak(t, args, reply) }
            "stop" -> {
                engine?.stop()
                reply.success(null)
            }
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }

    /** Runs then with a started engine, starting it the first time. */
    private fun start(reply: PluginReply, then: (TextToSpeech) -> Unit) {
        if (failed) {
            reply.failure(PluginFailure("Unavailable", "no voice to say text with"))
            return
        }
        val running = engine
        if (running != null && ready) {
            then(running)
            return
        }
        val ctx = PluginHost.context
        // The listener runs once the engine is made, so it reads it from
        // here rather than from a name of its own.
        val made = arrayOfNulls<TextToSpeech>(1)
        val t = TextToSpeech(ctx) { status ->
            ready = status == TextToSpeech.SUCCESS
            failed = !ready
            val started = made[0]
            if (ready && started != null) {
                then(started)
            } else {
                reply.failure(PluginFailure("Unavailable", "the voice did not start"))
            }
        }
        made[0] = t
        engine = t
    }

    private fun speak(t: TextToSpeech, args: JSONObject, reply: PluginReply) {
        val text = args.optString("Text")
        if (text.isEmpty()) {
            reply.failure(PluginFailure("BadArgument", "no text to say"))
            return
        }
        val language = args.optString("Language")
        if (language.isNotEmpty()) {
            val result = t.setLanguage(Locale.forLanguageTag(language))
            if (result == TextToSpeech.LANG_MISSING_DATA || result == TextToSpeech.LANG_NOT_SUPPORTED) {
                reply.failure(PluginFailure("NoLanguage", language))
                return
            }
        }
        if (args.has("Rate")) t.setSpeechRate(args.optDouble("Rate", 1.0).toFloat())
        if (args.has("Pitch")) t.setPitch(args.optDouble("Pitch", 1.0).toFloat())
        said++
        // Each text replaces the one being said, as the Go side says.
        val code = t.speak(text, TextToSpeech.QUEUE_FLUSH, null, "mygo-$said")
        if (code == TextToSpeech.SUCCESS) {
            reply.success(null)
        } else {
            reply.failure(PluginFailure("Failed", "the voice took no text"))
        }
    }
}
