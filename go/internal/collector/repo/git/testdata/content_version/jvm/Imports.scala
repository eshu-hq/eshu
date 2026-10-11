import scala.util.Try

object ImportsDemo {
  def parseInt(raw: String): Try[Int] = Try(raw.toInt)
}
