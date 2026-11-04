package local.camerahub.tv

import android.graphics.Bitmap
import android.graphics.Canvas
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.android.controller.ActivityController
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.annotation.LooperMode
import java.io.File

@RunWith(org.robolectric.RobolectricTestRunner::class)
@Config(sdk = [28], qualifiers = "w960dp-h540dp-land-television-mdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@LooperMode(LooperMode.Mode.PAUSED)
class TvSetupViewTest {
    private lateinit var activity: ActivityController<MainActivity>
    private lateinit var root: View
    @Before fun start() {
        activity = Robolectric.buildActivity(MainActivity::class.java).setup().visible()
        root = activity.get().findViewById<ViewGroup>(android.R.id.content).getChildAt(0)
        root.measure(View.MeasureSpec.makeMeasureSpec(960, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(540, View.MeasureSpec.EXACTLY))
        root.layout(0, 0, 960, 540)
    }
    @After fun finish() { activity.pause().stop().destroy() }
    private fun descendants(view: View = root): List<View> = listOf(view) + if (view is ViewGroup) (0 until view.childCount).flatMap { descendants(view.getChildAt(it)) } else emptyList()
    @Test fun firstSetupNeedsNoCameraPasswordAndFitsTvSafeArea() {
        val fields = descendants().filterIsInstance<EditText>()
        assertEquals(1, fields.size)
        assertTrue(fields.single().hint.toString().startsWith("https://"))
        val connect = descendants().filterIsInstance<Button>().single()
        val bounds = android.graphics.Rect(); connect.getDrawingRect(bounds)
        (root as ViewGroup).offsetDescendantRectToMyCoords(connect, bounds)
        assertTrue(bounds.top >= 27); assertTrue(bounds.bottom <= 513)
        assertTrue(connect.width >= 200); assertTrue(connect.height >= 48)
        val image = Bitmap.createBitmap(1920, 1080, Bitmap.Config.ARGB_8888)
        val canvas = Canvas(image); canvas.scale(2f, 2f); root.draw(canvas)
        val file = File("build/reports/tv-preview/06-first-setup.png"); file.parentFile?.mkdirs()
        file.outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
    @Test fun wrongAddressShowsPersistentExplanationWithoutStartingPairing() {
        descendants().filterIsInstance<EditText>().single().setText("http://127.0.0.1:17801")
        val connect = descendants().filterIsInstance<Button>().single()
        connect.performClick()
        assertTrue(connect.isEnabled)
        assertTrue(descendants().filterIsInstance<TextView>().any { it.text.toString().contains("ใช้ HTTPS จาก Tailscale Serve") })
        assertEquals(1, descendants().filterIsInstance<EditText>().size)
    }
}
