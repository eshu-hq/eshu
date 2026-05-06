package comprehensive

// Higher-order functions
fun <T, R> List<T>.customMap(transform: (T) -> R): List<R> {
    return map(transform)
}

fun <T> List<T>.customFilter(predicate: (T) -> Boolean): List<T> {
    return filter(predicate)
}

// Inline function
inline fun <T> measureTime(block: () -> T): Pair<T, Long> {
    val start = System.currentTimeMillis()
    val result = block()
    val elapsed = System.currentTimeMillis() - start
    return Pair(result, elapsed)
}

// Lambda with receiver
fun buildString(block: StringBuilder.() -> Unit): String {
    val sb = StringBuilder()
    sb.block()
    return sb.toString()
}

// Destructuring
fun parseNameAge(input: String): Pair<String, Int> {
    val (name, ageStr) = input.split(":")
    return Pair(name, ageStr.toInt())
}

// When expression
fun describe(obj: Any): String = when (obj) {
    is Int -> "Integer: $obj"
    is String -> "String: $obj"
    is List<*> -> "List of ${obj.size} items"
    in 1..10 -> "Between 1 and 10"
    else -> "Unknown"
}

// Scope functions
fun scopeFunctionExamples() {
    val numbers = mutableListOf(1, 2, 3)

    numbers.also { println("Before: $it") }
        .apply { add(4) }
        .let { list -> list.filter { it > 2 } }
        .run { println("Filtered: $this") }
}
