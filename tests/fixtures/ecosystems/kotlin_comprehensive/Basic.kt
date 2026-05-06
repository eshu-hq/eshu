package comprehensive

fun greet(name: String): String = "Hello, $name!"

fun add(a: Int, b: Int): Int = a + b

fun divide(a: Double, b: Double): Double? {
    return if (b == 0.0) null else a / b
}

fun processItems(items: List<String>, transform: (String) -> String = { it }): List<String> {
    return items.map(transform)
}

// Extension function
fun String.toTitleCase(): String {
    return split(" ").joinToString(" ") { word ->
        word.replaceFirstChar { it.uppercase() }
    }
}

// Top-level property
val VERSION = "1.0.0"

// Object declaration (singleton)
object AppConfig {
    var debug: Boolean = false
    val version: String = VERSION

    fun isProduction(): Boolean = !debug
}
