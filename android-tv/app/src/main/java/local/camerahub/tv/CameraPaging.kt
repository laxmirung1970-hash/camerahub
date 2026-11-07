package local.camerahub.tv

/** Same rules as the web monitor (web/src/monitoring.ts): groups kept together, a page never mixes groups. */
object CameraPaging {
    const val SIZE = 4

    /** Owner order with each group made contiguous: groups in the order of their first item, order kept inside a group. */
    fun <T> grouped(items: List<T>, group: (T) -> String): List<T> {
        val order = items.map(group).distinct()
        return order.flatMap { g -> items.filter { group(it) == g } }
    }
    /** Pages of up to four; each group starts a new page. */
    fun <T> pages(items: List<T>, group: (T) -> String = { "" }): List<List<T>> {
        val pages = mutableListOf<List<T>>()
        var page = mutableListOf<T>()
        for (item in items) {
            if (page.size == SIZE || (page.isNotEmpty() && group(page[0]) != group(item))) { pages += page; page = mutableListOf() }
            page += item
        }
        if (page.isNotEmpty()) pages += page
        return pages
    }
    fun count(pages: Int) = maxOf(1, pages)
    fun clamp(page: Int, pages: Int) = page.coerceIn(0, count(pages) - 1)
    fun next(page: Int, step: Int, pages: Int) = Math.floorMod(page + step, count(pages))
    /** Page holding [index]; 0 when out of range. */
    fun <T> pageOf(pages: List<List<T>>, index: Int): Int {
        var seen = 0
        pages.forEachIndexed { p, items -> seen += items.size; if (index in 0 until seen) return p }
        return 0
    }
}
