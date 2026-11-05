package local.camerahub.tv

import android.content.ContentProvider
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInfo
import android.database.Cursor
import android.database.MatrixCursor
import android.net.Uri
import android.os.Build
import android.os.ParcelFileDescriptor
import android.provider.OpenableColumns
import android.provider.Settings
import org.json.JSONObject
import java.io.File
import java.io.FileNotFoundException
import java.io.InputStream
import java.io.OutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest

/** What the hub offers at GET /api/tv/update. */
data class TvUpdateInfo(val versionCode: Long, val versionName: String, val sha256: String, val size: Long)

/** Pure update rules, kept apart from Android so they are unit-tested. */
object TvUpdateCheck {
    const val INTERVAL_MS = 6 * 60 * 60 * 1000L
    const val MAX_BYTES = 200L shl 20
    private val name = Regex("^[0-9A-Za-z.+-]{1,32}$")
    private val hex = Regex("^[0-9a-f]{64}$")

    fun parse(raw: String): TvUpdateInfo? = runCatching {
        val o = JSONObject(raw)
        TvUpdateInfo(o.getLong("versionCode"), o.getString("versionName"), o.getString("sha256").lowercase(), o.getLong("size"))
    }.getOrNull()?.takeIf { it.versionCode > 0 && name.matches(it.versionName) && hex.matches(it.sha256) && it.size in 1..MAX_BYTES }

    /** Only a strictly newer build is offered; Android refuses a downgrade anyway. */
    fun offer(info: TvUpdateInfo?, installedCode: Long): TvUpdateInfo? = info?.takeIf { it.versionCode > installedCode }

    fun due(lastCheckAt: Long?, now: Long): Boolean = lastCheckAt == null || now - lastCheckAt >= INTERVAL_MS

    /** Copies at most [limit] bytes and returns the byte count and lowercase sha256. */
    fun copyHashed(input: InputStream, output: OutputStream, limit: Long): Pair<Long, String> {
        val digest = MessageDigest.getInstance("SHA-256")
        val buffer = ByteArray(64 * 1024)
        var total = 0L
        while (true) {
            val n = input.read(buffer)
            if (n < 0) break
            total += n
            if (total > limit) throw IllegalStateException("ไฟล์อัปเดตใหญ่กว่าที่เครื่องกลางแจ้ง")
            digest.update(buffer, 0, n); output.write(buffer, 0, n)
        }
        return total to digest.digest().joinToString("") { "%02x".format(it) }
    }

    fun fileName(versionCode: Long) = "camerahub-tv-$versionCode.apk"
    fun validFileName(value: String) = Regex("^camerahub-tv-[0-9]{1,12}\\.apk$").matches(value)
}

/** Downloads the hub's APK into app cache, checks it, and hands it to the system installer. */
class TvUpdater(private val context: Context) {
    private val folder get() = File(context.cacheDir, TvUpdateProvider.FOLDER)
    // OutputStream.nullOutputStream() needs Android 13.
    private val discard = object : OutputStream() {
        override fun write(b: Int) = Unit
        override fun write(b: ByteArray, off: Int, len: Int) = Unit
    }

    @Suppress("DEPRECATION")
    fun installedCode(): Long = context.packageManager.getPackageInfo(context.packageName, 0).let(::codeOf)

    /** Blocking; call off the UI thread. Reuses an already verified file. */
    fun download(server: String, token: String, info: TvUpdateInfo): File {
        folder.mkdirs()
        val target = File(folder, TvUpdateCheck.fileName(info.versionCode))
        if (target.isFile && target.length() == info.size && target.inputStream().use { TvUpdateCheck.copyHashed(it, discard, info.size) }.second == info.sha256) {
            return target
        }
        val part = File(folder, "download.part"); part.delete()
        val connection = URL("$server/api/tv/update.apk").openConnection() as HttpURLConnection
        connection.connectTimeout = 15_000; connection.readTimeout = 30_000; connection.instanceFollowRedirects = false
        connection.setRequestProperty("Authorization", "Bearer $token")
        try {
            if (connection.responseCode != 200) throw IllegalStateException("ดาวน์โหลดไม่สำเร็จ (${connection.responseCode})")
            val (size, sha) = connection.inputStream.use { input -> part.outputStream().use { TvUpdateCheck.copyHashed(input, it, info.size) } }
            if (size != info.size || sha != info.sha256) throw IllegalStateException("ไฟล์ที่ได้ไม่ตรงกับเครื่องกลาง · ไม่ติดตั้ง")
            checkArchive(part, info)
            folder.listFiles()?.filter { it != part }?.forEach { it.delete() }
            if (!part.renameTo(target)) throw IllegalStateException("บันทึกไฟล์อัปเดตไม่ได้")
            return target
        } catch (e: Exception) {
            part.delete(); throw e
        } finally { connection.disconnect() }
    }

