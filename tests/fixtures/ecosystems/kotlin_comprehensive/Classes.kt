package comprehensive

// Data class
data class Point(val x: Double, val y: Double) {
    fun distanceTo(other: Point): Double {
        val dx = x - other.x
        val dy = y - other.y
        return kotlin.math.sqrt(dx * dx + dy * dy)
    }
}

// Sealed class
sealed class Result<out T> {
    data class Success<T>(val value: T) : Result<T>()
    data class Failure(val error: String) : Result<Nothing>()

    fun <R> map(transform: (T) -> R): Result<R> = when (this) {
        is Success -> Success(transform(value))
        is Failure -> Failure(error)
    }
}

// Abstract class
abstract class Shape {
    abstract fun area(): Double
    abstract fun perimeter(): Double

    fun describe(): String = "${this::class.simpleName}: area=${area()}"
}

class Circle(val radius: Double) : Shape() {
    override fun area(): Double = Math.PI * radius * radius
    override fun perimeter(): Double = 2 * Math.PI * radius
}

class Rectangle(val width: Double, val height: Double) : Shape() {
    override fun area(): Double = width * height
    override fun perimeter(): Double = 2 * (width + height)
}

// Enum class
enum class Color(val hex: String) {
    RED("#FF0000"),
    GREEN("#00FF00"),
    BLUE("#0000FF");

    fun brightness(): Int = hex.drop(1).chunked(2).sumOf { it.toInt(16) } / 3
}

// Class with companion object
class Person(val name: String, val age: Int) {
    companion object {
        fun create(name: String): Person = Person(name, 0)
    }

    fun greet(): String = "Hi, I'm $name"
}
