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

class TvTourClock(private val now: () -> Long, val intervalMs: Long = 30_000) {
    private var touchedAt = now()
    fun touch() { touchedAt = now() }
    fun secondsLeft(): Int = ((intervalMs - (now() - touchedAt)).coerceAtLeast(0) + 999).div(1000).toInt()
    fun due(enabled: Boolean, single: Boolean, blocked: Boolean, count: Int): Boolean =
        enabled && !single && !blocked && count > CameraPaging.SIZE && secondsLeft() == 0
}

object TvRetryPolicy {
    fun delayMs(attempt: Int): Long = when (attempt.coerceAtLeast(0)) {
        0 -> 10_000; 1 -> 20_000; 2 -> 40_000; else -> 60_000
    }
}
