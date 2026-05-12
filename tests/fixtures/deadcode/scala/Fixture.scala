package deadcode.fixture

import akka.actor.Actor
import org.junit.Test
import org.scalatest.funsuite.AnyFunSuite
import play.api.mvc.InjectedController

trait Task {
  def run(): Int
}

class DefaultTask extends Task {
  override def run(): Int = Helpers.directlyUsedHelper()
}

object Helpers {
  def unusedCleanupCandidate(): Int = 42

  def directlyUsedHelper(): Int = 7

  def generatedExcludedHelper(): Int = 13
}

class PublicService {
  def status(): String = "ok"
}

class JobEndpoint extends InjectedController {
  def handle = Action { "handled" }
}

class WorkerActor extends Actor {
  override def receive: Receive = {
    case "run" => sender() ! Helpers.directlyUsedHelper()
  }
}

class FixtureSuite extends AnyFunSuite {
  @Test
  def exercisedByTestRunner(): Unit = {}
}

object ScriptMain extends App {
  Helpers.directlyUsedHelper()
}

object Main {
  val dynamicMethodName = "run"

  def main(args: Array[String]): Unit = {
    val task: Task = () => Helpers.directlyUsedHelper()
    task.run()
    new PublicService().status()
    new JobEndpoint().handle()
  }
}
