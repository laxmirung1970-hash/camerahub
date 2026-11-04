package local.camerahub.tv

import org.junit.Assert.*
import org.junit.Test

class TvPtzControllerTest {
    private class Rig {
        data class Request(val camera: String, val direction: String, val done: (Boolean) -> Unit)
        data class Timer(val delay: Long, val task: () -> Unit, var cancelled: Boolean = false)
        val sent = mutableListOf<Request>(); val timers = mutableListOf<Timer>(); var failures = 0
        val control = TvPtzController<String>(
            { camera, direction, done -> sent += Request(camera, direction, done) },
            { delay, task -> val t = Timer(delay, task); timers += t; ({ t.cancelled = true }) },
            { failures++ },
        )
        fun done(index: Int, ok: Boolean = true) = sent[index].done(ok)
        fun tick() { val timer = timers.first { !it.cancelled }; timer.cancelled = true; timer.task() }
        fun commands() = sent.map { "${it.camera}:${it.direction}" }
    }
    @Test fun slowRequestCannotQueueRepeatedMovesAndReleaseStopsItsCapturedCamera() {
        val r = Rig(); r.control.hold("A", "left")
        repeat(30) { r.control.hold("A", "left") }
        assertEquals(listOf("A:left"), r.commands())
        r.control.release(); assertEquals(1, r.sent.size)
        r.done(0); assertEquals(listOf("A:left", "A:stop"), r.commands())
        r.done(1); assertTrue(r.timers.none { !it.cancelled })
    }
    @Test fun changingCameraOrDirectionAlwaysStopsOldCameraFirst() {
        val r = Rig(); r.control.hold("A", "up"); r.control.hold("B", "right")
        r.done(0); assertEquals(listOf("A:up", "A:stop"), r.commands())
        r.done(1); assertEquals("B:right", r.commands().last())
        r.control.hold("B", "down"); r.done(2)
        assertEquals("B:stop", r.commands().last())
        r.done(3); assertEquals("B:down", r.commands().last())
    }
    @Test fun repeatIsScheduledOnlyAfterReplyAndCancelledOnRelease() {
        val r = Rig(); r.control.hold("A", "left"); assertTrue(r.timers.isEmpty())
        r.done(0); assertEquals(350L, r.timers.single().delay)
        r.tick(); assertEquals(listOf("A:left", "A:left"), r.commands())
        r.control.release(); r.done(1); assertEquals("A:stop", r.commands().last())
        r.done(2); assertTrue(r.timers.none { !it.cancelled })
    }
    @Test fun failedMoveStillSendsStopAndStopHasOneBoundedRetry() {
        val r = Rig(); r.control.hold("A", "up"); r.done(0, false)
        assertEquals(1, r.failures); assertEquals("A:stop", r.commands().last())
        r.done(1, false); assertEquals(250L, r.timers.single().delay)
        r.tick(); assertEquals("A:stop", r.commands().last())
        r.done(2, false); assertEquals(2, r.failures)
        assertTrue(r.timers.none { !it.cancelled }); assertEquals(3, r.sent.size)
    }
    @Test fun closingDuringRequestWaitsUntilStopBeforeDisposingTransport() {
        val r = Rig(); var closed = false
        r.control.hold("A", "down"); r.control.close { closed = true }
        assertFalse(closed); r.done(0); assertFalse(closed)
        r.done(1); assertTrue(closed)
        r.control.hold("B", "left"); assertEquals(2, r.sent.size)
    }
}
