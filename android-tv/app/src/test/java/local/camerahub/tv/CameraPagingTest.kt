package local.camerahub.tv
import org.junit.Assert.*
import org.junit.Test

class CameraPagingTest {
    @Test fun tourVisitsAll17ItemsAndWraps() {
        val items = (0 until 17).toList()
        val pages = CameraPaging.pages(items)
        assertEquals(listOf(4,4,4,4,1), pages.map { it.size })
        assertEquals(items, pages.flatten())
        assertEquals(0, CameraPaging.next(4,1,pages.size))
        assertEquals(4, CameraPaging.next(0,-1,pages.size))
        assertEquals(4, CameraPaging.pageOf(pages, 16)); assertEquals(0, CameraPaging.pageOf(pages, 99))
    }
    @Test fun emptyAndShrunkListsRemainSafe() {
        assertTrue(CameraPaging.pages(emptyList<Int>()).isEmpty())
        assertEquals(1, CameraPaging.count(0)); assertEquals(0, CameraPaging.clamp(9, 0))
        assertEquals(0, CameraPaging.pageOf(emptyList<List<Int>>(), 0))
    }
    @Test fun groupsStayTogetherAndNeverShareAPage() {
        // Same cases as web/src/monitoring.test.ts: group order follows each group's first camera.
        val owner = listOf("a" to "หลังบ้าน", "b" to "ชั้น 2", "c" to "หลังบ้าน", "d" to "", "e" to "ชั้น 2")
        val grouped = CameraPaging.grouped(owner) { it.second }
        assertEquals(listOf("a", "c", "b", "e", "d"), grouped.map { it.first })
        val six = (1..6).map { "x$it" to "ชั้น 2" } + listOf("y" to "หลังบ้าน")
        assertEquals(listOf(4, 2, 1), CameraPaging.pages(six) { it.second }.map { it.size })
        assertEquals(listOf(listOf("a", "c"), listOf("b", "e"), listOf("d")), CameraPaging.pages(grouped) { it.second }.map { p -> p.map { it.first } })
    }
}
