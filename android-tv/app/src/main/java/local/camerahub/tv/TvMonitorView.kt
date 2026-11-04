package local.camerahub.tv

import android.content.Context
import android.graphics.Color
import android.text.TextUtils
import android.view.Gravity
import android.view.KeyEvent
import android.view.View
import android.view.ViewGroup
import android.widget.*

class TvMonitorView(context: Context, private val events: Events) : FrameLayout(context) {
    interface Events {
        fun focusCamera(id: String)
        fun openCamera(id: String)
        fun action(name: String)
    }
    private val s = TvStyle(context)
    private val content = LinearLayout(context)
    private val header = LinearLayout(context)
    private val heading = s.text("CAMERAHUB", 25f, s.paper, true)
    private val subtitle = s.text("กำลังอ่านรายการกล้อง", 14f, s.muted)
    private val clock = s.text("", 15f, s.muted)
    private val demo = s.text("โหมดสาธิต", 13f, s.accent, true)
    private val stage = LinearLayout(context)
    private val emptyState = s.text("กำลังอ่านรายชื่อกล้อง…", 22f, s.muted).apply { gravity = Gravity.CENTER }
    private val bottom = LinearLayout(context)
    private val controls = LinearLayout(context)
    private val hint = s.text("", 13f, s.muted)
    private val ptzHelp = s.text("", 18f, s.accent, true)
    private val buttons = linkedMapOf<String, Button>()
    val tiles = linkedMapOf<String, TvCameraTile>()
    private var ids = emptyList<String>()
    private var wasSingle = false
    private var current: TvViewingState? = null
    private var overlay: View? = null
    private var overlayFocus: View? = null
    private var chromeVisible = true
    private var rebuilding = false
    var ptzMode = false
        private set
    val hasOverlay: Boolean get() = overlay != null
    val controlsFocused: Boolean get() = controls.hasFocus()
    val cameraFocused: Boolean get() = stage.hasFocus()