    /** Same app and the promised version, before the system installer is even asked. */
    @Suppress("DEPRECATION")
    private fun checkArchive(file: File, info: TvUpdateInfo) {
        val archive = context.packageManager.getPackageArchiveInfo(file.path, 0)
            ?: throw IllegalStateException("ไฟล์อัปเดตเสีย · ไม่ติดตั้ง")
        if (archive.packageName != context.packageName || codeOf(archive) != info.versionCode) throw IllegalStateException("ไฟล์อัปเดตไม่ใช่ CameraHub TV รุ่นที่แจ้ง · ไม่ติดตั้ง")
    }

    /** Android 8+: the owner allows "install unknown apps" for CameraHub once. */
    fun canInstall(): Boolean = context.packageManager.canRequestPackageInstalls()
    fun permissionIntent(): Intent = Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}"))

    @Suppress("DEPRECATION")
    fun installIntent(file: File): Intent = Intent(Intent.ACTION_INSTALL_PACKAGE)
        .setDataAndType(TvUpdateProvider.uri(context, file.name), TvUpdateProvider.APK_TYPE)
        .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)

    @Suppress("DEPRECATION")
    private fun codeOf(info: PackageInfo): Long = if (Build.VERSION.SDK_INT >= 28) info.longVersionCode else info.versionCode.toLong()
}

/**
 * Read-only access to the downloaded APK for the system installer, granted per
 * intent. Serves only camerahub-tv-<code>.apk from the app's update cache.
 */
class TvUpdateProvider : ContentProvider() {
    companion object {
        const val FOLDER = "update"
        const val APK_TYPE = "application/vnd.android.package-archive"
        fun authority(context: Context) = "${context.packageName}.update"
        fun uri(context: Context, name: String): Uri = Uri.Builder().scheme("content").authority(authority(context)).appendPath(name).build()
    }
    private fun file(uri: Uri): File {
        val name = uri.lastPathSegment ?: ""
        val ctx = context ?: throw FileNotFoundException()
        if (uri.pathSegments.size != 1 || !TvUpdateCheck.validFileName(name)) throw FileNotFoundException()
        return File(File(ctx.cacheDir, FOLDER), name).takeIf { it.isFile } ?: throw FileNotFoundException()
    }
    override fun onCreate() = true
    override fun getType(uri: Uri): String = APK_TYPE
    override fun openFile(uri: Uri, mode: String): ParcelFileDescriptor {
        if (mode != "r") throw SecurityException("read only")
        return ParcelFileDescriptor.open(file(uri), ParcelFileDescriptor.MODE_READ_ONLY)
    }
    override fun query(uri: Uri, projection: Array<out String>?, selection: String?, selectionArgs: Array<out String>?, sortOrder: String?): Cursor {
        val f = file(uri)
        val columns = projection?.filter { it == OpenableColumns.DISPLAY_NAME || it == OpenableColumns.SIZE }?.toTypedArray()
            ?: arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE)
        return MatrixCursor(columns, 1).apply { addRow(columns.map { if (it == OpenableColumns.SIZE) f.length() else f.name }) }
    }
    override fun insert(uri: Uri, values: ContentValues?): Uri? = null
    override fun update(uri: Uri, values: ContentValues?, selection: String?, selectionArgs: Array<out String>?) = 0
    override fun delete(uri: Uri, selection: String?, selectionArgs: Array<out String>?) = 0
}
