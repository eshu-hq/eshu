// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathKotlinEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "src/main/kotlin/example/App.kt")
	writeKotlinTestFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "interfaces", "Runner"), "dead_code_root_kinds", "kotlin.interface_type")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "run", "Runner"), "dead_code_root_kinds", "kotlin.interface_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "constructor", "Worker"), "dead_code_root_kinds", "kotlin.constructor")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "run", "Worker"), "dead_code_root_kinds", "kotlin.override_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "run", "Worker"), "dead_code_root_kinds", "kotlin.interface_implementation_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "apply", "DemoPlugin"), "dead_code_root_kinds", "kotlin.gradle_plugin_apply")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "execute", "DemoTask"), "dead_code_root_kinds", "kotlin.gradle_task_action")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "getTarget", "DemoTask"), "dead_code_root_kinds", "kotlin.gradle_task_property")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "setEnabled", "DemoTask"), "dead_code_root_kinds", "kotlin.gradle_task_setter")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "classes", "GreetingController"), "dead_code_root_kinds", "kotlin.spring_component_class")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "hello", "GreetingController"), "dead_code_root_kinds", "kotlin.spring_request_mapping_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "client", "GreetingController"), "dead_code_root_kinds", "kotlin.spring_bean_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "tick", "GreetingController"), "dead_code_root_kinds", "kotlin.spring_scheduled_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "init", "GreetingController"), "dead_code_root_kinds", "kotlin.lifecycle_callback_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "runsFromTestRunner", "Tests"), "dead_code_root_kinds", "kotlin.junit_test_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "main", ""), "dead_code_root_kinds", "kotlin.main_function")

	for _, tc := range []struct {
		name         string
		classContext string
	}{
		{name: "helper", classContext: "Worker"},
		{name: "helper", classContext: "DemoTask"},
		{name: "helper", classContext: "GreetingController"},
		{name: "unusedCleanupCandidate"},
	} {
		// Top-level functions carry no class_context field, which the
		// shared lookup matches as the empty string, so one helper covers
		// both the method rows and the package-level row.
		function := parsertest.AssertFunctionByNameAndClass(t, got, tc.name, tc.classContext)
		if function["dead_code_root_kinds"] != nil {
			t.Fatalf("%s.%s dead_code_root_kinds = %#v, want nil", tc.classContext, tc.name, function["dead_code_root_kinds"])
		}
	}
}

func TestDefaultEngineParsePathKotlinDeadCodeFixtureExpectedRoots(t *testing.T) {
	t.Parallel()

	repoRoot := kotlinFixturePath("deadcode", "kotlin")
	sourcePath := kotlinFixturePath("deadcode", "kotlin", "Fixture.kt")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "interfaces", "Task"), "dead_code_root_kinds", "kotlin.interface_type")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "run", "Task"), "dead_code_root_kinds", "kotlin.interface_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "execute", "DefaultTaskFixture"), "dead_code_root_kinds", "kotlin.gradle_task_action")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "classes", "JobRoute"), "dead_code_root_kinds", "kotlin.spring_component_class")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "constructor", "JobRoute"), "dead_code_root_kinds", "kotlin.constructor")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "handle", "JobRoute"), "dead_code_root_kinds", "kotlin.spring_request_mapping_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "exercisedByTestRunner", "FixtureTests"), "dead_code_root_kinds", "kotlin.junit_test_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "main", ""), "dead_code_root_kinds", "kotlin.main_function")
	if helper := parsertest.AssertFunctionByNameAndClass(t, got, "unusedCleanupCandidate", ""); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("unusedCleanupCandidate dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathKotlinKeepsPendingMultilineAnnotations(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "src/main/kotlin/example/Routes.kt")
	writeKotlinTestFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "status", "Routes"), "dead_code_root_kinds", "kotlin.spring_request_mapping_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "health", "Routes"), "dead_code_root_kinds", "kotlin.spring_request_mapping_method")
	if helper := parsertest.AssertFunctionByNameAndClass(t, got, "helper", "Routes"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("Routes.helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}