    init {
        setBackgroundColor(s.ink)
        content.orientation = LinearLayout.VERTICAL
        content.setPadding(s.dp(48), s.dp(27), s.dp(48), s.dp(27))
        addView(content, LayoutParams(-1, -1))
        header.gravity = Gravity.CENTER_VERTICAL
        val identity = LinearLayout(context).apply { orientation = LinearLayout.VERTICAL }
        subtitle.maxLines = 1; subtitle.ellipsize = TextUtils.TruncateAt.END
        identity.addView(heading); identity.addView(subtitle, LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(3) })
        header.addView(identity, LinearLayout.LayoutParams(0, -2, 1f))
        demo.setPadding(s.dp(12), s.dp(5), s.dp(12), s.dp(5)); demo.background = s.shape(s.accentDark, s.accentDark)
        header.addView(demo, LinearLayout.LayoutParams(-2, -2).apply { marginEnd = s.dp(20) })
        header.addView(clock)
        content.addView(header, LinearLayout.LayoutParams(-1, s.dp(62)))
        stage.orientation = LinearLayout.VERTICAL; stage.clipChildren = false
        content.addView(stage, LinearLayout.LayoutParams(-1, 0, 1f))
        stage.addView(emptyState, LinearLayout.LayoutParams(-1, -1))
        bottom.orientation = LinearLayout.VERTICAL; bottom.setPadding(0, s.dp(12), 0, 0)
        controls.gravity = Gravity.CENTER_VERTICAL
        bottom.addView(controls, LinearLayout.LayoutParams(-1, s.dp(48)))
        ptzHelp.gravity = Gravity.CENTER; ptzHelp.visibility = View.GONE
        bottom.addView(ptzHelp, LinearLayout.LayoutParams(-1, s.dp(48)))
        hint.gravity = Gravity.CENTER
        bottom.addView(hint, LinearLayout.LayoutParams(-1, s.dp(27)))
        content.addView(bottom, LinearLayout.LayoutParams(-1, -2))
    }

    fun render(state: TvViewingState, isDemo: Boolean, auto: Boolean, hd: Boolean, audible: Boolean) {
        current = state
        demo.visibility = if (isDemo) View.VISIBLE else View.GONE
        heading.text = if (state.single) state.active?.name ?: "CameraHub" else "CameraHub."
        heading.maxLines = 1; heading.ellipsize = TextUtils.TruncateAt.END
        val nextIds = state.showing.map { it.id }
        val modeChanged = wasSingle != state.single
        val changed = ids != nextIds || modeChanged
        val restoreCamera = state.activeId
        if (changed) {
            rebuilding = true
            ids = nextIds; wasSingle = state.single
            tiles.values.forEach { (it.parent as? ViewGroup)?.removeView(it) }
            tiles.keys.retainAll(ids.toSet()); stage.removeAllViews()
            if (ids.isEmpty()) {
                stage.addView(emptyState, LinearLayout.LayoutParams(-1, -1))
            } else {
                val rows = if (state.single) 1 else 2
                val columns = if (state.single) 1 else 2
                for (r in 0 until rows) {
                    val row = LinearLayout(context).apply { clipChildren = false }
                    stage.addView(row, LinearLayout.LayoutParams(-1, 0, 1f).apply { if (r == 0 && rows == 2) bottomMargin = s.dp(12) })
                    for (col in 0 until columns) {
                        val index = r * columns + col
                        val camera = state.showing.getOrNull(index)
                        val tile: View = if (camera == null) {
                            FrameLayout(context).apply {
                                background = s.shape(0xFF17191E.toInt(), 0xFF292C34.toInt())
                                addView(s.text("—", 24f, 0xFF4A4E58.toInt()).apply { gravity = Gravity.CENTER }, LayoutParams(-1, -1))
                            }
                        } else tiles.getOrPut(camera.id) { makeTile(camera.id) }
                        row.addView(tile, LinearLayout.LayoutParams(0, -1, 1f).apply { if (col == 0 && columns == 2) marginEnd = s.dp(12) })
                    }
                }
            }
            if (modeChanged || buttons.isEmpty()) buildControls(state.single)
            // Adding children can otherwise focus the first new tile and overwrite the saved camera.
            if (!hasOverlay && !controls.hasFocus()) focusCamera(restoreCamera)
            rebuilding = false
        } else if (buttons.isEmpty()) buildControls(state.single)
        for ((id, tile) in tiles) state.cameras.find { it.id == id }?.let {
            tile.update(it, state.cameras.indexOf(it), state.single)
        }
        buttons["auto"]?.text = if (auto) "สลับชุด: เปิด" else "สลับชุด: พัก"
        buttons["audio"]?.apply {
            text = if (audible) "เสียง: เปิด" else "เสียง: ปิด"
            isEnabled = state.active?.audio == true; alpha = if (isEnabled) 1f else .4f
        }
        buttons["ptz"]?.apply { isEnabled = state.active?.ptz == true; alpha = if (isEnabled) 1f else .4f }
        buttons["quality"]?.text = if (hd) "คุณภาพ: HD" else "คุณภาพ: ปกติ"
        if (!ptzMode) hint.text = if (state.single) "ลูกศรเลือกปุ่ม  ·  OK ใช้งาน  ·  กลับ ดู 4 ช่อง" else "ลูกศรเลือกกล้อง  ·  OK ดูเดี่ยว  ·  กดลงเพื่อเลือกชุดหรือรายชื่อ"
        updateSummary(if (state.single) "${state.active?.modelLabel ?: ""}  ·  กล้อง ${state.cameras.indexOfFirst { it.id == state.activeId } + 1} / ${state.cameras.size}" else "4 ช่อง  ·  ชุด ${state.page + 1} / ${state.pageCount}  ·  ${state.cameras.size} กล้อง")
    }
    fun updateSummary(value: String) { subtitle.text = value }
    fun showCatalogNotice(message: String?) {
        emptyState.text = message ?: "ยังไม่มีกล้อง\nเพิ่มกล้องจากมือถือหรือคอมพิวเตอร์ก่อน"
    }
    fun updateClock(value: String) { clock.text = value }
    fun updateTour(seconds: Int?, paused: Boolean) {
        val state = current ?: return
        if (state.single) return
        val tour = if (seconds == null) "พักการสลับ" else if (paused) "พักขณะใช้เมนู" else "เปลี่ยนใน $seconds วิ"
        subtitle.text = "ชุด ${state.page + 1} / ${state.pageCount}  ·  ${state.cameras.size} กล้อง  ·  $tour"
    }
    private fun makeTile(id: String) = TvCameraTile(context, id).apply {
        setOnFocusChangeListener { _, focused -> focusStyle(focused); if (focused && !rebuilding) events.focusCamera(id) }
        setOnClickListener { if (current?.single == true) focusControls() else events.openCamera(id) }
        setOnKeyListener { _, keyCode, event ->
            val direction = direction(keyCode)
            if (direction == null || event.action != KeyEvent.ACTION_DOWN) false else {
                val state = current
                if (state?.single == true) { if (direction == TvGridFocus.Direction.DOWN) focusControls() }
                else {
                    val index = ids.indexOf(id)
                    val next = TvGridFocus.target(index, ids.size, direction)
                    if (next < 0) focusControls() else tiles[ids[next]]?.requestFocus()
                }
                true
            }
        }
    }
    private fun buildControls(single: Boolean) {
        controls.removeAllViews(); buttons.clear()
        val actions = if (single) listOf("grid" to "4 ช่อง", "picker" to "เลือกกล้อง", "audio" to "เสียง: ปิด", "ptz" to "หมุนกล้อง", "quality" to "คุณภาพ: ปกติ")
        else listOf("previous" to "‹ ชุดก่อน", "next" to "ชุดถัด ›", "auto" to "สลับชุด: เปิด", "picker" to "เลือกกล้อง", "settings" to "ตั้งค่า")
        actions.forEachIndexed { index, (key, label) ->
            val b = s.button(label) { events.action(key) }
            buttons[key] = b
            controls.addView(b, LinearLayout.LayoutParams(0, -1, if (key == "quality" || key == "auto") 1.25f else 1f).apply { if (index > 0) marginStart = s.dp(8) })
            b.setOnKeyListener { _, code, event ->
                val direction = direction(code)
                if (direction == null || event.action != KeyEvent.ACTION_DOWN) false else {
                    when (direction) {
                        TvGridFocus.Direction.UP -> focusCamera(current?.activeId ?: "")
                        TvGridFocus.Direction.DOWN -> Unit
                        else -> {
                            val available = buttons.values.filter { it.isEnabled }
                            val old = available.indexOf(b)
                            val offset = if (direction == TvGridFocus.Direction.LEFT) -1 else 1
                            available.getOrNull((old + offset).coerceIn(0, available.lastIndex))?.requestFocus()
                        }
                    }
                    true
                }
            }
        }
    }
    private fun direction(code: Int) = when (code) {
        KeyEvent.KEYCODE_DPAD_LEFT -> TvGridFocus.Direction.LEFT
        KeyEvent.KEYCODE_DPAD_RIGHT -> TvGridFocus.Direction.RIGHT
        KeyEvent.KEYCODE_DPAD_UP -> TvGridFocus.Direction.UP
        KeyEvent.KEYCODE_DPAD_DOWN -> TvGridFocus.Direction.DOWN
        else -> null
    }
    fun focusCamera(id: String) {
        (tiles[id] ?: tiles.values.firstOrNull())?.requestFocus() ?: buttons["picker"]?.requestFocus()
    }
    fun focusControls() { showChrome(true); buttons.values.firstOrNull { it.isEnabled }?.requestFocus() }
    fun showChrome(show: Boolean) {
        if (ptzMode || hasOverlay) return
        if (chromeVisible == show) return
        chromeVisible = show
        header.visibility = if (show) View.VISIBLE else View.GONE
        bottom.visibility = if (show) View.VISIBLE else View.GONE
    }
    fun setPtz(enabled: Boolean, direction: String = "") {
        if (enabled) showChrome(true)
        ptzMode = enabled
        controls.visibility = if (enabled) View.GONE else View.VISIBLE
        ptzHelp.visibility = if (enabled) View.VISIBLE else View.GONE
        val moving = when (direction) { "left" -> "← ซ้าย"; "right" -> "ขวา →"; "up" -> "↑ ขึ้น"; "down" -> "↓ ลง"; else -> "พร้อมหมุน" }
        ptzHelp.text = "โหมดหมุนกล้อง  ·  $moving  ·  ปล่อยปุ่มเพื่อหยุด"
        hint.text = if (enabled) "กดลูกศรค้างเพื่อหมุน  ·  OK หรือปุ่มกลับ เพื่อออกจากโหมดหมุน" else if (current?.single == true) "ลูกศรเลือกปุ่ม  ·  OK ใช้งาน  ·  กลับ ดู 4 ช่อง" else "ลูกศรเลือกกล้อง  ·  OK ดูเดี่ยว"
    }

    fun openPicker(cameras: List<TvCamera>, activeId: String, selected: (String) -> Unit) {
        if (hasOverlay) return
        showChrome(true); overlayFocus = findFocus()
        content.descendantFocusability = ViewGroup.FOCUS_BLOCK_DESCENDANTS
        val shade = FrameLayout(context).apply { setBackgroundColor(0xC9000000.toInt()) }
        val panel = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL; background = s.shape(s.panel, s.edge)
            setPadding(s.dp(24), s.dp(20), s.dp(24), s.dp(16))
        }
        val headingRow = LinearLayout(context).apply { gravity = Gravity.CENTER_VERTICAL }
        headingRow.addView(s.text("เลือกกล้อง  /  ${cameras.size}", 25f, s.paper, true), LinearLayout.LayoutParams(0, -2, 1f))
        val close = s.button("ปิด") { dismissOverlay() }
        headingRow.addView(close, LinearLayout.LayoutParams(s.dp(72), s.dp(48)))
        panel.addView(headingRow)
        panel.addView(s.text("เลื่อนขึ้น–ลง แล้วกด OK เพื่อดูเดี่ยว", 15f, s.muted), LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(5); bottomMargin = s.dp(18) })
        val scroll = ScrollView(context).apply { isFillViewport = true; isFocusable = false; clipToPadding = false }
        val list = LinearLayout(context).apply { orientation = LinearLayout.VERTICAL }
        scroll.addView(list); panel.addView(scroll, LinearLayout.LayoutParams(-1, 0, 1f))
        val entries = mutableListOf<View>()
        cameras.forEachIndexed { index, camera ->
            val item = LinearLayout(context).apply {
                id = View.generateViewId(); orientation = LinearLayout.VERTICAL
                isFocusable = true; isFocusableInTouchMode = true
                setPadding(s.dp(16), s.dp(12), s.dp(16), s.dp(12)); minimumHeight = s.dp(84)
                background = s.shape(s.ink, if (camera.id == activeId) s.accent else s.edge)
                addView(s.text("${(index + 1).toString().padStart(2, '0')}   ${camera.name}", 19f, s.paper, true).apply { maxLines = 2 })
                addView(s.text(camera.modelLabel + if (camera.group.isBlank()) "" else "  ·  ${camera.group}", 14f, s.muted).apply { maxLines = 2 }, LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(4) })
                if (camera.status in listOf("offline", "auth_expired", "needs_credentials", "error")) addView(s.text(if (camera.status == "offline") "ออฟไลน์" else "รอตรวจสอบ · ดูรายละเอียดที่ภาพ", 13f, s.accent), LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(3) })
                contentDescription = "${camera.name}, ${camera.modelLabel}"
                setOnFocusChangeListener { _, focused -> background = s.shape(if (focused) s.accentDark else s.ink, if (focused || camera.id == activeId) s.accent else s.edge, if (focused) 3 else 1) }
                setOnClickListener { dismissOverlay(false); selected(camera.id) }
            }
            entries.add(item); list.addView(item, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = s.dp(10) })
        }
        entries.forEachIndexed { i, item ->
            item.nextFocusUpId = entries.getOrNull(i - 1)?.id ?: close.id
            item.nextFocusDownId = entries.getOrNull(i + 1)?.id ?: item.id
            item.nextFocusLeftId = item.id; item.nextFocusRightId = item.id
        }
        close.nextFocusDownId = entries.firstOrNull()?.id ?: close.id
        close.nextFocusLeftId = close.id; close.nextFocusRightId = close.id; close.nextFocusUpId = close.id
        shade.addView(panel, LayoutParams(s.dp(530), -1, Gravity.END).apply { topMargin = s.dp(27); bottomMargin = s.dp(27); rightMargin = s.dp(48) })
        overlay = shade; addView(shade, LayoutParams(-1, -1))
        val index = cameras.indexOfFirst { it.id == activeId }.coerceAtLeast(0)
        (entries.getOrNull(index) ?: close).requestFocus()
    }
    fun dismissOverlay(restoreFocus: Boolean = true): Boolean {
        val old = overlay ?: return false
        removeView(old); overlay = null; content.descendantFocusability = ViewGroup.FOCUS_AFTER_DESCENDANTS
        if (restoreFocus) {
            val previous = overlayFocus
            if (previous?.isAttachedToWindow == true) previous.requestFocus() else focusCamera(current?.activeId ?: "")
        }
        overlayFocus = null
        return true
    }
}
