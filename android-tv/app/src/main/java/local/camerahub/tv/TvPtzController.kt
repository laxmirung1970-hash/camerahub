package local.camerahub.tv

/** Main-thread controller. At most one request is in flight; stop always precedes a new move. */
class TvPtzController<T>(
    private val send: (T, String, (Boolean) -> Unit) -> Unit,
    private val schedule: (Long, () -> Unit) -> (() -> Unit),
    private val onFailure: () -> Unit,
) {
    private data class Move<T>(val target: T, val direction: String)
    private var wanted: Move<T>? = null
    private var issued: Move<T>? = null
    private var inFlight = false
    private var cancelRepeat: (() -> Unit)? = null
    private var closed = false
    private var whenClosed: (() -> Unit)? = null
    private var stopAttempts = 0

    fun hold(target: T, direction: String) {
        if (closed || direction !in listOf("left", "right", "up", "down")) return
        val next = Move(target, direction)
        if (wanted == next) return
        cancelTimer(); wanted = next; pump()
    }
    fun release() { cancelTimer(); wanted = null; pump() }
    fun close(done: () -> Unit = {}) { closed = true; whenClosed = done; release() }
    private fun cancelTimer() { cancelRepeat?.invoke(); cancelRepeat = null }
    private fun pump() {
        if (inFlight) return
        val previous = issued
        if (previous != null && previous != wanted) {
            inFlight = true
            send(previous.target, "stop") { ok ->
                inFlight = false
                if (ok) { issued = null; stopAttempts = 0; pump() }
                else {
                    wanted = null
                    if (++stopAttempts < 2) cancelRepeat = schedule(250) { cancelRepeat = null; pump() }
                    else { issued = null; stopAttempts = 0; onFailure(); finishIfClosed() }
                }
            }
            return
        }
        val next = wanted
        if (next == null) { finishIfClosed(); return }
        inFlight = true; issued = next
        send(next.target, next.direction) { ok ->
            inFlight = false
            if (!ok) { wanted = null; onFailure() }
            if (wanted != issued) pump()
            else cancelRepeat = schedule(350) { cancelRepeat = null; pump() }
        }
    }
    private fun finishIfClosed() {
        if (closed && !inFlight && issued == null) { val done = whenClosed; whenClosed = null; done?.invoke() }
    }
}
