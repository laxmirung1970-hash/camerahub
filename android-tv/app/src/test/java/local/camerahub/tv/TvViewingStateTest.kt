package local.camerahub.tv

import org.junit.Assert.*
import org.junit.Test

class TvViewingStateTest {
    private fun cameras() = (0 until 17).map { TvCamera("id$it", "กล้อง $it", status = if (it % 3 == 0) "offline" else "online") }
    @Test fun tourIncludesProblemCamerasAndWrapsAcrossFivePages() {
        val state = TvViewingState().apply { replace(cameras()) }
        val visited = mutableListOf<String>()
        repeat(5) { visited += state.showing.map { it.id }; state.step(1) }
        assertEquals(cameras().map { it.id }, visited)
        assertEquals(0, state.page); assertEquals(4, state.showing.size)
        state.step(-1); assertEquals(listOf("id16"), state.showing.map { it.id })
    }
    @Test fun singleBackRestoresSameCameraAndPageAfterMetadataRefresh() {
        val state = TvViewingState().apply { replace(cameras()); step(1); focus("id6"); open("id6") }
        state.replace(cameras().map { it.copy(name = "ชื่อใหม่ ${it.id}", model = "xiaomi.camera.c302n") })
        assertTrue(state.single); assertEquals("id6", state.activeId)
        assertEquals("Xiaomi C302", state.active?.modelLabel)
        state.grid()
        assertEquals(1, state.page); assertEquals("id6", state.activeId)
        assertEquals(listOf("id4", "id5", "id6", "id7"), state.showing.map { it.id })
    }
    @Test fun cameraRemovedWhileWatchingAndEmptyCatalogRemainSafe() {
        val state = TvViewingState().apply { replace(cameras()); open("id16") }
        state.replace(cameras().take(2))
        assertFalse(state.single); assertEquals(0, state.page); assertEquals("id0", state.activeId)
        state.replace(emptyList()); state.step(1); state.open("missing")
        assertTrue(state.showing.isEmpty()); assertEquals("", state.activeId)
    }
    @Test fun singleNextWrapsAndBackReturnsToItsOwnGridPage() {
        val state = TvViewingState().apply { replace(cameras()); open("id16"); step(1) }
        assertEquals("id0", state.activeId); assertTrue(state.single)
        state.step(-1); state.grid()
        assertEquals(4, state.page); assertEquals("id16", state.activeId)
    }
    @Test fun partialGridHasNoUnreachableCameraOrFocusTrap() {
        assertEquals(1, TvGridFocus.target(0, 4, TvGridFocus.Direction.RIGHT))
        assertEquals(3, TvGridFocus.target(1, 4, TvGridFocus.Direction.DOWN))
        assertEquals(-1, TvGridFocus.target(3, 4, TvGridFocus.Direction.DOWN))
        assertEquals(2, TvGridFocus.target(2, 3, TvGridFocus.Direction.RIGHT))
        assertEquals(-1, TvGridFocus.target(0, 1, TvGridFocus.Direction.DOWN))
        assertEquals(0, TvGridFocus.target(0, 1, TvGridFocus.Direction.UP))
    }
    @Test fun tourWaitsForInactivityAndPausesInSingleMenusAndPtz() {
        var now = 0L; val clock = TvTourClock({ now })
        val grid = TvTourMode.GRID
        now = 29_001; assertEquals(1, clock.secondsLeft()); assertFalse(clock.due(grid, false, false, 17))
        now = 30_000; assertTrue(clock.due(grid, false, false, 17))
        assertFalse(clock.due(grid, true, false, 17)); assertFalse(clock.due(grid, false, true, 17))
        assertFalse(clock.due(TvTourMode.OFF, false, false, 17)); assertFalse(clock.due(grid, false, false, 4))
        clock.touch(); now += 2000; assertEquals(28, clock.secondsLeft())
    }
    @Test fun singleTourOpensActiveCameraThenWalksEveryCameraFullScreen() {
        val state = TvViewingState().apply { replace(cameras()); step(1); focus("id6") }
        state.advance(TvTourMode.SINGLE)
        assertTrue(state.single); assertEquals("id6", state.activeId)
        val visited = mutableListOf<String>()
        repeat(17) { state.advance(TvTourMode.SINGLE); visited += state.activeId }
        assertEquals((7 until 17).map { "id$it" } + (0..6).map { "id$it" }, visited)
        state.grid(); assertEquals(1, state.page)
        state.advance(TvTourMode.GRID); assertFalse(state.single); assertEquals(2, state.page)
    }
    @Test fun singleTourRunsInsideSingleViewAndModeCyclesWithLegacyPreference() {
        var now = 0L; val clock = TvTourClock({ now }); now = 30_000
        assertTrue(clock.due(TvTourMode.SINGLE, true, false, 17)); assertTrue(clock.due(TvTourMode.SINGLE, false, false, 4))
        assertTrue(clock.due(TvTourMode.SINGLE, false, false, 1)); assertFalse(clock.due(TvTourMode.SINGLE, true, false, 1))
        assertFalse(clock.due(TvTourMode.SINGLE, true, true, 17)); assertFalse(clock.running(TvTourMode.SINGLE, false, 0))
        assertEquals(listOf(TvTourMode.SINGLE, TvTourMode.OFF, TvTourMode.GRID), listOf(TvTourMode.GRID.next(), TvTourMode.SINGLE.next(), TvTourMode.OFF.next()))
        assertEquals(TvTourMode.GRID, TvTourMode.from(null)); assertEquals(TvTourMode.OFF, TvTourMode.from(null, legacyAuto = false))
        assertEquals(TvTourMode.SINGLE, TvTourMode.from("single", legacyAuto = false))
    }
    @Test fun singleCameraTourMovesEvery12SecondsWhileGridKeeps30() {
        var now = 0L; val clock = TvTourClock({ now })
        now = 11_001; assertEquals(1, clock.secondsLeft(TvTourMode.SINGLE)); assertFalse(clock.due(TvTourMode.SINGLE, true, false, 17))
        now = 12_000; assertTrue(clock.due(TvTourMode.SINGLE, true, false, 17)); assertFalse(clock.due(TvTourMode.GRID, false, false, 17))
        assertEquals(18, clock.secondsLeft(TvTourMode.GRID)); assertFalse(clock.due(TvTourMode.SINGLE, true, true, 17))
        clock.touch(); now += 2000; assertEquals(10, clock.secondsLeft(TvTourMode.SINGLE))
    }
    @Test fun upcomingNamesWhatTheNextTourMoveShowsAndWrapsAround() {
        val state = TvViewingState(); state.replace(cameras())
        assertEquals(listOf("id4", "id5", "id6", "id7"), state.upcoming(TvTourMode.GRID).map { it.id })
        repeat(4) { state.advance(TvTourMode.GRID) }; assertEquals(4, state.page)
        assertEquals(listOf("id0", "id1", "id2", "id3"), state.upcoming(TvTourMode.GRID).map { it.id })
        assertEquals(emptyList<String>(), state.upcoming(TvTourMode.SINGLE).map { it.id })
        state.open("id16"); assertEquals(listOf("id0"), state.upcoming(TvTourMode.SINGLE).map { it.id })
        val small = TvViewingState(); small.replace(cameras().take(3))
        assertEquals(emptyList<String>(), small.upcoming(TvTourMode.GRID))
    }
    @Test fun retryBackoffIsBoundedAndModelAndErrorsAreReadable() {
        assertEquals(listOf(10_000L, 20_000L, 40_000L, 60_000L, 60_000L), (0..4).map(TvRetryPolicy::delayMs))
        assertEquals(60_000L, TvRetryPolicy.delayMs(Int.MAX_VALUE))
        assertEquals("Xiaomi C300 Dual", TvCamera("x", "บ้าน", "chuangmi.camera.072ae2").modelLabel)
        assertFalse(TvCamera("x", "บ้าน", message = "อ่าน rtsp://user:secret@host/a ไม่ได้").problem.contains("secret"))
        assertTrue(TvCamera("x", "บ้าน", status = "auth_expired").accountBlocked)
    }

    @Test
    fun liveCatchUpSpeedsUpOnlyWhileFarBehindWithHysteresis() {
        assertEquals(1f, TvLiveCatchUp.speed(1000, 1f))
        assertEquals(TvLiveCatchUp.FAST, TvLiveCatchUp.speed(3500, 1f))
        assertEquals(TvLiveCatchUp.FAST, TvLiveCatchUp.speed(2000, TvLiveCatchUp.FAST))
        assertEquals(1f, TvLiveCatchUp.speed(2000, 1f))
        assertEquals(1f, TvLiveCatchUp.speed(1500, TvLiveCatchUp.FAST))
    }
}
