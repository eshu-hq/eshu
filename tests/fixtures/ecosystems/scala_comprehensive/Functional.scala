package comprehensive

object Functional {
  // Higher-order functions
  def transform[A, B](items: List[A])(f: A => B): List[B] = items.map(f)

  def filter[A](items: List[A])(p: A => Boolean): List[A] = items.filter(p)

  def reduce[A](items: List[A])(f: (A, A) => A): A = items.reduce(f)

  // Pattern matching
  def describe(x: Any): String = x match {
    case i: Int if i > 0 => s"Positive: $i"
    case s: String => s"String: $s"
    case (a, b) => s"Tuple: ($a, $b)"
    case head :: tail => s"List starting with $head"
    case None => "Nothing"
    case Some(v) => s"Something: $v"
    case _ => "Unknown"
  }

  // For comprehension
  def combinations(xs: List[Int], ys: List[Int]): List[(Int, Int)] =
    for {
      x <- xs
      y <- ys
      if x + y > 5
    } yield (x, y)

  // Partial functions
  val safeDiv: PartialFunction[(Int, Int), Double] = {
    case (a, b) if b != 0 => a.toDouble / b
  }

  // Currying
  def multiply(a: Int)(b: Int): Int = a * b
  val double: Int => Int = multiply(2)
}
