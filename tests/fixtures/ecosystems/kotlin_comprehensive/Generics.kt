package comprehensive

class Box<T>(private val value: T) {
    fun unwrap(): T = value
}

class Service {
    fun info(): String = "ok"
}

fun createBox(): Box<Service>? = Box(Service())

fun usage(): String {
    val typedBox: Box<Service>? = Box(Service())
    val returnedBox = createBox()
    return typedBox.unwrap().info() + returnedBox.unwrap().info()
}
