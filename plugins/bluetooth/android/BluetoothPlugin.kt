package dev.mygo

import android.Manifest
import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothGatt
import android.bluetooth.BluetoothGattCallback
import android.bluetooth.BluetoothGattCharacteristic
import android.bluetooth.BluetoothGattDescriptor
import android.bluetooth.BluetoothManager
import android.bluetooth.BluetoothProfile
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanFilter
import android.bluetooth.le.ScanResult
import android.bluetooth.le.ScanSettings
import android.content.Context
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.ParcelUuid
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID

/**
 * The device's Bluetooth LE radio: scans for advertisements, and talks GATT to
 * one device at a time. Scanning needs the system's permission to see devices
 * (BLUETOOTH_SCAN since Android 12, the location one before it), which the
 * first scan asks for.
 *
 * What it reports goes to Go as events: kind "device" (address, name, rssi),
 * "state" (id, state) and "value" (id, char, value), through PluginHost.emit.
 */
class BluetoothPlugin : MyGoPlugin {
    private val main = Handler(Looper.getMainLooper())
    private val adapter: BluetoothAdapter?
        get() = (PluginHost.context.getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager)?.adapter

    private var scanning = false
    private var next = 1L
    private val connections = HashMap<Long, BluetoothGatt>()

    /** What one call waits for: the characteristic it is about, and its answer. */
    private inner class Pending(val id: Long, val op: String, val char: String, val reply: PluginReply) {
        val timeout = Runnable {
            pending.remove(key(id, op, char))
            reply.failure(PluginFailure("Timeout"))
        }
    }

    private val pending = HashMap<String, Pending>()

