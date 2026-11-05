package local.camerahub.tv

data class TvCamera(
    val id: String,
    val name: String,
    val model: String = "",
    val group: String = "",
    val status: String = "pending",
    val message: String = "",
    val ptz: Boolean = false,
    val audio: Boolean = false,
) {
    val modelLabel: String get() = when (model) {
        "xiaomi.camera.c01a01" -> "Xiaomi C300"
        "xiaomi.camera.c302n" -> "Xiaomi C302"
        "chuangmi.camera.072ae2" -> "Xiaomi C300 Dual"
        "chuangmi.camera.046a01" -> "Mi 360° 1080p"
        "" -> "ยังไม่ทราบรุ่น"
        else -> model
    }
    val accountBlocked: Boolean get() = status in listOf("auth_expired", "needs_credentials")
    val problem: String get() = when (status) {
        "offline" -> "กล้องออฟไลน์ · ตรวจไฟและเครือข่ายของกล้อง"
        "auth_expired" -> "บัญชีกล้องหมดอายุ · ยืนยันบัญชีจากมือถือหรือคอมพิวเตอร์"
        "needs_credentials" -> "ยังไม่มีบัญชีกล้อง · ตั้งค่าจากมือถือหรือคอมพิวเตอร์"
        else -> message.replace(Regex("(?i)(https?|rtsp)://\\S+"), "เครื่องกลาง").take(140)
    }
}

/** Keeps the viewing position across metadata refreshes; never filters out problem cameras. */
class TvViewingState {
    var cameras: List<TvCamera> = emptyList()
        private set
    var page: Int = 0
        private set
    var activeId: String = ""
        private set
    var single: Boolean = false
        private set
    val active: TvCamera? get() = cameras.find { it.id == activeId }
    val pageCount: Int get() = CameraPaging.count(cameras.size)
    val showing: List<TvCamera> get() = if (single) listOfNotNull(active) else CameraPaging.items(cameras, page)

    fun replace(next: List<TvCamera>) {
        cameras = next.distinctBy { it.id }
        if (single && cameras.none { it.id == activeId }) single = false
        page = if (single) cameras.indexOfFirst { it.id == activeId } / CameraPaging.SIZE else CameraPaging.clamp(page, cameras.size)
        if (showing.none { it.id == activeId }) activeId = showing.firstOrNull()?.id ?: ""
    }
    fun focus(id: String) { if (showing.any { it.id == id }) activeId = id }
    fun open(id: String) {
        val index = cameras.indexOfFirst { it.id == id }
        if (index < 0) return
        activeId = id; page = index / CameraPaging.SIZE; single = true
    }
    fun grid() { single = false; page = CameraPaging.clamp(page, cameras.size) }
    /** One automatic tour move: next page of 4, or next camera full screen (entering single view first). */
    fun advance(mode: TvTourMode) {
        if (mode == TvTourMode.SINGLE && !single) (active ?: showing.firstOrNull())?.let { open(it.id) } else step(1)
    }
    /** Cameras the next [advance] would put on screen that are not showing yet, so they can be started early. */
    fun upcoming(mode: TvTourMode): List<TvCamera> {
        if (cameras.isEmpty()) return emptyList()
        val next = when {
            mode == TvTourMode.SINGLE && !single -> listOfNotNull(active ?: showing.firstOrNull())
            single -> listOf(cameras[Math.floorMod(cameras.indexOfFirst { it.id == activeId }.coerceAtLeast(0) + 1, cameras.size)])
            else -> CameraPaging.items(cameras, CameraPaging.next(page, 1, cameras.size))
        }
        return next.filter { n -> showing.none { it.id == n.id } }
    }
    fun step(delta: Int) {
        if (cameras.isEmpty()) return
        if (single) {
            val index = cameras.indexOfFirst { it.id == activeId }.coerceAtLeast(0)
            open(cameras[Math.floorMod(index + delta, cameras.size)].id)
        } else {
            page = CameraPaging.next(page, delta, cameras.size)
            activeId = showing.firstOrNull()?.id ?: ""
        }
    }
}

/** -1 means the footer. Navigation is predictable even on the last, partly filled page. */
object TvGridFocus {
    enum class Direction { LEFT, RIGHT, UP, DOWN }
    fun target(index: Int, count: Int, direction: Direction): Int {
        if (count == 0) return -1
        val current = index.coerceIn(0, count - 1)
        return when (direction) {
            Direction.LEFT -> if (current % 2 == 1) current - 1 else current
            Direction.RIGHT -> if (current % 2 == 0 && current + 1 < count) current + 1 else current
            Direction.UP -> if (current >= 2) current - 2 else current
            Direction.DOWN -> if (current + 2 < count) current + 2 else -1
        }
    }
}

/** Remote button cycles GRID → SINGLE → OFF. GRID keeps the default 4-up tour of every camera every 30 s; SINGLE moves every 12 s. */
enum class TvTourMode(val key: String, val label: String, val intervalMs: Long) {
    GRID("grid", "สลับ: 4 ช่อง", 30_000), SINGLE("single", "สลับ: ทีละกล้อง", 12_000), OFF("off", "สลับ: พัก", 30_000);
    fun next(): TvTourMode = values()[(ordinal + 1) % values().size]
    companion object {
        fun from(key: String?, legacyAuto: Boolean = true): TvTourMode = values().find { it.key == key } ?: if (legacyAuto) GRID else OFF
    }
}

class TvTourClock(private val now: () -> Long) {
    private var touchedAt = now()
    fun touch() { touchedAt = now() }
    fun secondsLeft(mode: TvTourMode = TvTourMode.GRID): Int = ((mode.intervalMs - (now() - touchedAt)).coerceAtLeast(0) + 999).div(1000).toInt()
    /** A camera opened by hand in GRID mode stays put; SINGLE mode also moves from the grid into single view. */
    fun running(mode: TvTourMode, single: Boolean, count: Int): Boolean = when (mode) {
        TvTourMode.OFF -> false
        TvTourMode.GRID -> !single && count > CameraPaging.SIZE
        TvTourMode.SINGLE -> count > 1 || (count == 1 && !single)
    }
    fun due(mode: TvTourMode, single: Boolean, blocked: Boolean, count: Int): Boolean =
        running(mode, single, count) && !blocked && secondsLeft(mode) == 0
}

/** Plays slightly fast while more than 3 s is buffered, back to normal under 1.5 s. */
object TvLiveCatchUp {
    const val FAST = 1.15f
    fun speed(bufferedMs: Long, current: Float): Float = when {
        bufferedMs >= 3000 -> FAST
        bufferedMs <= 1500 -> 1f
        else -> current
    }
}

object TvRetryPolicy {
    fun delayMs(attempt: Int): Long = when (attempt.coerceAtLeast(0)) {
        0 -> 10_000; 1 -> 20_000; 2 -> 40_000; else -> 60_000
    }
}
