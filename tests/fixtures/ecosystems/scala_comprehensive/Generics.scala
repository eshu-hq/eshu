package comprehensive

// Generic class with variance
class Container[+A](val value: A) {
  def map[B](f: A => B): Container[B] = new Container(f(value))
  def flatMap[B](f: A => Container[B]): Container[B] = f(value)
}

// Contravariant
trait Comparator[-A] {
  def compare(a1: A, a2: A): Int
}

// Upper bound
class Sorter[A <: Ordered[A]] {
  def sort(items: List[A]): List[A] = items.sorted
}

// Generic method
object Collections {
  def first[A](items: List[A]): Option[A] = items.headOption

  def zip[A, B](as: List[A], bs: List[B]): List[(A, B)] = as.zip(bs)

  def groupBy[A, K](items: List[A])(key: A => K): Map[K, List[A]] =
    items.groupBy(key)
}

// Type class pattern
trait Show[A] {
  def show(a: A): String
}

object Show {
  implicit val intShow: Show[Int] = (a: Int) => a.toString
  implicit val stringShow: Show[String] = (a: String) => s""""$a""""

  def apply[A](implicit s: Show[A]): Show[A] = s
}
