package local.camerahub.tv
import org.junit.Assert.*
import org.junit.Test

class CameraPagingTest {
    @Test fun tourVisitsAll17ItemsAndWraps() {
        val items = (0 until 17).toList()
        val pages = (0 until CameraPaging.count(items.size)).map { CameraPaging.items(items, it) }
        assertEquals(listOf(4,4,4,4,1), pages.map { it.size })
        assertEquals(items, pages.flatten())
        assertEquals(0, CameraPaging.next(4,1,17))
        assertEquals(4, CameraPaging.next(0,-1,17))
    }
    @Test fun emptyAndShrunkListsRemainSafe() {
        assertTrue(CameraPaging.items(emptyList<Int>(), 9).isEmpty())
        assertEquals(listOf(1,2), CameraPaging.items(listOf(1,2),9))
    }
}