    private fun key(id: Long, op: String, char: String) = "$id/$op/$char"

    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "available" -> {
                val a = adapter
                reply.success(JSONObject().put("available", a != null && a.isEnabled))
            }
            "scan" -> scan(args, reply)
            "stopScan" -> {
                stopScan()
                reply.success(null)
            }
            "connect" -> connect(args, reply)
            "disconnect" -> {
                disconnect(args.optLong("id"))
                reply.success(null)
            }
            "read" -> read(args, reply)
            "write" -> write(args, reply)
            "subscribe" -> subscribe(args, reply, true)
            "unsubscribe" -> subscribe(args, reply, false)
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }

    private fun scan(args: JSONObject, reply: PluginReply) {
        val a = adapter
        if (a == null || !a.isEnabled) {
            reply.failure(PluginFailure("Unavailable", "Bluetooth is off"))
            return
        }
        val scanner = a.bluetoothLeScanner
        if (scanner == null) {
            reply.failure(PluginFailure("Unavailable", "no scanner"))
            return
        }
        val permissions = if (Build.VERSION.SDK_INT >= 31) {
            arrayOf(Manifest.permission.BLUETOOTH_SCAN)
        } else {
            arrayOf(Manifest.permission.ACCESS_FINE_LOCATION)
        }
        PluginHost.requestPermissions(permissions) { granted ->
            if (!granted) {
                reply.failure(PluginFailure("Permission", "the app may not scan for devices"))
                return@requestPermissions
            }
            val filters = ArrayList<ScanFilter>()
            val service = args.optString("service")
            if (service.isNotEmpty()) {
                filters.add(ScanFilter.Builder().setServiceUuid(ParcelUuid(uuidOf(service))).build())
            }
            val settings = ScanSettings.Builder()
                .setScanMode(ScanSettings.SCAN_MODE_LOW_LATENCY)
                .build()
            try {
                scanning = true
                scanner.startScan(filters, settings, callback)
            } catch (e: SecurityException) {
                scanning = false
                reply.failure(PluginFailure("Permission", e.toString()))
                return@requestPermissions
            }
            reply.success(null)
        }
    }

    private val callback = object : ScanCallback() {
        override fun onScanResult(callbackType: Int, result: ScanResult) = report(result)

        override fun onBatchScanResults(results: MutableList<ScanResult>) = results.forEach { report(it) }

        override fun onScanFailed(errorCode: Int) {
            scanning = false
            PluginHost.emit("bluetooth", "event", JSONObject().put("kind", "error").put("error", "scan failed: $errorCode"))
        }

        private fun report(r: ScanResult) {
            val d: BluetoothDevice = r.device ?: return
            // A device without a name is common while its advertisement has
            // not been scanned yet: it is reported anyway, by its address.
            @Suppress("MissingPermission")
            val name = runCatching { d.name }.getOrNull() ?: ""
            PluginHost.emit(
                "bluetooth", "event",
                JSONObject()
                    .put("kind", "device")
                    .put("address", d.address)
                    .put("name", name)
                    .put("rssi", r.rssi),
            )
        }
    }

    private fun stopScan() {
        if (!scanning) return
        scanning = false
        runCatching { adapter?.bluetoothLeScanner?.stopScan(callback) }
    }

    private fun connect(args: JSONObject, reply: PluginReply) {
        val a = adapter
        if (a == null || !a.isEnabled) {
            reply.failure(PluginFailure("Unavailable", "Bluetooth is off"))
            return
        }
        val address = args.optString("address")
        if (address.isEmpty()) {
            reply.failure(PluginFailure("BadArgument", "no address"))
            return
        }
        val permissions = if (Build.VERSION.SDK_INT >= 31) {
            arrayOf(Manifest.permission.BLUETOOTH_CONNECT)
        } else {
            arrayOf(Manifest.permission.ACCESS_FINE_LOCATION)
        }
        PluginHost.requestPermissions(permissions) { granted ->
            if (!granted) {
                reply.failure(PluginFailure("Permission", "the app may not connect to devices"))
                return@requestPermissions
            }
            val id = next++
            val device = try {
                a.getRemoteDevice(address)
            } catch (e: IllegalArgumentException) {
                reply.failure(PluginFailure("BadArgument", e.toString()))
                return@requestPermissions
            }
            val gatt = try {
                @Suppress("MissingPermission")
                device.connectGatt(PluginHost.context, false, gattCallback(id, reply), BluetoothDevice.TRANSPORT_LE)
            } catch (e: SecurityException) {
                reply.failure(PluginFailure("Permission", e.toString()))
                return@requestPermissions
            }
            if (gatt == null) {
                reply.failure(PluginFailure("Failed", "no GATT link"))
                return@requestPermissions
            }
            connections[id] = gatt
            // A device that does not answer leaves Go waiting forever otherwise.
            val lost = Runnable {
                if (connected.containsKey(id)) {
                    connections.remove(id)
                    runCatching { gatt.close() }
                    finishConnect(id, reply, PluginFailure("Timeout"))
                }
            }
            connected[id] = lost to reply
            main.postDelayed(lost, connectTimeoutMs)
        }
    }

    private val connectTimeoutMs = 15_000L

    /** The reply of a connection still discovering its services, by id. */
    private val connected = HashMap<Long, Pair<Runnable, PluginReply>>()

    private fun gattCallback(id: Long, reply: PluginReply) = object : BluetoothGattCallback() {
        override fun onConnectionStateChange(gatt: BluetoothGatt, status: Int, newState: Int) {
            if (newState == BluetoothProfile.STATE_CONNECTED) {
                @Suppress("MissingPermission")
                val started = runCatching { gatt.discoverServices() }.getOrDefault(false)
                if (!started) {
                    finishConnect(id, reply, PluginFailure("Failed", "services did not start"))
                }
                return
            }
            val wasConnected = connections.remove(id) != null
            runCatching { gatt.close() }
            emitState(id, newState, status)
            if (wasConnected) {
                finishConnect(id, reply, PluginFailure("Disconnected", "state $newState"))
                failPending(id, "Disconnected")
            }
        }

        override fun onServicesDiscovered(gatt: BluetoothGatt, status: Int) {
            if (status != BluetoothGatt.GATT_SUCCESS) {
                finishConnect(id, reply, PluginFailure("Failed", "services: $status"))
                return
            }
            val services = JSONArray()
            @Suppress("MissingPermission")
            for (s in gatt.services) {
                val chars = JSONArray()
                for (c in s.characteristics) chars.put(shortUuid(c.uuid))
                services.put(JSONObject().put("uuid", shortUuid(s.uuid)).put("characteristics", chars))
            }
            finishConnect(id, reply, null, services)
        }

        override fun onCharacteristicRead(gatt: BluetoothGatt, c: BluetoothGattCharacteristic, status: Int) {
            onRead(id, c, status)
        }

        override fun onCharacteristicRead(gatt: BluetoothGatt, c: BluetoothGattCharacteristic, value: ByteArray, status: Int) {
            if (status == BluetoothGatt.GATT_SUCCESS) c.value = value
            onRead(id, c, status)
        }

        override fun onCharacteristicWrite(gatt: BluetoothGatt, c: BluetoothGattCharacteristic, status: Int) {
            finish(id, "write", c.uuid, if (status == BluetoothGatt.GATT_SUCCESS) null else PluginFailure("Failed", "write: $status"))
        }

        override fun onCharacteristicChanged(gatt: BluetoothGatt, c: BluetoothGattCharacteristic) {
            emitValue(id, c)
        }

        override fun onCharacteristicChanged(gatt: BluetoothGatt, c: BluetoothGattCharacteristic, value: ByteArray) {
            c.value = value
            emitValue(id, c)
        }

        override fun onDescriptorWrite(gatt: BluetoothGatt, d: BluetoothGattDescriptor, status: Int) {
            // The subscription is the descriptor write that follows the call.
            finish(id, "subscribe", d.characteristic.uuid, if (status == BluetoothGatt.GATT_SUCCESS) null else PluginFailure("Failed", "subscribe: $status"))
        }
    }

    private fun onRead(id: Long, c: BluetoothGattCharacteristic, status: Int) {
        if (status != BluetoothGatt.GATT_SUCCESS) {
            finish(id, "read", c.uuid, PluginFailure("Failed", "read: $status"))
            return
        }
        val value = android.util.Base64.encodeToString(c.value ?: ByteArray(0), android.util.Base64.NO_WRAP)
        finish(id, "read", c.uuid, null, JSONObject().put("value", value))
    }

    private fun emitValue(id: Long, c: BluetoothGattCharacteristic) {
        val value = android.util.Base64.encodeToString(c.value ?: ByteArray(0), android.util.Base64.NO_WRAP)
        PluginHost.emit(
            "bluetooth", "event",
            JSONObject().put("kind", "value").put("id", id).put("char", shortUuid(c.uuid)).put("value", value),
        )
    }

    private fun emitState(id: Long, state: Int, status: Int) {
        PluginHost.emit(
            "bluetooth", "event",
            JSONObject().put("kind", "state").put("id", id).put("state", state).put("status", status),
        )
    }

    /** Answers a connection's first call once: the services, or why not. */
    private fun finishConnect(id: Long, reply: PluginReply, error: PluginFailure?, services: JSONArray? = null) {
        val waiting = connected.remove(id) ?: return
        main.removeCallbacks(waiting.first)
        if (error != null) {
            connections.remove(id)
            reply.failure(error)
            return
        }
        reply.success(JSONObject().put("id", id).put("services", services))
    }

    /** Answers the call waiting for op on a characteristic, if any. */
    private fun finish(id: Long, op: String, char: UUID, error: PluginFailure?, result: JSONObject? = null) {
        val k = key(id, op, shortUuid(char))
        val p = pending.remove(k) ?: return
        main.removeCallbacks(p.timeout)
        if (error != null) p.reply.failure(error) else p.reply.success(result)
    }

    private fun failPending(id: Long, code: String) {
        for (k in pending.keys.filter { it.startsWith("$id/") }) {
            val p = pending.remove(k) ?: continue
            main.removeCallbacks(p.timeout)
            p.reply.failure(PluginFailure(code))
        }
    }

    private fun read(args: JSONObject, reply: PluginReply) {
        val id = args.optLong("id")
        val gatt = connections[id] ?: return reply.failure(PluginFailure("Disconnected"))
        val c = characteristic(gatt, args.optString("char")) ?: return reply.failure(PluginFailure("NoCharacteristic", args.optString("char")))
        wait(id, "read", c.uuid, reply)
        @Suppress("MissingPermission")
        val started = runCatching { gatt.readCharacteristic(c) }.getOrDefault(false)
        if (!started) finish(id, "read", c.uuid, PluginFailure("Busy"))
    }

    private fun write(args: JSONObject, reply: PluginReply) {
        val id = args.optLong("id")
        val gatt = connections[id] ?: return reply.failure(PluginFailure("Disconnected"))
        val c = characteristic(gatt, args.optString("char")) ?: return reply.failure(PluginFailure("NoCharacteristic", args.optString("char")))
        val value = android.util.Base64.decode(args.optString("value"), android.util.Base64.NO_WRAP)
        val type = if (args.optBoolean("response", true)) {
            BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT
        } else {
            BluetoothGattCharacteristic.WRITE_TYPE_NO_RESPONSE
        }
        wait(id, "write", c.uuid, reply)
        @Suppress("MissingPermission")
        val started = runCatching {
            if (Build.VERSION.SDK_INT >= 33) {
                gatt.writeCharacteristic(c, value, type) == BluetoothGatt.GATT_SUCCESS
            } else {
                c.writeType = type
                c.value = value
                gatt.writeCharacteristic(c)
            }
        }.getOrDefault(false)
        if (!started) finish(id, "write", c.uuid, PluginFailure("Busy"))
    }

    private fun subscribe(args: JSONObject, reply: PluginReply, on: Boolean) {
        val id = args.optLong("id")
        val gatt = connections[id] ?: return reply.failure(PluginFailure("Disconnected"))
        val c = characteristic(gatt, args.optString("char")) ?: return reply.failure(PluginFailure("NoCharacteristic", args.optString("char")))
        if (!on) {
            @Suppress("MissingPermission")
            runCatching { gatt.setCharacteristicNotification(c, false) }
            // Go drops what arrives after its unsubscribe; the descriptor is
            // cleared so the radio stops sending it.
            val d = c.getDescriptor(cccd) ?: return reply.success(null)
            @Suppress("MissingPermission")
            runCatching {
                d.value = BluetoothGattDescriptor.DISABLE_NOTIFICATION_VALUE
                gatt.writeDescriptor(d)
            }
            return reply.success(null)
        }
        val d = c.getDescriptor(cccd) ?: return reply.failure(PluginFailure("NoDescriptor", "no CCCD"))
        wait(id, "subscribe", c.uuid, reply)
        @Suppress("MissingPermission")
        val started = runCatching {
            gatt.setCharacteristicNotification(c, true)
            d.value = if (c.properties and BluetoothGattCharacteristic.PROPERTY_NOTIFY != 0) {
                BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE
            } else {
                BluetoothGattDescriptor.ENABLE_INDICATION_VALUE
            }
            gatt.writeDescriptor(d)
        }.getOrDefault(false)
        if (!started) finish(id, "subscribe", c.uuid, PluginFailure("Busy"))
    }

    private fun wait(id: Long, op: String, char: UUID, reply: PluginReply) {
        val k = key(id, op, shortUuid(char))
        pending.remove(k)?.let { main.removeCallbacks(it.timeout) }
        val p = Pending(id, op, shortUuid(char), reply)
        pending[k] = p
        main.postDelayed(p.timeout, replyTimeoutMs)
    }

    private val replyTimeoutMs = 10_000L

    private fun disconnect(id: Long) {
        val gatt = connections.remove(id) ?: return
        runCatching { gatt.disconnect() }
        runCatching { gatt.close() }
        failPending(id, "Disconnected")
        emitState(id, BluetoothProfile.STATE_DISCONNECTED, 0)
    }

    private fun characteristic(gatt: BluetoothGatt, s: String): BluetoothGattCharacteristic? {
        if (s.isEmpty()) return null
        val want = uuidOf(s)
        @Suppress("MissingPermission")
        for (service in gatt.services) {
            service.getCharacteristic(want)?.let { return it }
        }
        return null
    }

    private fun uuidOf(s: String): UUID {
        val t = s.trim()
        if (t.length <= 8 && t.all { it.isLetterOrDigit() }) {
            return UUID.fromString("0000" + t.lowercase().padStart(4, '0') + "-0000-1000-8000-00805f9b34fb")
        }
        return UUID.fromString(t)
    }

    private fun shortUuid(u: UUID): String {
        val s = u.toString()
        return if (s.startsWith("0000") && s.endsWith("-0000-1000-8000-00805f9b34fb")) s.substring(4, 8) else s
    }

    private val cccd: UUID = uuidOf("2902")
}
