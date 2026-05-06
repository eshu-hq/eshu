package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJava(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "App.java")
	writeTestFile(
		t,
		filePath,
		`import java.util.List;

@Logged
public class App {
    private String name;

    public App(String name) {
        this.name = name;
    }

    public String greet() {
        System.out.println(name);
        return name;
    }
}

interface Runner {
    String run();
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "java" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "java")
	}

	assertNamedBucketContains(t, got, "classes", "App")
	assertNamedBucketContains(t, got, "interfaces", "Runner")
	assertNamedBucketContains(t, got, "functions", "greet")
	assertNamedBucketContains(t, got, "variables", "name")
	importItem := assertBucketItemByName(t, got, "imports", "java.util.List")
	assertStringFieldValue(t, importItem, "source", "java.util.List")
	assertStringFieldValue(t, importItem, "alias", "List")
	assertStringFieldValue(t, importItem, "full_import_name", "import java.util.List;")
	callItem := assertBucketItemByName(t, got, "function_calls", "println")
	assertStringFieldValue(t, callItem, "full_name", "System.out.println")
}

func TestDefaultEngineParsePathJavaAnnotationMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "java_comprehensive", "annotations")
	filePath := filepath.Join(repoRoot, "AnnotatedService.java")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "annotations", "Logged")
	assertNamedBucketContains(t, got, "functions", "process")
	assertNamedBucketContains(t, got, "functions", "cleanup")
}

func TestDefaultEngineParsePathJavaAnnotationUsageKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "AnnotatedService.java")
	writeTestFile(
		t,
		filePath,
		`package comprehensive;

@interface Logged {
    String value() default "";
}

@Logged("service")
public class AnnotatedService {

    @Logged("process")
    public String process(String input) {
        return input.toUpperCase();
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "annotations", "Logged")
	assertBucketContainsFieldValue(t, got, "annotations", "kind", "declaration")
	assertBucketContainsFieldValue(t, got, "annotations", "kind", "applied")
	assertBucketContainsFieldValue(t, got, "annotations", "target_kind", "class_declaration")
	assertBucketContainsFieldValue(t, got, "annotations", "target_kind", "method_declaration")
}

func TestDefaultEngineParsePathCSharp(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Service.cs")
	writeTestFile(
		t,
		filePath,
		`using System;

public interface IService {
    string Execute(string input);
}

public record Request(string Value);

public class Service : IService {
    public string Name { get; set; } = "default";

    public string Execute(string input) {
        Console.WriteLine(input);
        var request = new Request(input);
        return request.Value;
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "c_sharp" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "c_sharp")
	}

	assertNamedBucketContains(t, got, "classes", "Service")
	assertNamedBucketContains(t, got, "interfaces", "IService")
	assertNamedBucketContains(t, got, "records", "Request")
	assertNamedBucketContains(t, got, "properties", "Name")
	assertNamedBucketContains(t, got, "functions", "Execute")
	assertNamedBucketContains(t, got, "imports", "System")
	assertNamedBucketContains(t, got, "function_calls", "WriteLine")
	assertNamedBucketContains(t, got, "function_calls", "Request")
	assertBucketItemStringSliceContains(t, got, "classes", "Service", "IService")
}

func TestDefaultEngineParsePathCSharpLocalTypes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Domain.cs")
	writeTestFile(
		t,
		filePath,
		`using System;

public struct Color {
    public byte R;
    public byte G;
    public byte B;
}

public enum Status {
    Active,
    Disabled,
}

public class Runner {
    public int Execute(int input) {
        int Local(int value) {
            return value + 1;
        }

        Console.WriteLine(input);
        return Local(input);
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "structs", "Color")
	assertNamedBucketContains(t, got, "enums", "Status")
	assertNamedBucketContains(t, got, "functions", "Local")
	assertNamedBucketContains(t, got, "function_calls", "WriteLine")
	assertBucketContainsFieldValue(t, got, "functions", "class_context", "Runner")
}

func TestDefaultEngineParsePathScala(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Service.scala")
	writeTestFile(
		t,
		filePath,
		`import scala.collection.mutable

trait Runner {
  def run(): String
}

class Service(val name: String) extends Runner {
  def run(): String = {
    println(name)
    name
  }
}

object Bootstrap {
  val version = "1.0.0"

  def main(): String = {
    runApp()
    version
  }

  private def runApp(): Unit = {
    println(version)
  }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "scala" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "scala")
	}

	assertNamedBucketContains(t, got, "classes", "Service")
	assertNamedBucketContains(t, got, "classes", "Bootstrap")
	assertNamedBucketContains(t, got, "traits", "Runner")
	assertNamedBucketContains(t, got, "functions", "run")
	assertNamedBucketContains(t, got, "functions", "main")
	assertNamedBucketContains(t, got, "functions", "runApp")
	assertNamedBucketContains(t, got, "variables", "version")
	assertNamedBucketContains(t, got, "imports", "scala.collection.mutable")
	assertNamedBucketContains(t, got, "function_calls", "println")
	assertBucketContainsFieldValue(t, got, "functions", "class_context", "Service")
	assertBucketContainsFieldValue(t, got, "functions", "class_context", "Bootstrap")
}

func TestDefaultEngineParsePathKotlinSecondaryConstructors(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Secondary.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Greeter private constructor(private val prefix: String) {
    constructor() : this("hello")

    fun greet(name: String): String = "$prefix $name"
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "functions", "constructor")
	assertBucketContainsFieldValue(t, got, "functions", "class_context", "Greeter")
	assertBucketContainsFieldValue(t, got, "functions", "constructor_kind", "secondary")
}

func TestDefaultEngineParsePathKotlinImportMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Metadata.kt")
	writeTestFile(
		t,
		filePath,
		`package demo

import kotlin.collections.List
import demo.shared.Widget as SharedWidget

class Sample {
    fun run(items: List<String>): SharedWidget = SharedWidget()
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	listImport := assertBucketItemByName(t, got, "imports", "kotlin.collections.List")
	assertStringFieldValue(t, listImport, "source", "kotlin.collections.List")
	assertStringFieldValue(t, listImport, "alias", "List")
	assertStringFieldValue(t, listImport, "full_import_name", "import kotlin.collections.List")

	widgetImport := assertBucketItemByName(t, got, "imports", "demo.shared.Widget")
	assertStringFieldValue(t, widgetImport, "source", "demo.shared.Widget")
	assertStringFieldValue(t, widgetImport, "alias", "SharedWidget")
	assertStringFieldValue(t, widgetImport, "full_import_name", "import demo.shared.Widget as SharedWidget")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "SharedWidget")
}

func TestDefaultEnginePreScanPathsManagedOO(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	javaPath := filepath.Join(repoRoot, "App.java")
	csharpPath := filepath.Join(repoRoot, "Service.cs")
	scalaPath := filepath.Join(repoRoot, "Bootstrap.scala")

	writeTestFile(
		t,
		javaPath,
		`public class App {
    public String greet() {
        return "hi";
    }
}

interface Runner {}
`,
	)
	writeTestFile(
		t,
		csharpPath,
		`public interface IService {
    string Execute(string input);
}

public class Service : IService {
    public string Execute(string input) {
        return input;
    }
}
`,
	)
	writeTestFile(
		t,
		scalaPath,
		`trait Runner {
  def run(): String
}

object Bootstrap {
  def main(): String = "ready"
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanPaths([]string{javaPath, csharpPath, scalaPath})
	if err != nil {
		t.Fatalf("PreScanPaths() error = %v, want nil", err)
	}

	assertPrescanContains(t, got, "App", javaPath)
	assertPrescanContains(t, got, "Runner", javaPath)
	assertPrescanContains(t, got, "greet", javaPath)
	assertPrescanContains(t, got, "IService", csharpPath)
	assertPrescanContains(t, got, "Service", csharpPath)
	assertPrescanContains(t, got, "Execute", csharpPath)
	assertPrescanContains(t, got, "Runner", scalaPath)
	assertPrescanContains(t, got, "Bootstrap", scalaPath)
	assertPrescanContains(t, got, "main", scalaPath)
}
