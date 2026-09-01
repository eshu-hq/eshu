// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathJavaMarksSpringFrameworkRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/GreetingController.java")
	writeJavaTestFile(t, filePath, `package example;

import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.context.event.EventListener;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;
import jakarta.annotation.PostConstruct;

@RestController
class GreetingController {
    @GetMapping("/greeting")
    String greeting() {
        return "hi";
    }

    @Bean
    HttpClient httpClient() {
        return new HttpClient();
    }

    @EventListener
    void onReady(ApplicationReadyEvent event) {
    }

    @Scheduled(fixedDelay = 1000)
    void tick() {
    }

    @PostConstruct
    void init() {
    }

    private void helper() {
    }
}

@ConfigurationProperties("demo")
class DemoProperties {
    String getName() {
        return "demo";
    }
}

@Service
class WorkerService {
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "classes", "GreetingController"), "dead_code_root_kinds", "java.spring_component_class")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "classes", "DemoProperties"), "dead_code_root_kinds", "java.spring_configuration_properties_class")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "classes", "WorkerService"), "dead_code_root_kinds", "java.spring_component_class")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "greeting", "GreetingController"), "dead_code_root_kinds", "java.spring_request_mapping_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "httpClient", "GreetingController"), "dead_code_root_kinds", "java.spring_bean_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "onReady", "GreetingController"), "dead_code_root_kinds", "java.spring_event_listener_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "tick", "GreetingController"), "dead_code_root_kinds", "java.spring_scheduled_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "init", "GreetingController"), "dead_code_root_kinds", "java.lifecycle_callback_method")
	if _, ok := parsertest.AssertFunctionByNameAndClass(t, got, "helper", "GreetingController")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent")
	}
}

func TestDefaultEngineParsePathJavaMarksJUnitRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/test/java/example/GreetingControllerTests.java")
	writeJavaTestFile(t, filePath, `package example;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.RepeatedTest;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;

class GreetingControllerTests {
    @BeforeEach
    void beforeEach() {
    }

    @Test
    void greets() {
    }

    @ParameterizedTest
    void greetsManyTimes() {
    }

    @RepeatedTest(3)
    void repeats() {
    }

    @AfterEach
    void afterEach() {
    }

    private void helper() {
    }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "beforeEach"), "dead_code_root_kinds", "java.junit_lifecycle_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "greets"), "dead_code_root_kinds", "java.junit_test_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "greetsManyTimes"), "dead_code_root_kinds", "java.junit_test_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "repeats"), "dead_code_root_kinds", "java.junit_test_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "afterEach"), "dead_code_root_kinds", "java.junit_lifecycle_method")
	if _, ok := parsertest.AssertBucketItemByName(t, got, "functions", "helper")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent")
	}
}

func TestDefaultEngineParsePathJavaMarksJenkinsFrameworkRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/DemoBuilder.java")
	writeJavaTestFile(t, filePath, `package example;

import hudson.Extension;
import hudson.init.Initializer;
import org.jenkinsci.Symbol;
import org.kohsuke.stapler.interceptor.RequirePOST;

@Extension
@Symbol("demoDescriptor")
class DemoDescriptor {
    @Symbol("demo")
    String getDisplayName() {
        return "demo";
    }

    @Initializer(after = InitMilestone.JOB_LOADED)
    static void register() {
    }

    @DataBoundSetter
    public void setEnabled(boolean enabled) {
    }

    @RequirePOST
    public void doSave() {
    }

    private void helper() {
    }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "classes", "DemoDescriptor"), "dead_code_root_kinds", "java.jenkins_extension_class")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "classes", "DemoDescriptor"), "dead_code_root_kinds", "java.jenkins_symbol_class")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "getDisplayName"), "dead_code_root_kinds", "java.jenkins_symbol_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "register"), "dead_code_root_kinds", "java.jenkins_initializer_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "setEnabled"), "dead_code_root_kinds", "java.jenkins_databound_setter_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "doSave"), "dead_code_root_kinds", "java.stapler_web_method")
	if _, ok := parsertest.AssertBucketItemByName(t, got, "functions", "helper")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent")
	}
}
