package comprehensive

object Main extends App {
  val person = new Person("Alice", 30)
  println(person.greet())

  val shapes: List[Shape] = List(
    Circle(5.0),
    Rectangle(3.0, 4.0)
  )
  shapes.foreach(s => println(s.describe))

  val result = Functional.transform(List(1, 2, 3))(x => x * 2)
  println(result)
}

object AppConfig {
  val version: String = "1.0.0"
  val debug: Boolean = false

  def formatVersion: String = s"v$version"
}
