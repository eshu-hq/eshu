package deadcode.fixture

import org.junit.jupiter.api.Test
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.RestController

interface Task {
    fun run()
}

class DefaultTaskFixture : org.gradle.api.DefaultTask() {
    @org.gradle.api.tasks.TaskAction
    fun execute() {}
}

private fun unusedCleanupCandidate(): Int = 42

private fun directlyUsedHelper(): Int = 7

class PublicService {
    fun status(): String = "ok"
}

@RestController
class JobRoute {
    constructor(prefix: String)

    @GetMapping("/jobs")
    fun handle(): String = "handled"

    private fun helper(): String = "internal"
}

class FixtureTests {
    @Test
    fun exercisedByTestRunner() {}
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
