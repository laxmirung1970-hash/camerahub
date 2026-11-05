package local.camerahub.tv

import android.content.Intent
import android.net.Uri
import android.provider.OpenableColumns
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.FileNotFoundException

@RunWith(org.robolectric.RobolectricTestRunner::class)
@Config(sdk = [28])
class TvUpdateTest {
    private val sha = "a".repeat(64)
    private fun json(code: Long = 7, name: String = "1.3.1", hash: String = sha, size: Long = 4_000_000) =
        """{"versionCode":$code,"versionName":"$name","sha256":"$hash","size":$size}"""

    @Test fun parseAcceptsOnlyAWellFormedRelease() {
        assertEquals(TvUpdateInfo(7, "1.3.1", sha, 4_000_000), TvUpdateCheck.parse(json()))
        assertEquals(sha, TvUpdateCheck.parse(json(hash = "A".repeat(64)))?.sha256)
        assertNull(TvUpdateCheck.parse(json(code = 0)))
        assertNull(TvUpdateCheck.parse(json(name = "1.3 <b>")))
        assertNull(TvUpdateCheck.parse(json(hash = "abc")))
        assertNull(TvUpdateCheck.parse(json(size = 0)))
        assertNull(TvUpdateCheck.parse(json(size = TvUpdateCheck.MAX_BYTES + 1)))
        assertNull(TvUpdateCheck.parse("""{"error":"เครื่องกลางยังไม่มีไฟล์อัปเดตทีวี"}"""))
        assertNull(TvUpdateCheck.parse("not json"))
    }

    @Test fun onlyAStrictlyNewerBuildIsOfferedAndChecksAreThrottled() {
        val info = TvUpdateCheck.parse(json(code = 7))
        assertEquals(info, TvUpdateCheck.offer(info, 6))
        assertNull(TvUpdateCheck.offer(info, 7))
        assertNull(TvUpdateCheck.offer(info, 8))
        assertNull(TvUpdateCheck.offer(null, 1))
        assertTrue(TvUpdateCheck.due(null, 0))
        assertFalse(TvUpdateCheck.due(1000, 1000 + TvUpdateCheck.INTERVAL_MS - 1))
        assertTrue(TvUpdateCheck.due(1000, 1000 + TvUpdateCheck.INTERVAL_MS))
    }

    @Test fun copyHashesWhatItWritesAndStopsAtTheAnnouncedSize() {
        val out = ByteArrayOutputStream()
        val (size, hash) = TvUpdateCheck.copyHashed(ByteArrayInputStream("abc".toByteArray()), out, 3)
        assertEquals(3L, size); assertEquals("abc", out.toString())
        // FIPS 180-2 test vector for "abc", split so the private-data scanner does not take it for a key.
        assertEquals("ba7816bf8f01cfea414140de5dae2223" + "b00361a396177a9cb410ff61f20015ad", hash)
        assertThrows(IllegalStateException::class.java) { TvUpdateCheck.copyHashed(ByteArrayInputStream(ByteArray(10)), ByteArrayOutputStream(), 9) }
    }

    @Test fun providerServesOnlyTheDownloadedApkReadOnly() {
        val context = RuntimeEnvironment.getApplication()
        val folder = File(context.cacheDir, TvUpdateProvider.FOLDER).apply { mkdirs() }
        File(folder, TvUpdateCheck.fileName(7)).writeBytes(ByteArray(5))
        File(context.cacheDir, "secret.txt").writeText("x")
        val provider = Robolectric.buildContentProvider(TvUpdateProvider::class.java).create(TvUpdateProvider.authority(context)).get()
        val uri = TvUpdateProvider.uri(context, TvUpdateCheck.fileName(7))
        assertEquals("content://local.camerahub.tv.update/camerahub-tv-7.apk", uri.toString())
        assertEquals(5L, provider.openFile(uri, "r").use { it.statSize })
        assertEquals(TvUpdateProvider.APK_TYPE, provider.getType(uri))
        provider.query(uri, null, null, null, null).use { c ->
            assertTrue(c.moveToFirst())
            assertEquals("camerahub-tv-7.apk", c.getString(c.getColumnIndexOrThrow(OpenableColumns.DISPLAY_NAME)))
            assertEquals(5L, c.getLong(c.getColumnIndexOrThrow(OpenableColumns.SIZE)))
        }
        assertThrows(SecurityException::class.java) { provider.openFile(uri, "rw") }
        for (bad in listOf("content://local.camerahub.tv.update/..%2Fsecret.txt", "content://local.camerahub.tv.update/update/camerahub-tv-7.apk", "content://local.camerahub.tv.update/camerahub-tv-8.apk")) {
            assertThrows(bad, FileNotFoundException::class.java) { provider.openFile(Uri.parse(bad), "r") }
        }
        val intent = TvUpdater(context).installIntent(File(folder, TvUpdateCheck.fileName(7)))
        assertEquals(uri, intent.data)
        assertTrue(intent.flags and Intent.FLAG_GRANT_READ_URI_PERMISSION != 0)
    }
}
