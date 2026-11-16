package dev.mygo

import android.os.Build
import org.json.JSONObject

/** What the app runs on: the maker, the model and the system, as Android tells. */
class DevicePlugin : MyGoPlugin {
    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "info" -> reply.success(
                JSONObject()
                    .put("manufacturer", Build.MANUFACTURER)
                    .put("model", Build.MODEL)
                    .put("system", "Android")
                    .put("systemVersion", Build.VERSION.RELEASE)
                    .put("sdk", Build.VERSION.SDK_INT),
            )
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }
}
