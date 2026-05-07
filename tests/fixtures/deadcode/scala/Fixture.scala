package deadcode.fixture

trait Task {
  def run(): Int
}

object Helpers {
  def unusedCleanupCandidate(): Int = 42

  def directlyUsedHelper(): Int = 7

  def generatedExcludedHelper(): Int = 13
}

class PublicService {
  def status(): String = "ok"
}

class JobEndpoint {
  @deprecated("fixture framework root", "1.0")
  def handle(): String = "handled"
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
