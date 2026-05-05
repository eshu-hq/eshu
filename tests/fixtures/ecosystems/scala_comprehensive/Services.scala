package comprehensive

abstract class Service(val name: String) {
  def start(): Unit
  def stop(): Unit
  def isRunning: Boolean
}

class HttpService(name: String, val port: Int) extends Service(name) {
  private var running = false

  override def start(): Unit = { running = true }
  override def stop(): Unit = { running = false }
  override def isRunning: Boolean = running
}

object HttpService {
  def apply(port: Int): HttpService = new HttpService("http", port)
  def default: HttpService = apply(8080)
}

class Person(val name: String, val age: Int) {
  def greet(): String = s"Hello, I'm $name"
  override def toString: String = s"Person($name, $age)"
}

object Person {
  def apply(name: String, age: Int): Person = new Person(name, age)
}
