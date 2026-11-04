package local.camerahub.tv

import android.app.Activity
import android.app.AlertDialog
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.text.InputType
import android.view.Gravity
import android.view.KeyEvent
import android.view.View
import android.view.ViewGroup
import android.view.WindowManager
import android.widget.*
import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.concurrent.Executors

@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
class MainActivity : Activity(), TvMonitorView.Events {
    private enum class Screen { CONNECT, PAIR, HOME }
    private class ApiError(val code: Int, message: String) : Exception(message)
    private data class Binding(val playback: TvPlayback, val high: Boolean)
    private data class PtzTarget(val camera: String, val address: String, val credential: String)
    private val ui = Handler(Looper.getMainLooper())
    private val io = Executors.newFixedThreadPool(3)
    private val ptzIo = Executors.newSingleThreadExecutor()
    private val viewing = TvViewingState()
    private val tour = TvTourClock(SystemClock::elapsedRealtime)
    private val players = linkedMapOf<String, Binding>()
    private lateinit var store: SecureStore
    private lateinit var s: TvStyle
    private lateinit var monitor: TvMonitorView
    private lateinit var ptz: TvPtzController<PtzTarget>
    private var server = ""
    private var token = ""
    private var screen = Screen.CONNECT
    private var generation = 0
    private var visible = false
    private var windowFocused = true
    private var isDemo = false
    private var hd = false
    private var audible = false
    private var autoTour = true
    private var ptzCameraId = ""
    private var ptzTarget: PtzTarget? = null
    private var ptzProblem = "สั่งหมุนไม่สำเร็จ · กรุณาตรวจเครื่องกลาง"
    private var heldDirection = ""
    private var heartbeat: Runnable? = null
    private var refreshTask: Runnable? = null
    private var pairingTask: Runnable? = null
    private var pairingTick: Runnable? = null
    private var loadingGeneration: Int? = null
    private var catalogProblem: String? = null
    private var dialog: AlertDialog? = null
    private var connectButton: Button? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        @Suppress("DEPRECATION")
        window.decorView.systemUiVisibility = View.SYSTEM_UI_FLAG_FULLSCREEN or View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY or View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
        s = TvStyle(this); store = SecureStore(this)
        server = store.server; token = store.token()
        autoTour = getPreferences(MODE_PRIVATE).getBoolean("autoTour", true)
        ptz = TvPtzController(
            send = { target, direction, done ->
                ptzIo.execute {
                    val result = runCatching {
                        api("/cameras/${target.camera}/ptz", JSONObject().put("direction", direction), address = target.address, credential = target.credential, timeout = 2500)
                    }
                    ui.post {
                        if (result.isFailure) ptzProblem = (result.exceptionOrNull() as? ApiError)?.message ?: "ติดต่อเครื่องกลางไม่ได้ · หยุดคำสั่งหมุนแล้ว"
                        done(result.isSuccess)
                    }
                }
            },
            schedule = { delay, task ->
                val runnable = Runnable { task() }; ui.postDelayed(runnable, delay)
                ({ ui.removeCallbacks(runnable) })
            },
            onFailure = { if (visible && screen == Screen.HOME) { leavePtz(); toast(ptzProblem) } },
        )
    }
    override fun onResume() {
        super.onResume(); visible = true
        if (server.isNotBlank() && token.isNotBlank()) showHome() else showConnect()
    }
    override fun onPause() {
        visible = false; generation++; cancelTasks(); leavePtz(); releasePlayers(); audible = false
        dialog?.dismiss(); dialog = null
        super.onPause()
    }
    override fun onDestroy() {
        ptz.close { ptzIo.shutdown() }; io.shutdown(); super.onDestroy()
    }
    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus); windowFocused = hasFocus
        if (!hasFocus && ::ptz.isInitialized) leavePtz()
    }
    private fun cancelTasks() {
        listOf(heartbeat, refreshTask, pairingTask, pairingTick).forEach { it?.let(ui::removeCallbacks) }
        heartbeat = null; refreshTask = null; pairingTask = null; pairingTick = null
    }
    private fun toast(message: String) { if (visible) Toast.makeText(this, message, Toast.LENGTH_LONG).show() }
    private fun api(
        path: String,
        body: JSONObject? = null,
        authenticated: Boolean = true,
        address: String = server,
        credential: String = token,
        timeout: Int = 10_000,
    ): String {
        val connection = URL("$address/api$path").openConnection() as HttpURLConnection
        connection.connectTimeout = timeout; connection.readTimeout = timeout; connection.instanceFollowRedirects = false
        if (authenticated) connection.setRequestProperty("Authorization", "Bearer $credential")
        try {
            if (body != null) {
                connection.requestMethod = "POST"; connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/json")
                connection.outputStream.use { it.write(body.toString().toByteArray(Charsets.UTF_8)) }
            }
            val code = connection.responseCode
            val raw = (if (code in 200..299) connection.inputStream else connection.errorStream)?.bufferedReader(Charsets.UTF_8)?.use { it.readText() } ?: ""
            if (code !in 200..299) {
                if ((code == 401 || code == 403) && authenticated) ui.post { expireSession(address, credential) }
                val reason = runCatching { JSONObject(raw).optString("error") }.getOrDefault("")
                throw ApiError(code, reason.ifBlank { "เชื่อมต่อเครื่องกลางไม่ได้ ($code)" }.take(180))
            }
            return raw
        } finally { connection.disconnect() }
    }
    private fun expireSession(address: String = server, credential: String = token) {
        if (server != address || token != credential || token.isEmpty()) return
        leavePtz(); store.clear(); token = ""; releasePlayers()
        if (visible) { showConnect(); toast("สิทธิ์ทีวีหมดอายุหรือถูกยกเลิก · กรุณาจับคู่ใหม่") }
    }
    private fun releasePlayers() { players.values.forEach { it.playback.release() }; players.clear() }
    private fun connectPage(): LinearLayout {
        cancelTasks(); leavePtz(); releasePlayers(); dialog?.dismiss(); dialog = null
        val outer = FrameLayout(this).apply { setBackgroundColor(s.ink); setPadding(s.dp(48), s.dp(27), s.dp(48), s.dp(27)) }
        val column = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; gravity = Gravity.CENTER_VERTICAL }
        val scroll = ScrollView(this).apply { isFillViewport = true; isFocusable = false }
        scroll.addView(column, FrameLayout.LayoutParams(-1, -2))
        outer.addView(scroll, FrameLayout.LayoutParams(s.dp(700), -1, Gravity.CENTER)); setContentView(outer)
        return column
    }
    private fun showConnect() {
        generation++; screen = Screen.CONNECT
        val page = connectPage()
        page.addView(s.text("CAMERAHUB  /  TV", 14f, s.accent, true))
        page.addView(s.text("กล้องทุกมุม บนจอเดียว", 34f, s.paper, true), LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(12) })
        page.addView(s.text("1. กรอกที่อยู่เครื่องกลาง  →  2. อนุมัติรหัสจากมือถือ", 18f, s.muted), LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(8); bottomMargin = s.dp(24) })
        page.addView(s.text("ที่อยู่ HTTPS ของ CameraHub", 16f))
        val address = EditText(this).apply {
            id = View.generateViewId(); setSingleLine(true); textSize = 19f; typeface = s.regular
            hint = "https://nas.your-tailnet.ts.net"; setHintTextColor(s.muted); setTextColor(s.paper)
            setText(server); inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
            background = s.shape(s.panel); setPadding(s.dp(18), s.dp(10), s.dp(18), s.dp(10))
            setOnFocusChangeListener { _, focused -> background = s.shape(s.panel, if (focused) s.accent else s.edge, if (focused) 3 else 1) }
        }
        page.addView(address, LinearLayout.LayoutParams(-1, s.dp(56)).apply { topMargin = s.dp(8) })
        val feedback = s.text("ใช้ HTTPS ผ่าน Tailscale · ลงชื่อเข้าใช้ Tailscale บนทีวีก่อน", 14f, s.muted)
        page.addView(feedback, LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(10); bottomMargin = s.dp(20) })
        val row = LinearLayout(this)
        val next = s.button("รับรหัสจับคู่") {
            try { startPairing(ServerAddress.normalize(address.text.toString()), feedback) }
            catch (e: Exception) { feedback.text = e.message ?: "ที่อยู่ไม่ถูกต้อง"; feedback.setTextColor(s.accent) }
        }
        row.addView(next, LinearLayout.LayoutParams(0, s.dp(52), 1f))
        connectButton = next
        if (token.isNotEmpty()) row.addView(s.button("กลับไปดูกล้อง") { showHome() }, LinearLayout.LayoutParams(0, s.dp(52), 1f).apply { marginStart = s.dp(12) })
        page.addView(row)
        page.addView(s.text("ไม่ต้องกรอกรหัสผ่านกล้องบนทีวี · ตั้งค่าเจ้าของระบบบนมือถือให้เรียบร้อยก่อน", 14f, s.muted), LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(18) })
        if (server.isBlank()) address.requestFocus() else next.requestFocus()
    }
    private fun startPairing(address: String, feedback: TextView) {
        if (connectButton?.isEnabled == false) return
        connectButton?.apply { isEnabled = false; text = "กำลังเชื่อมต่อ…" }
        val gen = ++generation
        feedback.text = "กำลังเชื่อมต่อเครื่องกลาง…"; feedback.setTextColor(s.accent)
        io.execute {
            try {
                val boot = JSONObject(api("/bootstrap", authenticated = false, address = address))
                require(!boot.optBoolean("needsSetup")) { "ตั้งค่าเจ้าของระบบบนมือถือก่อน แล้วจึงจับคู่ทีวี" }
                val pair = JSONObject(api("/pair/request", JSONObject().put("name", "${Build.MANUFACTURER} ${Build.MODEL}".take(80)), false, address))
                ui.post {
                    if (!visible || gen != generation) return@post
                    screen = Screen.PAIR
                    val page = connectPage()
                    page.addView(s.text("ขั้นตอน 2 / 2", 14f, s.accent, true))
                    page.addView(s.text("อนุมัติทีวีจากมือถือ", 28f, s.paper, true), LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(8) })
                    page.addView(s.text("เปิด CameraHub → ตั้งค่า → ทีวีและอุปกรณ์", 18f, s.muted), LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(6) })
                    page.addView(s.text(pair.getString("code"), 54f, s.paper, true).apply { letterSpacing = .16f }, LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(12); bottomMargin = s.dp(10) })
                    val progress = s.text("รออนุมัติ · ตรวจรหัสบนมือถือให้ตรงกัน", 16f, s.accent)
                    page.addView(progress)
                    val countdown = s.text("", 14f, s.muted)
                    page.addView(countdown, LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(4); bottomMargin = s.dp(12) })
                    page.addView(s.text(address, 13f, s.muted))
                    val cancel = s.button("กลับไปเปลี่ยนที่อยู่") { showConnect() }
                    page.addView(cancel, LinearLayout.LayoutParams(s.dp(260), s.dp(48)).apply { topMargin = s.dp(12) }); cancel.requestFocus()
                    val deadline = SystemClock.elapsedRealtime() + 300_000
                    pairingTick = object : Runnable {
                        override fun run() {
                            if (!visible || gen != generation) return
                            val seconds = ((deadline - SystemClock.elapsedRealtime()).coerceAtLeast(0) / 1000).toInt()
                            countdown.text = "รหัสหมดอายุใน ${seconds / 60}:${(seconds % 60).toString().padStart(2, '0')}"
                            if (seconds <= 0) { generation++; pairingTask?.let(ui::removeCallbacks); progress.text = "รหัสหมดอายุ · กดกลับแล้วรับรหัสใหม่" }
                            else ui.postDelayed(this, 1000)
                        }
                    }; ui.post(pairingTick!!)
                    pollPair(address, pair.getString("id"), pair.getString("secret"), gen, progress)
                }
            } catch (e: Exception) {
                ui.post { if (visible && gen == generation) {
                    feedback.text = "เชื่อมต่อไม่ได้ · ${e.message ?: "ตรวจที่อยู่และ Tailscale"}"; feedback.setTextColor(s.accent)
                    connectButton?.apply { isEnabled = true; text = "ลองเชื่อมต่ออีกครั้ง" }
                } }
            }
        }
    }
    private fun pollPair(address: String, id: String, secret: String, gen: Int, progress: TextView) {
        pairingTask = Runnable {
            if (!visible || gen != generation) return@Runnable
            io.execute {
                try {
                    val result = JSONObject(api("/pair/poll", JSONObject().put("id", id).put("secret", secret), false, address))
                    ui.post {
                        if (!visible || gen != generation) return@post
                        if (result.optBoolean("approved")) {
                            val granted = result.getString("token")
                            try {
                                store.saveConnection(address, granted); server = address; token = granted; showHome()
                            } catch (_: Exception) { progress.text = "บันทึกสิทธิ์ทีวีไม่ได้ · กรุณาจับคู่ใหม่"; generation++; pairingTick?.let(ui::removeCallbacks) }
                        } else {
                            progress.text = "รออนุมัติ · ตรวจรหัสบนมือถือให้ตรงกัน"
                            pairingTask?.let { ui.postDelayed(it, 2500) }
                        }
                    }
                } catch (e: Exception) {
                    ui.post {
                        if (visible && gen == generation) {
                            if (e is ApiError && e.code in listOf(400, 403, 404, 410)) {
                                progress.text = "รหัสนี้ใช้ไม่ได้แล้ว · กดกลับเพื่อรับรหัสใหม่"; generation++; pairingTick?.let(ui::removeCallbacks)
                            } else { progress.text = "ยังติดต่อเครื่องกลางไม่ได้ · กำลังลองใหม่"; pairingTask?.let { ui.postDelayed(it, 4000) } }
                        }
                    }
                }
            }
        }; ui.post(pairingTask!!)
    }

    private fun showHome() {
        generation++; screen = Screen.HOME; cancelTasks(); leavePtz(); releasePlayers()
        audible = false; catalogProblem = null; tour.touch()
        monitor = TvMonitorView(this, this); setContentView(monitor)
        renderHome(true); loadCameras()
        val gen = generation
        refreshTask = object : Runnable {
            override fun run() {
                if (!visible || screen != Screen.HOME || gen != generation) return
                loadCameras(); ui.postDelayed(this, 15_000)
            }
        }; ui.postDelayed(refreshTask!!, 15_000)
        heartbeat = object : Runnable {
            override fun run() {
                if (!visible || screen != Screen.HOME || gen != generation) return
                val blocked = monitor.hasOverlay || monitor.controlsFocused || monitor.ptzMode || dialog != null || !windowFocused
                if (blocked) tour.touch()
                if (tour.due(autoTour, viewing.single, blocked, viewing.cameras.size)) changePage(1, true)
                if (!blocked && monitor.cameraFocused && SystemClock.elapsedRealtime() - lastKeyAt >= 8000) monitor.showChrome(false)
                monitor.updateClock(SimpleDateFormat("HH:mm", Locale.US).format(Date()))
                if (catalogProblem != null) monitor.updateSummary(catalogProblem!!)
                else monitor.updateTour(if (autoTour && viewing.cameras.size > 4) tour.secondsLeft() else null, blocked)
                ui.postDelayed(this, 1000)
            }
        }; lastKeyAt = SystemClock.elapsedRealtime(); ui.post(heartbeat!!)
    }
    private fun renderHome(focusCamera: Boolean = false) {
        if (!visible || screen != Screen.HOME) return
        val showing = viewing.showing
        val high = viewing.single && hd
        val removed = players.keys.filter { id -> showing.none { it.id == id } || players[id]?.high != high }
        removed.forEach { players.remove(it)?.playback?.release() }
        monitor.render(viewing, isDemo, autoTour, hd, audible)
        for (camera in showing) {
            val existing = players[camera.id]
            if (existing != null) existing.playback.update(camera)
            else {
                val tile = monitor.tiles.getValue(camera.id)
                val gen = generation
                val playback = TvPlayback(tile, camera, server, token, high, isDemo, ui,
                    changed = { ui.post {
                        if (visible && gen == generation && screen == Screen.HOME && monitor.ptzMode && ptzCameraId == camera.id && !tile.isLive) {
                            leavePtz(); toast("ภาพขาดการเชื่อมต่อ · ออกจากโหมดหมุนแล้ว")
                        }
                    } },
                    sessionExpired = { ui.post { if (visible && gen == generation) expireSession() } },
                )
                players[camera.id] = Binding(playback, high)
            }
        }
        updateAudio()
        if (focusCamera) monitor.focusCamera(viewing.activeId)
    }
    private fun loadCameras() {
        val gen = generation
        if (loadingGeneration == gen) return
        loadingGeneration = gen
        val address = server; val credential = token
        io.execute {
            try {
                val boot = JSONObject(api("/bootstrap", address = address, credential = credential))
                val items = JSONArray(api("/cameras", address = address, credential = credential))
                val next = (0 until items.length()).map { index ->
                    val c = items.getJSONObject(index)
                    TvCamera(c.getString("id"), c.getString("name"), c.optString("model"), c.optString("group"), c.optString("status"), c.optString("message"), c.optBoolean("ptz"), c.optBoolean("audio"))
                }
                ui.post {
                    if (loadingGeneration == gen) loadingGeneration = null
                    if (!visible || gen != generation || screen != Screen.HOME) return@post
                    val first = viewing.cameras.isEmpty()
                    if (first) { lastKeyAt = SystemClock.elapsedRealtime(); tour.touch() }
                    if (ptzCameraId.isNotBlank() && next.none { it.id == ptzCameraId && it.ptz && !it.accountBlocked && it.status != "offline" }) leavePtz()
                    viewing.replace(next); isDemo = boot.optString("mode") == "demo"; catalogProblem = null
                    // Keep the picker stable while someone is choosing. New entries appear next time it opens.
                    renderHome(first && !monitor.hasOverlay)
                    monitor.showCatalogNotice(null)
                }
            } catch (_: Exception) {
                ui.post {
                    if (loadingGeneration == gen) loadingGeneration = null
                    if (visible && gen == generation && screen == Screen.HOME) {
                        catalogProblem = "อ่านรายชื่อไม่ได้ · ตรวจ Tailscale / เครื่องกลาง · กำลังลองใหม่"
                        monitor.updateSummary(catalogProblem!!)
                        monitor.showCatalogNotice("ยังติดต่อเครื่องกลางไม่ได้\nตรวจ Tailscale และเครื่องกลาง · ระบบจะลองใหม่")
                    }
                }
            }
        }
    }
    override fun focusCamera(id: String) { viewing.focus(id); updateAudio() }
    override fun openCamera(id: String) {
        leavePtz(); audible = false; tour.touch(); viewing.open(id); monitor.dismissOverlay(false)
        monitor.showChrome(true); renderHome(true)
    }
    override fun action(name: String) {
        touch()
        when (name) {
            "previous" -> changePage(-1)
            "next" -> changePage(1)
            "grid" -> showGrid()
            "picker" -> { leavePtz(); monitor.openPicker(viewing.cameras, viewing.activeId, ::openCamera) }
            "auto" -> {
                autoTour = !autoTour; getPreferences(MODE_PRIVATE).edit().putBoolean("autoTour", autoTour).apply()
                tour.touch(); renderHome()
            }
            "audio" -> {
                if (viewing.active?.audio == true && monitor.tiles[viewing.activeId]?.isLive == true) {
                    audible = !audible; updateAudio(); renderHome()
                } else toast("รอภาพจากกล้องก่อนเปิดเสียง")
            }
            "ptz" -> enterPtz()
            "quality" -> showDialog(AlertDialog.Builder(this).setTitle("คุณภาพภาพเดี่ยว").setMessage("คุณภาพปกติใช้ข้อมูลและกำลังทีวีน้อยกว่า\nHD เหมาะกับการดูรายละเอียดทีละกล้อง")
                .setPositiveButton(if (hd) "ใช้คุณภาพปกติ" else "ใช้ HD") { _, _ -> hd = !hd; renderHome() }.setNegativeButton("กลับ", null).create())
            "settings" -> settings()
        }
    }
    private fun showGrid() {
        leavePtz(); audible = false; viewing.grid(); tour.touch(); monitor.showChrome(true); renderHome(true)
    }
    private fun changePage(step: Int, automatic: Boolean = false) {
        if (viewing.cameras.isEmpty()) return
        leavePtz(); audible = false
        val keepControls = monitor.controlsFocused && !automatic
        viewing.step(step); tour.touch(); renderHome(!keepControls)
    }
    private fun updateAudio() { players.forEach { (id, binding) -> binding.playback.volume(audible && viewing.single && id == viewing.activeId) } }
    private fun enterPtz() {
        if (!viewing.single || viewing.active?.ptz != true) { toast("กล้องนี้ยังไม่รองรับคำสั่งหมุน"); return }
        if (monitor.tiles[viewing.activeId]?.isLive != true) { toast("รอภาพสดก่อนเริ่มหมุนกล้อง"); return }
        ptzCameraId = viewing.activeId; heldDirection = ""
        ptzTarget = PtzTarget(ptzCameraId, server, token)
        monitor.setPtz(true); monitor.focusCamera(viewing.activeId)
    }
    private fun leavePtz() {
        if (::ptz.isInitialized) ptz.release()
        val wasActive = ptzCameraId.isNotEmpty(); ptzCameraId = ""; ptzTarget = null; heldDirection = ""
        if (::monitor.isInitialized && monitor.ptzMode) {
            monitor.setPtz(false)
            if (wasActive && visible && screen == Screen.HOME) monitor.focusCamera(viewing.activeId)
        }
    }
    private fun settings() {
        leavePtz()
        showDialog(AlertDialog.Builder(this).setTitle("CameraHub TV").setItems(arrayOf("ลองเปิดภาพชุดนี้ใหม่", "วิธีใช้รีโมต", "เปลี่ยนเครื่องกลาง / จับคู่ใหม่", "เกี่ยวกับแอป")) { _, which ->
            when (which) {
                0 -> { players.values.forEach { it.playback.retry() }; loadCameras() }
                1 -> ui.post { showDialog(AlertDialog.Builder(this).setTitle("รีโมต 3 ปุ่มก็ใช้ได้")
                    .setMessage("ลูกศร: เลือกกล้องหรือปุ่ม\nOK ที่ภาพ: ดูเดี่ยว\nกลับ: ออกจากหมุน → 4 ช่อง → ออกจากแอป\n\nกดลงจากแถวล่างเพื่อเปลี่ยนชุดหรือเลือกรายชื่อ\nสลับชุดอัตโนมัติทุก 30 วินาที หยุดขณะใช้เมนู\nแถบเมนูซ่อนเมื่อดูภาพเฉย ๆ กดปุ่มใดก็แสดงอีกครั้ง\n\nหมุนกล้อง: เลือก ‘หมุนกล้อง’ ก่อน กดลูกศรค้างแล้วปล่อยเพื่อหยุด\nChannel + / − ใช้เปลี่ยนชุดหรือกล้องได้ถ้ารีโมตมี")
                    .setPositiveButton("เข้าใจแล้ว", null).create()) }
                2 -> showConnect()
                3 -> ui.post { showDialog(AlertDialog.Builder(this).setTitle("CameraHub TV 1.1.0")
                    .setMessage("Android 8 ขึ้นไป · เปิดพร้อมกันสูงสุด 4 ภาพ\nใช้ชื่อ ลำดับ และรุ่นจากเครื่องกลาง\n\nความสามารถถอดรหัสภาพ เสียง และรีโมตของทีวีแต่ละรุ่นยังต้องตรวจบนเครื่องจริง\nใช้ HTTPS ผ่าน Tailscale โดยไม่ส่งรหัสผ่านกล้องให้ทีวี")
                    .setPositiveButton("ปิด", null).create()) }
            }
        }.setNegativeButton("กลับ", null).create())
    }
    private fun showDialog(next: AlertDialog) {
        dialog?.dismiss(); dialog = next; tour.touch()
        if (::monitor.isInitialized) monitor.showChrome(true)
        next.setOnDismissListener { if (dialog === next) dialog = null; tour.touch() }
        next.setOnShowListener {
            fun style(view: View) {
                if (view is TextView) { view.typeface = s.regular; view.setTextColor(s.paper); if (view.textSize / resources.displayMetrics.scaledDensity < 16) view.textSize = 16f }
                if (view is ViewGroup) for (i in 0 until view.childCount) style(view.getChildAt(i))
            }
            next.window?.decorView?.let(::style)
        }
        next.show()
    }
    private var lastKeyAt = 0L
    private fun touch() {
        lastKeyAt = SystemClock.elapsedRealtime(); tour.touch()
        if (::monitor.isInitialized && screen == Screen.HOME) monitor.showChrome(true)
    }
    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        if (event.action == KeyEvent.ACTION_DOWN) touch()
        if (screen == Screen.HOME && ::monitor.isInitialized && !monitor.hasOverlay && dialog == null) {
            if (monitor.ptzMode) {
                val direction = when (event.keyCode) {
                    KeyEvent.KEYCODE_DPAD_LEFT -> "left"; KeyEvent.KEYCODE_DPAD_RIGHT -> "right"
                    KeyEvent.KEYCODE_DPAD_UP -> "up"; KeyEvent.KEYCODE_DPAD_DOWN -> "down"; else -> ""
                }
                if (direction.isNotEmpty()) {
                    if (event.action == KeyEvent.ACTION_DOWN) { heldDirection = direction; ptzTarget?.let { ptz.hold(it, direction) }; monitor.setPtz(true, direction) }
                    else if (event.action == KeyEvent.ACTION_UP && heldDirection == direction) { heldDirection = ""; ptz.release(); monitor.setPtz(true) }
                    return true
                }
                if (event.keyCode == KeyEvent.KEYCODE_DPAD_CENTER || event.keyCode == KeyEvent.KEYCODE_ENTER) {
                    if (event.action == KeyEvent.ACTION_UP) leavePtz()
                    return true
                }
            } else {
                val step = when (event.keyCode) {
                    KeyEvent.KEYCODE_PAGE_DOWN, KeyEvent.KEYCODE_CHANNEL_DOWN, KeyEvent.KEYCODE_MEDIA_NEXT -> 1
                    KeyEvent.KEYCODE_PAGE_UP, KeyEvent.KEYCODE_CHANNEL_UP, KeyEvent.KEYCODE_MEDIA_PREVIOUS -> -1
                    else -> 0
                }
                if (step != 0) { if (event.action == KeyEvent.ACTION_DOWN && event.repeatCount == 0) changePage(step); return true }
                if (event.keyCode == KeyEvent.KEYCODE_MENU) { if (event.action == KeyEvent.ACTION_DOWN && event.repeatCount == 0) settings(); return true }
            }
        }
        return super.dispatchKeyEvent(event)
    }
    @Deprecated("TV remote back navigation")
    override fun onBackPressed() {
        touch()
        when {
            screen == Screen.HOME && monitor.dismissOverlay() -> Unit
            screen == Screen.HOME && monitor.ptzMode -> leavePtz()
            screen == Screen.HOME && viewing.single -> showGrid()
            screen == Screen.HOME && monitor.controlsFocused -> monitor.focusCamera(viewing.activeId)
            screen == Screen.HOME -> showDialog(AlertDialog.Builder(this).setTitle("ออกจาก CameraHub?").setMessage("กลับมาเปิดแอปเพื่อดูกล้องต่อได้ทุกเมื่อ")
                .setPositiveButton("ดูกล้องต่อ", null).setNegativeButton("ออกจากแอป") { _, _ -> finish() }.create())
            token.isNotEmpty() -> showHome()
            screen == Screen.PAIR -> showConnect()
            else -> super.onBackPressed()
        }
    }
}
