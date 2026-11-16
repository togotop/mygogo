package dev.mygo

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import org.json.JSONObject

/**
 * How the device reaches the network: ConnectivityManager's default network
 * and what it can do, reported whenever it changes.
 */
class NetPathPlugin : MyGoPlugin {
    private var watching: ConnectivityManager.NetworkCallback? = null

    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "current" -> reply.success(report(manager()?.let { m ->
                m.getNetworkCapabilities(m.activeNetwork)
            }))
            "watch" -> watch(reply)
            "stopWatch" -> {
                stopWatch()
                reply.success(null)
            }
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }

    private fun manager(): ConnectivityManager? =
        PluginHost.context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager

    private fun watch(reply: PluginReply) {
        val m = manager() ?: return reply.failure(PluginFailure("Unavailable", "no connectivity manager"))
        if (watching != null) return reply.success(null)
        val cb = object : ConnectivityManager.NetworkCallback() {
            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                PluginHost.emit("netpath", "event", report(caps))
            }

            override fun onLost(network: Network) {
                // The default network went: another may take over, and its
                // capabilities come as they do.
                PluginHost.emit("netpath", "event", report(null))
            }
        }
        try {
            m.registerDefaultNetworkCallback(cb)
        } catch (e: SecurityException) {
            reply.failure(PluginFailure("Permission", e.toString()))
            return
        }
        watching = cb
        reply.success(null)
    }

    private fun stopWatch() {
        val m = manager() ?: return
        val cb = watching ?: return
        watching = null
        runCatching { m.unregisterNetworkCallback(cb) }
    }

    /** What the capabilities say, for the JSON Go reads. */
    private fun report(caps: NetworkCapabilities?): JSONObject {
        val online = caps != null &&
            caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
            caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
        val kind = when {
            caps == null -> "none"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN) -> "vpn"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
            else -> "other"
        }
        val expensive = caps != null && !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)
        val constrained = caps != null && !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_CONGESTED)
        return JSONObject()
            .put("known", true)
            .put("online", online)
            .put("interface", kind)
            .put("expensive", expensive)
            .put("constrained", constrained)
    }
}
