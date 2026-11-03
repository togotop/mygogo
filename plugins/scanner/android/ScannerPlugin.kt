package dev.mygo

import android.app.Activity
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.provider.MediaStore
import androidx.core.content.FileProvider
import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.MultiFormatReader
import com.google.zxing.NotFoundException
import com.google.zxing.RGBLuminanceSource
import com.google.zxing.common.HybridBinarizer
import org.json.JSONObject
import java.io.File

/**
 * Reads a barcode from a photo the user takes: the system's camera app, so the
 * app needs no permission of its own, and ZXing reads the pixels.
 *
 * The photo goes to the app's cache through the host's FileProvider, as a
 * thumbnail is too small to read a code from.
 */
class ScannerPlugin : MyGoPlugin {
    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "available" -> {
                val intent = Intent(MediaStore.ACTION_IMAGE_CAPTURE)
                val ok = PluginHost.activity != null && intent.resolveActivity(PluginHost.context.packageManager) != null
                reply.success(JSONObject().put("available", ok))
            }
            "scan" -> scan(args, reply)
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }

    private fun scan(args: JSONObject, reply: PluginReply) {
        val ctx = PluginHost.context
        val dir = File(ctx.cacheDir, "scans").apply { mkdirs() }
        val photo = File(dir, System.nanoTime().toString() + ".jpg")
        val uri = try {
            FileProvider.getUriForFile(ctx, ctx.packageName + ".mygo.files", photo)
        } catch (e: IllegalArgumentException) {
            reply.failure(PluginFailure("Failed", e.toString()))
            return
        }
        val intent = Intent(MediaStore.ACTION_IMAGE_CAPTURE)
            .putExtra(MediaStore.EXTRA_OUTPUT, uri)
            .addFlags(Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
        if (intent.resolveActivity(ctx.packageManager) == null) {
            reply.failure(PluginFailure("Unavailable", "no camera app"))
            return
        }
        PluginHost.startForResult(intent) { resultCode, _ ->
            if (resultCode != Activity.RESULT_OK) {
                photo.delete()
                reply.failure(PluginFailure("Canceled"))
                return@startForResult
            }
            val text = read(photo, args)
            photo.delete()
            if (text == null) {
                reply.failure(PluginFailure("NoCode", "the photo holds no code"))
            } else {
                reply.success(JSONObject().put("text", text))
            }
        }
    }

    /** What the barcode in the photo says, or null when it holds none. */
    private fun read(photo: File, args: JSONObject): String? {
        if (!photo.exists() || photo.length() == 0L) return null
        val bitmap = BitmapFactory.decodeFile(photo.absolutePath) ?: return null
        val w = bitmap.width
        val h = bitmap.height
        val pixels = IntArray(w * h)
        bitmap.getPixels(pixels, 0, w, 0, 0, w, h)
        bitmap.recycle()
        val source = RGBLuminanceSource(w, h, pixels)
        val image = BinaryBitmap(HybridBinarizer(source))
        val hints = HashMap<DecodeHintType, Any>()
        hints[DecodeHintType.POSSIBLE_FORMATS] = formats(args)
        hints[DecodeHintType.TRY_HARDER] = true
        val reader = MultiFormatReader()
        return try {
            reader.decode(image, hints).text
        } catch (e: NotFoundException) {
            null
        } catch (e: Exception) {
            null
        } finally {
            reader.reset()
        }
    }

    /** The formats asked for, or all of them. */
    private fun formats(args: JSONObject): List<BarcodeFormat> {
        val names = args.optJSONArray("Formats") ?: return BarcodeFormat.values().toList()
        val out = ArrayList<BarcodeFormat>()
        for (i in 0 until names.length()) {
            val f = when (names.optString(i).lowercase()) {
                "qr" -> BarcodeFormat.QR_CODE
                "ean13" -> BarcodeFormat.EAN_13
                "ean8" -> BarcodeFormat.EAN_8
                "upca" -> BarcodeFormat.UPC_A
                "upce" -> BarcodeFormat.UPC_E
                "code128" -> BarcodeFormat.CODE_128
                "code39" -> BarcodeFormat.CODE_39
                "codabar" -> BarcodeFormat.CODABAR
                "itf" -> BarcodeFormat.ITF
                "datamatrix" -> BarcodeFormat.DATA_MATRIX
                "pdf417" -> BarcodeFormat.PDF_417
                "aztec" -> BarcodeFormat.AZTEC
                else -> null
            }
            if (f != null && !out.contains(f)) out.add(f)
        }
        return if (out.isEmpty()) BarcodeFormat.values().toList() else out
    }
}
