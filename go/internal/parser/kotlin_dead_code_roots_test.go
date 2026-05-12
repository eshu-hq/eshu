package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathKotlinEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "src/main/kotlin/example/App.kt")
	writeTestFile(
		t,
		sourcePath,
		`package example

import jakarta.annotation.PostConstruct
import org.gradle.api.Plugin
import org.gradle.api.Project
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.TaskAction
import org.junit.jupiter.api.Test
import org.springframework.context.annotation.Bean
import org.springframework.scheduling.annotation.Scheduled
import org.springframework.stereotype.Service
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.RestController

interface Runner {
    fun run()
}

class Worker : Runner {
    constructor(name: String)

    override fun run() {}

    fun helper() {}
}

class DemoPlugin : Plugin<Project> {
    override fun apply(project: Project) {}
}

open class DemoTask : org.gradle.api.DefaultTask() {
    @TaskAction
    fun execute() {}

    @Input
    fun getTarget(): String = "demo"

    fun setEnabled(enabled: Boolean) {}

    fun helper() {}
}

@RestController
class GreetingController {
    @GetMapping("/hello")
    fun hello(): String = "hello"

    @Bean
    fun client(): String = "client"

    @Scheduled(fixedDelay = 1000)
    fun tick() {}

    @PostConstruct
    fun init() {}

    private fun helper() {}
}

class Tests {
    @Test
    fun runsFromTestRunner() {}
}

fun main(args: Array<String>) {}

private fun unusedCleanupCandidate() {}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "interfaces", "Runner"), "dead_code_root_kinds", "kotlin.interface_type")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Runner"), "dead_code_root_kinds", "kotlin.interface_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "constructor", "Worker"), "dead_code_root_kinds", "kotlin.constructor")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Worker"), "dead_code_root_kinds", "kotlin.override_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Worker"), "dead_code_root_kinds", "kotlin.interface_implementation_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "apply", "DemoPlugin"), "dead_code_root_kinds", "kotlin.gradle_plugin_apply")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "execute", "DemoTask"), "dead_code_root_kinds", "kotlin.gradle_task_action")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "getTarget", "DemoTask"), "dead_code_root_kinds", "kotlin.gradle_task_property")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "setEnabled", "DemoTask"), "dead_code_root_kinds", "kotlin.gradle_task_setter")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "classes", "GreetingController"), "dead_code_root_kinds", "kotlin.spring_component_class")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "hello", "GreetingController"), "dead_code_root_kinds", "kotlin.spring_request_mapping_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "client", "GreetingController"), "dead_code_root_kinds", "kotlin.spring_bean_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "tick", "GreetingController"), "dead_code_root_kinds", "kotlin.spring_scheduled_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "init", "GreetingController"), "dead_code_root_kinds", "kotlin.lifecycle_callback_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "runsFromTestRunner", "Tests"), "dead_code_root_kinds", "kotlin.junit_test_method")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "main"), "dead_code_root_kinds", "kotlin.main_function")

	for _, tc := range []struct {
		name         string
		classContext string
	}{
		{name: "helper", classContext: "Worker"},
		{name: "helper", classContext: "DemoTask"},
		{name: "helper", classContext: "GreetingController"},
		{name: "unusedCleanupCandidate"},
	} {
		function := assertFunctionByName(t, got, tc.name)
		if tc.classContext != "" {
			function = assertFunctionByNameAndClass(t, got, tc.name, tc.classContext)
		}
		if function["dead_code_root_kinds"] != nil {
			t.Fatalf("%s.%s dead_code_root_kinds = %#v, want nil", tc.classContext, tc.name, function["dead_code_root_kinds"])
		}
	}
}

func TestDefaultEngineParsePathKotlinDeadCodeFixtureExpectedRoots(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("deadcode", "kotlin")
	sourcePath := repoFixturePath("deadcode", "kotlin", "Fixture.kt")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "interfaces", "Task"), "dead_code_root_kinds", "kotlin.interface_type")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Task"), "dead_code_root_kinds", "kotlin.interface_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "execute", "DefaultTaskFixture"), "dead_code_root_kinds", "kotlin.gradle_task_action")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "classes", "JobRoute"), "dead_code_root_kinds", "kotlin.spring_component_class")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "constructor", "JobRoute"), "dead_code_root_kinds", "kotlin.constructor")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "handle", "JobRoute"), "dead_code_root_kinds", "kotlin.spring_request_mapping_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "exercisedByTestRunner", "FixtureTests"), "dead_code_root_kinds", "kotlin.junit_test_method")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "main"), "dead_code_root_kinds", "kotlin.main_function")
	if helper := assertFunctionByName(t, got, "unusedCleanupCandidate"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("unusedCleanupCandidate dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathKotlinKeepsPendingMultilineAnnotations(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "src/main/kotlin/example/Routes.kt")
	writeTestFile(
		t,
		sourcePath,
		`package example

import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController

@RestController
class Routes {
    @RequestMapping(
        "/status"
    )
    fun status(): String = "ok"

    @GetMapping(
        path = ["/health"]
    )
    fun health(): String = "ok"

    private fun helper(): String = "unused"
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "status", "Routes"), "dead_code_root_kinds", "kotlin.spring_request_mapping_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "health", "Routes"), "dead_code_root_kinds", "kotlin.spring_request_mapping_method")
	if helper := assertFunctionByNameAndClass(t, got, "helper", "Routes"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("Routes.helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}
