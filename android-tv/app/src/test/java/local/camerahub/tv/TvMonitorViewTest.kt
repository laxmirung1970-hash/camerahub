package local.camerahub.tv

import android.app.Activity
import android.graphics.*
import android.view.KeyEvent
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.ImageView
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
class TvMonitorViewTest {
    private lateinit var activity: ActivityController<Activity>
    private lateinit var monitor: TvMonitorView
    private lateinit var state: TvViewingState
    private var action = ""
    private val cameras get() = (0 until 17).map { i -> TvCamera(
        "demo-$i", listOf("ประตูทางเข้า", "สวนหลังบ้าน", "ลานจอดรถ", "ห้องรับแขก").getOrNull(i) ?: "กล้องตัวอย่าง ${i + 1}",
        listOf("Tapo C520WS", "xiaomi.camera.c01a01", "chuangmi.camera.072ae2", "xiaomi.camera.c302n")[i % 4],
        "บ้านตัวอย่าง", if (i == 3) "offline" else "pending", ptz = i != 3, audio = i != 3,
    ) }
    @Before fun start() {
        activity = Robolectric.buildActivity(Activity::class.java).setup().visible()
        state = TvViewingState().apply { replace(this@TvMonitorViewTest.cameras) }
        monitor = TvMonitorView(activity.get(), object : TvMonitorView.Events {
            override fun focusCamera(id: String) = state.focus(id)
            override fun openCamera(id: String) { state.open(id); render(); monitor.focusCamera(id) }
            override fun action(name: String) { action = name }
        })
        activity.get().setContentView(monitor); render(); monitor.focusCamera(state.activeId)
    }
    @After fun finish() { activity.pause().stop().destroy() }
    private fun render() { monitor.render(state, true, TvTourMode.GRID, false, false); layout() }
    private fun layout() {
        monitor.measure(View.MeasureSpec.makeMeasureSpec(960, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(540, View.MeasureSpec.EXACTLY))
        monitor.layout(0, 0, 960, 540)
    }
    private fun key(code: Int) {
        activity.get().dispatchKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, code))
        activity.get().dispatchKeyEvent(KeyEvent(KeyEvent.ACTION_UP, code))
    }
    private fun allViews(root: View = monitor): List<View> = listOf(root) + if (root is ViewGroup) (0 until root.childCount).flatMap { allViews(root.getChildAt(it)) } else emptyList()
    private fun button(label: String) = allViews().filterIsInstance<Button>().first { it.text.toString() == label }
    private fun sampleImages() {
        monitor.tiles.values.forEachIndexed { index, tile ->
            val bitmap = Bitmap.createBitmap(800, 450, Bitmap.Config.ARGB_8888)
            val canvas = Canvas(bitmap); val paint = Paint(Paint.ANTI_ALIAS_FLAG)
            paint.shader = LinearGradient(0f, 0f, 0f, 450f, intArrayOf(0xFF506B7E.toInt(), 0xFFAAAE9A.toInt()), null, Shader.TileMode.CLAMP)
            canvas.drawRect(0f, 0f, 800f, 450f, paint); paint.shader = null
            paint.color = 0xFF555C50.toInt(); canvas.drawRect(0f, 240f, 800f, 450f, paint)
            paint.color = 0xFFB29C83.toInt(); canvas.drawRect(100f + index * 25, 110f, 660f, 270f, paint)
            paint.color = 0xFF333B45.toInt(); canvas.drawRect(160f + index * 25, 150f, 290f + index * 25, 270f, paint)
            paint.color = 0xFFDDD0B8.toInt(); canvas.drawRect(350f, 160f, 440f, 210f, paint); canvas.drawRect(480f, 160f, 570f, 210f, paint)
            paint.color = 0xFF8B604C.toInt(); canvas.drawPath(Path().apply { moveTo(60f, 110f); lineTo(370f, 35f); lineTo(700f, 110f); close() }, paint)
            paint.color = 0xFFA7A69F.toInt(); canvas.drawPath(Path().apply { moveTo(280f, 450f); lineTo(340f, 270f); lineTo(430f, 270f); lineTo(570f, 450f); close() }, paint)
            paint.color = 0xFF2F493E.toInt(); canvas.drawCircle(70f, 220f, 50f, paint); canvas.drawCircle(730f, 200f, 68f, paint)
            tile.videoHost.addView(ImageView(activity.get()).apply { setImageBitmap(bitmap); scaleType = ImageView.ScaleType.CENTER_CROP }, ViewGroup.LayoutParams(-1, -1))
            if (state.showing[index].status == "offline") tile.showProblem("กล้องออฟไลน์", "ตรวจไฟและเครือข่าย\nลองใหม่ใน 30 วินาที") else tile.showLive(true)
        }
    }
    private fun screenshot(name: String) {
        layout()
        val image = Bitmap.createBitmap(1920, 1080, Bitmap.Config.ARGB_8888)
        val canvas = Canvas(image); canvas.scale(2f, 2f); monitor.draw(canvas)
        val out = File("build/reports/tv-preview/$name.png"); out.parentFile?.mkdirs()
        out.outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
    @Test fun arrowsReachFourCamerasFooterAndReturnToLastCamera() {
        key(KeyEvent.KEYCODE_DPAD_RIGHT); assertEquals("demo-1", state.activeId)
        key(KeyEvent.KEYCODE_DPAD_DOWN); assertEquals("demo-3", state.activeId)
        key(KeyEvent.KEYCODE_DPAD_LEFT); assertEquals("demo-2", state.activeId)
        key(KeyEvent.KEYCODE_DPAD_DOWN); assertTrue(monitor.controlsFocused)
        key(KeyEvent.KEYCODE_DPAD_RIGHT); assertTrue(button("ชุดถัด ›").isFocused)
        key(KeyEvent.KEYCODE_DPAD_UP); assertTrue(monitor.tiles.getValue("demo-2").isFocused)
    }
    @Test fun okOpensSingleAndStateReturnsToSameCameraAndPage() {
        state.step(1); render(); monitor.focusCamera("demo-6")
        key(KeyEvent.KEYCODE_DPAD_CENTER)
        assertTrue(state.single); assertEquals(listOf("demo-6"), monitor.tiles.keys.toList())
        key(KeyEvent.KEYCODE_DPAD_DOWN); assertTrue(button("4 ช่อง").isFocused)
        state.grid(); render(); monitor.focusCamera(state.activeId)
        assertEquals(1, state.page); assertTrue(monitor.tiles.getValue("demo-6").isFocused)
    }
    @Test fun pageAndMetadataRefreshKeepFocusedButtonAndExistingPlayersViews() {
        button("ชุดถัด ›").requestFocus(); val oldButton = button("ชุดถัด ›")
        state.step(1); render()
        assertSame(oldButton, button("ชุดถัด ›")); assertTrue(oldButton.isFocused)
        val tile = monitor.tiles.getValue("demo-4")
        state.replace(cameras.map { it.copy(name = "ชื่อใหม่ ${it.id}", status = "online") }); render()
        assertSame(tile, monitor.tiles["demo-4"]); assertTrue(oldButton.isFocused)
    }
    @Test fun pickerContainsFullNamesModelsTrapsFocusAndReturnsToItsButton() {
        button("เลือกกล้อง").requestFocus()
        monitor.openPicker(cameras, "demo-16") { action = it }; layout()
        assertTrue(monitor.hasOverlay)
        assertEquals("กล้องตัวอย่าง 17, Tapo C520WS", monitor.findFocus()?.contentDescription)
        key(KeyEvent.KEYCODE_DPAD_DOWN)
        assertEquals("กล้องตัวอย่าง 17, Tapo C520WS", monitor.findFocus()?.contentDescription)
        render(); assertTrue(monitor.hasOverlay); assertFalse(monitor.controlsFocused)
        assertTrue(allViews().filterIsInstance<TextView>().any { it.text.toString().contains("Xiaomi C300 Dual") })
        monitor.dismissOverlay(); assertTrue(button("เลือกกล้อง").isFocused)
    }
    @Test fun lastPageStillReachesFooterAndDisabledSingleActionsAreSkipped() {
        repeat(4) { state.step(1) }; render(); monitor.focusCamera("demo-16")
        key(KeyEvent.KEYCODE_DPAD_DOWN); assertTrue(monitor.controlsFocused)
        key(KeyEvent.KEYCODE_DPAD_UP); assertTrue(monitor.tiles.getValue("demo-16").isFocused)
        state.open("demo-3"); render(); monitor.focusControls()
        key(KeyEvent.KEYCODE_DPAD_RIGHT); assertTrue(button("สลับ: 4 ช่อง").isFocused)
        key(KeyEvent.KEYCODE_DPAD_RIGHT); assertTrue(button("เลือกกล้อง").isFocused)
        key(KeyEvent.KEYCODE_DPAD_RIGHT); assertTrue(button("คุณภาพ: ปกติ").isFocused)
    }
    @Test fun emptyAndUnreachableCatalogShowsExplanationAndRetainsRemoteControls() {
        state.replace(emptyList()); render(); monitor.showCatalogNotice("ยังติดต่อเครื่องกลางไม่ได้\nตรวจ Tailscale และเครื่องกลาง")
        monitor.focusCamera("")
        assertTrue(button("เลือกกล้อง").isFocused)
        assertTrue(allViews().filterIsInstance<TextView>().any { it.text.toString().contains("ยังติดต่อเครื่องกลางไม่ได้") })
        monitor.showCatalogNotice(null)
        assertTrue(allViews().filterIsInstance<TextView>().any { it.text.toString().contains("ยังไม่มีกล้อง") })
    }
    @Test fun renderNativeGridPickerSinglePtzAndQuietScreens() {
        sampleImages(); monitor.updateClock("19:35"); monitor.updateTour(28, false)
        screenshot("01-four-cameras")
        assertEquals(4, monitor.tiles.size)
        val tile = monitor.tiles.values.first(); val oldHeight = tile.height
        monitor.showChrome(false); layout(); assertTrue(tile.height > oldHeight)
        screenshot("02-quiet-four-cameras"); monitor.showChrome(true)
        button("เลือกกล้อง").requestFocus(); monitor.openPicker(cameras, "demo-1") { }; layout()
        screenshot("03-camera-picker"); monitor.dismissOverlay()
        state.open("demo-0"); render(); monitor.focusCamera("demo-0"); sampleImages()
        screenshot("04-single-camera")
        monitor.setPtz(true, "right"); screenshot("05-pan-tilt-mode")
        assertTrue(allViews().filterIsInstance<TextView>().any { it.text.toString().contains("ปล่อยปุ่มเพื่อหยุด") })
        monitor.setPtz(false)
        allViews().filterIsInstance<Button>().filter { it.visibility == View.VISIBLE }.forEach {
            assertEquals("Button must fit one line: ${it.text}", 1, it.lineCount)
            assertTrue(it.width >= 100); assertTrue(it.height >= 48)
        }
    }
}
