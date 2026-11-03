package dev.mygo

import android.nfc.NdefMessage
import android.nfc.NdefRecord
import android.nfc.NfcAdapter
import android.nfc.NfcManager
import android.nfc.Tag
import android.nfc.tech.IsoDep
import android.nfc.tech.Ndef
import android.nfc.tech.NdefFormatable
import android.nfc.tech.NfcA
import android.nfc.tech.NfcB
import android.nfc.tech.NfcF
import android.nfc.tech.NfcV
import org.json.JSONArray
import org.json.JSONObject
import java.nio.charset.StandardCharsets
import java.util.Locale

/**
 * The NFC reader: reader mode, with the system's own sound and vibration, so a
 * tag the phone is held against is reported with what it holds. The tag last
 * seen stays here for the write and the exchange that follow.
 *
 * What it reports goes to Go as events: kind "tag" with the id, the
 * technologies and the records.
 */
class NfcPlugin : MyGoPlugin {
    private val adapter: NfcAdapter?
        get() = (PluginHost.context.getSystemService(NfcManager::class.java))?.defaultAdapter

    private var scanning = false
    private var last: Tag? = null

    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "available" -> {
                val a = adapter
                reply.success(JSONObject().put("available", a != null && a.isEnabled))
            }
            "scan" -> scan(reply)
            "stopScan" -> {
                stopScan()
                reply.success(null)
            }
            "writeText" -> writeText(args.optString("text"), reply)
            "transceive" -> transceive(args.optString("tech"), args.optString("data"), reply)
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }

    private fun scan(reply: PluginReply) {
        if (scanning) {
            // Reader mode is already on: a second would double every tag.
            reply.success(null)
            return
        }
        val a = adapter
        if (a == null || !a.isEnabled) {
            reply.failure(PluginFailure("Unavailable", "NFC is off"))
            return
        }
        val activity = PluginHost.activity
        if (activity == null) {
            reply.failure(PluginFailure("NoActivity", "no window to read tags in"))
            return
        }
        val flags = NfcAdapter.FLAG_READER_NFC_A or NfcAdapter.FLAG_READER_NFC_B or
            NfcAdapter.FLAG_READER_NFC_F or NfcAdapter.FLAG_READER_NFC_V
        try {
            a.enableReaderMode(activity, callback, flags, null)
        } catch (e: Exception) {
            reply.failure(PluginFailure("Failed", e.toString()))
            return
        }
        scanning = true
        reply.success(null)
    }

    private fun stopScan() {
        if (!scanning) return
        scanning = false
        val activity = PluginHost.activity ?: return
        runCatching { adapter?.disableReaderMode(activity) }
    }

    private val callback = NfcAdapter.ReaderCallback { tag ->
        last = tag
        PluginHost.emit("nfc", "event", describe(tag))
    }

    private fun describe(tag: Tag): JSONObject {
        val id = tag.id.joinToString("") { String.format(Locale.ROOT, "%02x", it) }
        val techs = JSONArray()
        for (t in tag.techList) techs.put(t.substringAfterLast('.'))
        val records = JSONArray()
        for (r in read(tag)) records.put(r)
        return JSONObject()
            .put("kind", "tag")
            .put("id", id)
            .put("techs", techs)
            .put("records", records)
    }

    private fun read(tag: Tag): List<JSONObject> {
        val ndef = Ndef.get(tag) ?: return emptyList()
        return try {
            ndef.connect()
            val m = ndef.ndefMessage ?: return emptyList()
            m.records.mapNotNull { record(it) }
        } catch (e: Exception) {
            emptyList()
        } finally {
            runCatching { ndef.close() }
        }
    }

    private fun record(r: NdefRecord): JSONObject? {
        val payload = r.payload
        if (r.tnf == NdefRecord.TNF_WELL_KNOWN) {
            if (r.type.contentEquals(NdefRecord.RTD_TEXT) && payload.isNotEmpty()) {
                val lang = (payload[0].toInt() and 0x3F).coerceAtMost(payload.size - 1)
                val text = if (payload.size > 1 + lang) String(payload, 1 + lang, payload.size - 1 - lang, StandardCharsets.UTF_8) else ""
                return JSONObject().put("kind", "text").put("text", text)
            }
            if (r.type.contentEquals(NdefRecord.RTD_URI) && payload.isNotEmpty()) {
                val prefix = uriPrefixes.getOrNull(payload[0].toInt() and 0xFF) ?: ""
                val rest = String(payload, 1, payload.size - 1, StandardCharsets.UTF_8)
                return JSONObject().put("kind", "uri").put("uri", prefix + rest)
            }
        }
        val mime = r.toMimeType()
        if (mime != null) {
            return JSONObject().put("kind", "mime").put("mime", mime).put("data", base64(payload))
        }
        return JSONObject().put("kind", "raw").put("data", base64(payload))
    }

    private fun writeText(text: String, reply: PluginReply) {
        val tag = last
        if (tag == null) {
            reply.failure(PluginFailure("NoTag"))
            return
        }
        Thread {
            val record = NdefRecord.createTextRecord(Locale.getDefault().language, text)
            val message = NdefMessage(arrayOf(record))
            try {
                val ndef = Ndef.get(tag)
                if (ndef != null && ndef.isWritable) {
                    ndef.connect()
                    try {
                        ndef.writeNdefMessage(message)
                    } finally {
                        ndef.close()
                    }
                } else {
                    val format = NdefFormatable.get(tag) ?: return@Thread reply.failure(PluginFailure("NotWritable", "the tag cannot be written"))
                    format.connect()
                    try {
                        format.format(message)
                    } finally {
                        format.close()
                    }
                }
                reply.success(null)
            } catch (e: Exception) {
                reply.failure(PluginFailure("Failed", e.toString()))
            }
        }.start()
    }

    private fun transceive(tech: String, data: String, reply: PluginReply) {
        val tag = last
        if (tag == null) {
            reply.failure(PluginFailure("NoTag"))
            return
        }
        val bytes = android.util.Base64.decode(data, android.util.Base64.NO_WRAP)
        Thread {
            try {
                val answer = when (tech) {
                    "IsoDep" -> IsoDep.get(tag)?.let { exchange(bytes, it::connect, it::transceive, it::close) }
                    "NfcA" -> NfcA.get(tag)?.let { exchange(bytes, it::connect, it::transceive, it::close) }
                    "NfcB" -> NfcB.get(tag)?.let { exchange(bytes, it::connect, it::transceive, it::close) }
                    "NfcF" -> NfcF.get(tag)?.let { exchange(bytes, it::connect, it::transceive, it::close) }
                    "NfcV" -> NfcV.get(tag)?.let { exchange(bytes, it::connect, it::transceive, it::close) }
                    else -> return@Thread reply.failure(PluginFailure("NoTechnology", tech))
                }
                if (answer == null) {
                    reply.failure(PluginFailure("NoTechnology", tech))
                    return@Thread
                }
                reply.success(JSONObject().put("data", android.util.Base64.encodeToString(answer, android.util.Base64.NO_WRAP)))
            } catch (e: Exception) {
                reply.failure(PluginFailure("Failed", e.toString()))
            }
        }.start()
    }

    /** One exchange on a technology, connected for it and closed after. */
    private fun exchange(data: ByteArray, connect: () -> Unit, transceive: (ByteArray) -> ByteArray, close: () -> Unit): ByteArray =
        try {
            connect()
            transceive(data)
        } finally {
            runCatching { close() }
        }

    private fun base64(b: ByteArray) = android.util.Base64.encodeToString(b, android.util.Base64.NO_WRAP)

    private val uriPrefixes = listOf(
        "", "http://www.", "https://www.", "http://", "https://", "tel:", "mailto:",
        "ftp://anonymous:anonymous@", "ftp://ftp.", "ftps://", "sftp://", "smb://",
        "nfs://", "ftp://", "dav://", "news:", "telnet://", "imap:", "rtsp://",
        "urn:", "pop:", "sip:", "sips:", "tftp:", "btspp://", "btl2cap://",
        "btgoep://", "tcpobex://", "irdaobex://", "file://", "urn:epc:id:",
        "urn:epc:tag:", "urn:epc:pat:", "urn:epc:raw:", "urn:epc:", "urn:nfc:",
    )
}
