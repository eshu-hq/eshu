package comprehensive

sealed trait Shape {
  def area: Double
  def perimeter: Double
  def describe: String = s"${getClass.getSimpleName}: area=$area"
}

case class Circle(radius: Double) extends Shape {
  override def area: Double = math.Pi * radius * radius
  override def perimeter: Double = 2 * math.Pi * radius
}

case class Rectangle(width: Double, height: Double) extends Shape {
  override def area: Double = width * height
  override def perimeter: Double = 2 * (width + height)
}

case class Triangle(a: Double, b: Double, c: Double) extends Shape {
  override def area: Double = {
    val s = (a + b + c) / 2
    math.sqrt(s * (s - a) * (s - b) * (s - c))
  }
  override def perimeter: Double = a + b + c
}

object Shape {
  def largest(shapes: List[Shape]): Option[Shape] =
    shapes.sortBy(_.area).lastOption
}
