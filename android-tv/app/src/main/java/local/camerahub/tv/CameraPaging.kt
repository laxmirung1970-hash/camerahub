package local.camerahub.tv

object CameraPaging {
    const val SIZE = 4
    fun count(size: Int) = maxOf(1, (size + SIZE - 1) / SIZE)
    fun clamp(page: Int, size: Int) = page.coerceIn(0, count(size) - 1)
    fun next(page: Int, step: Int, size: Int) = Math.floorMod(page + step, count(size))
    fun <T> items(items: List<T>, page: Int): List<T> = items.drop(clamp(page, items.size) * SIZE).take(SIZE)
}
