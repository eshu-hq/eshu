package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "src/main/scala/example/App.scala")
	writeTestFile(
		t,
		sourcePath,
		`package example

import akka.actor.Actor
import jakarta.annotation.PostConstruct
import org.junit.Test
import org.scalatest.funsuite.AnyFunSuite
import play.api.mvc.InjectedController

trait Runner {
  def run(): String
}

class Worker extends Runner {
  override def run(): String = "ok"

  private def helper(): String = "unused"
}

class HealthController extends InjectedController {
  def status = Action { "ok" }

  private def helper(): String = "unused"
}

class WorkerActor extends Actor {
  override def receive: Receive = {
    case "run" => sender() ! "ok"
  }
}

class Lifecycle {
  @PostConstruct
  def init(): Unit = {}
}

class ServiceTests extends AnyFunSuite {
  @Test
  def runsFromJUnit(): Unit = {}
}

object ConsoleApp extends App {
  println("ready")
}

object Main {
  def main(args: Array[String]): Unit = {}
}

private object UnusedHelpers {
  def unusedCleanupCandidate(): Int = 42
}
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

	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "traits", "Runner"), "dead_code_root_kinds", "scala.trait_type")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Runner"), "dead_code_root_kinds", "scala.trait_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Worker"), "dead_code_root_kinds", "scala.override_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Worker"), "dead_code_root_kinds", "scala.trait_implementation_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "status", "HealthController"), "dead_code_root_kinds", "scala.play_controller_action")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "receive", "WorkerActor"), "dead_code_root_kinds", "scala.akka_actor_receive")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "init", "Lifecycle"), "dead_code_root_kinds", "scala.lifecycle_callback_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "runsFromJUnit", "ServiceTests"), "dead_code_root_kinds", "scala.junit_test_method")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "classes", "ServiceTests"), "dead_code_root_kinds", "scala.scalatest_suite_class")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "classes", "ConsoleApp"), "dead_code_root_kinds", "scala.app_object")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "main", "Main"), "dead_code_root_kinds", "scala.main_method")

	if helper := assertFunctionByNameAndClass(t, got, "helper", "Worker"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("Worker.helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
	if helper := assertFunctionByNameAndClass(t, got, "helper", "HealthController"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("HealthController.helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
	if helper := assertFunctionByNameAndClass(t, got, "unusedCleanupCandidate", "UnusedHelpers"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("UnusedHelpers.unusedCleanupCandidate dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathScalaDeadCodeFixtureExpectedRoots(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("deadcode", "scala")
	sourcePath := repoFixturePath("deadcode", "scala", "Fixture.scala")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "traits", "Task"), "dead_code_root_kinds", "scala.trait_type")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Task"), "dead_code_root_kinds", "scala.trait_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "DefaultTask"), "dead_code_root_kinds", "scala.trait_implementation_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "handle", "JobEndpoint"), "dead_code_root_kinds", "scala.play_controller_action")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "receive", "WorkerActor"), "dead_code_root_kinds", "scala.akka_actor_receive")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "exercisedByTestRunner", "FixtureSuite"), "dead_code_root_kinds", "scala.junit_test_method")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "classes", "FixtureSuite"), "dead_code_root_kinds", "scala.scalatest_suite_class")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "classes", "ScriptMain"), "dead_code_root_kinds", "scala.app_object")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "main", "Main"), "dead_code_root_kinds", "scala.main_method")
	if helper := assertFunctionByNameAndClass(t, got, "unusedCleanupCandidate", "Helpers"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("Helpers.unusedCleanupCandidate dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}
