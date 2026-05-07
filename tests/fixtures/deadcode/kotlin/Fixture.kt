package deadcode.fixture

interface Task {
    fun run()
}

private fun unusedCleanupCandidate(): Int = 42

private fun directlyUsedHelper(): Int = 7

class PublicService {
    fun status(): String = "ok"
}

class JobRoute {
    @Deprecated("fixture framework root")
    fun handle(): String = "handled"
}

fun generatedExcludedHelper(): Int = 13

val dynamicMethodName = "run"

fun main() {
    val task = object : Task {
        override fun run() {
            directlyUsedHelper()
        }
    }
    task.run()
    PublicService().status()
    JobRoute().handle()
}
