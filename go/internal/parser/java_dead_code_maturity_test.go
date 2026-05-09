package parser

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestDefaultEngineParsePathJavaInfersTypedLambdaCallbackCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/CyclonedxPluginAction.java")
	writeTestFile(t, filePath, `package example;

import org.gradle.api.Action;
import org.gradle.api.Project;
import org.gradle.api.Task;
import org.gradle.api.tasks.TaskProvider;

final class CyclonedxPluginAction {
    private void configureBootJarTask(Project project, TaskProvider<CyclonedxAggregateTask> taskProvider) {
        configureTask(project, "bootJar", BootJar.class,
                (bootJar) -> configureBootJarTask(bootJar, taskProvider));
    }

    private void configureBootJarTask(BootJar task, TaskProvider<CyclonedxAggregateTask> taskProvider) {
    }

    private <T extends Task> void configureTask(Project project, String name, Class<T> type, Action<T> action) {
    }
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	callback := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "configureBootJarTask")
	assertParserStringSliceEquals(t, callback, "argument_types", []string{"BootJar", "TaskProvider"})
	assertParserStringSliceEquals(t, assertFunctionByNameAndClass(t, got, "configureBootJarTask", "CyclonedxPluginAction"), "parameter_types", []string{"Project", "TaskProvider"})
}

func TestDefaultEngineParsePathJavaMarksMethodReferenceTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/JavaPluginAction.java")
	writeTestFile(t, filePath, `package example;

import org.gradle.api.Project;
import org.gradle.api.tasks.compile.JavaCompile;

final class JavaPluginAction {
    private void configureUtf8Encoding(Project project) {
        project.getTasks().withType(JavaCompile.class).configureEach(this::configureUtf8Encoding);
    }

    private void configureUtf8Encoding(JavaCompile compile) {
    }

    private void unused(JavaCompile compile) {
    }
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "configureUtf8Encoding", "JavaPluginAction"), "dead_code_root_kinds", "java.method_reference_target")
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "JavaPluginAction")["dead_code_root_kinds"]; ok {
		t.Fatalf("unused dead_code_root_kinds present, want absent")
	}
}

func TestDefaultEngineParsePathJavaModelsRecordsAndThisFieldReceivers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/ProtobufPluginAction.java")
	writeTestFile(t, filePath, `package example;

final class ProtobufPluginAction {
    private final Dependency protocDependency = new Dependency("com.google.protobuf", "protoc");
    private final SinglePublishedArtifact singlePublishedArtifact;

    void configure(Project project, TaskProvider<BootJar> bootJar) {
        protocDependency.asDependencySpec();
        this.singlePublishedArtifact.addJarCandidate(bootJar);
    }

    private record Dependency(String group, String module) {
        private String asDependencySpec() {
            return this.group + ":" + this.module;
        }
    }
}

final class SinglePublishedArtifact {
    void addJarCandidate(TaskProvider<BootJar> candidate) {
    }
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "classes", "Dependency")
	assertStringFieldValue(t, assertFunctionByNameAndClass(t, got, "asDependencySpec", "Dependency"), "class_context", "Dependency")
	assertStringFieldValue(t, assertBucketItemByName(t, got, "function_calls", "asDependencySpec"), "inferred_obj_type", "Dependency")
	assertStringFieldValue(t, assertBucketItemByName(t, got, "function_calls", "addJarCandidate"), "inferred_obj_type", "SinglePublishedArtifact")
}

func TestDefaultEngineParsePathJavaMarksGradleTaskSettersAndInterfaceMethods(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/BootArchive.java")
	writeTestFile(t, filePath, `package example;

import org.gradle.api.Task;
import org.gradle.api.tasks.JavaExec;

public interface BootArchive extends Task {
    void classpath(Object... classpath);
    void setClasspath(FileCollection classpath);
}

public abstract class ProcessTestAot extends JavaExec {
    public void setClasspathRoots(FileCollection classpathRoots) {
    }

    void helper(FileCollection classpathRoots) {
    }
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "classpath", "BootArchive"), "dead_code_root_kinds", "java.gradle_task_interface_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "setClasspath", "BootArchive"), "dead_code_root_kinds", "java.gradle_task_interface_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "setClasspathRoots", "ProcessTestAot"), "dead_code_root_kinds", "java.gradle_task_setter")
	if _, ok := assertFunctionByNameAndClass(t, got, "helper", "ProcessTestAot")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent")
	}
}

func TestDefaultEngineParsePathJavaIgnoresParameterAnnotationsAsDecorators(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/BootArchiveSupport.java")
	writeTestFile(t, filePath, `package example;

class BootArchiveSupport {
    void configureManifest(Manifest manifest, String mainClass,
            @Nullable String classPathIndex, @Nullable Object implementationVersion) {
    }

    @TaskAction
    void action() {
    }
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if decorators, ok := assertFunctionByName(t, got, "configureManifest")["decorators"]; ok {
		t.Fatalf("configureManifest decorators = %#v, want absent", decorators)
	}
	assertParserStringSliceEquals(t, assertFunctionByName(t, got, "action"), "decorators", []string{"@TaskAction"})
}

func assertParserStringSliceEquals(t *testing.T, item map[string]any, field string, want []string) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", field, item[field])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}
