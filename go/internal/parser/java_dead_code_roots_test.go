package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/CLI.java")
	writeTestFile(t, filePath, `package example;

public class CLI implements AutoCloseable {
    public CLI(String url) {
        this.url = url;
    }

    public static void main(String[] args) {
        new CLI(args[0]).close();
    }

    @Override
    public void close() {
    }

    private void helper() {
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

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "CLI"), "dead_code_root_kinds", "java.constructor")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "main"), "dead_code_root_kinds", "java.main_method")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "close"), "dead_code_root_kinds", "java.override_method")
	if _, ok := assertFunctionByName(t, got, "helper")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent")
	}
}

func TestDefaultEngineParsePathJavaMarksAntTaskSettersAsRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/FindMainClass.java")
	writeTestFile(t, filePath, `package example;

import java.io.File;

import org.apache.tools.ant.Task;

public class FindMainClass extends Task {
    private File classesRoot;

    public void setClassesRoot(File classesRoot) {
        this.classesRoot = classesRoot;
    }

    public void setup(File classesRoot) {
        this.classesRoot = classesRoot;
    }

    private void helper() {
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

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "setClassesRoot"), "dead_code_root_kinds", "java.ant_task_setter")
	if _, ok := assertFunctionByName(t, got, "setup")["dead_code_root_kinds"]; ok {
		t.Fatalf("setup dead_code_root_kinds present, want absent")
	}
	if _, ok := assertFunctionByName(t, got, "helper")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent")
	}
}

func TestDefaultEngineParsePathJavaMarksGradleRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/BootPlugin.java")
	writeTestFile(t, filePath, `package example;

import org.gradle.api.Action;
import org.gradle.api.DefaultTask;
import org.gradle.api.Plugin;
import org.gradle.api.Project;
import org.gradle.api.provider.Property;
import org.gradle.api.tasks.Input;
import org.gradle.api.tasks.Internal;
import org.gradle.api.tasks.TaskAction;

public class BootPlugin implements Plugin<Project> {
    public void apply(Project project) {
    }

    public void helper(Project project) {
    }
}

public class BootExtension {
    public void buildInfo() {
    }

    public void buildInfo(Action<BuildInfo> action) {
    }

    private void configureBuildInfoTask(BuildInfo task) {
    }
}

public class DockerSpec {
    public void publishRegistry(Action<RegistrySpec> action) {
    }

    private void normalize() {
    }
}

public class BootBuildImage extends DefaultTask {
    @TaskAction
    public void buildImage() {
    }

    public void docker(Action<DockerSpec> action) {
    }

    private void helper() {
    }
}

public class InternalHelper {
    public void unused() {
    }
}

public abstract class BuildInfoProperties {
    @Internal
    public abstract Property<String> getGroup();

    @Input
    String getGroupIfNotExcluded() {
        return null;
    }

    public void ordinaryPublicMethod() {
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

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "apply", "BootPlugin"), "dead_code_root_kinds", "java.gradle_plugin_apply")
	if _, ok := assertFunctionByNameAndClass(t, got, "helper", "BootPlugin")["dead_code_root_kinds"]; ok {
		t.Fatalf("BootPlugin.helper dead_code_root_kinds present, want absent")
	}
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "buildInfo", "BootExtension"), "dead_code_root_kinds", "java.gradle_dsl_public_method")
	if _, ok := assertFunctionByNameAndClass(t, got, "configureBuildInfoTask", "BootExtension")["dead_code_root_kinds"]; ok {
		t.Fatalf("BootExtension.configureBuildInfoTask dead_code_root_kinds present, want absent")
	}
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "publishRegistry", "DockerSpec"), "dead_code_root_kinds", "java.gradle_dsl_public_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "buildImage", "BootBuildImage"), "dead_code_root_kinds", "java.gradle_task_action")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "docker", "BootBuildImage"), "dead_code_root_kinds", "java.gradle_dsl_public_method")
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "InternalHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("InternalHelper.unused dead_code_root_kinds present, want absent")
	}
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "getGroup", "BuildInfoProperties"), "dead_code_root_kinds", "java.gradle_task_property")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "getGroupIfNotExcluded", "BuildInfoProperties"), "dead_code_root_kinds", "java.gradle_task_property")
	if _, ok := assertFunctionByNameAndClass(t, got, "ordinaryPublicMethod", "BuildInfoProperties")["dead_code_root_kinds"]; ok {
		t.Fatalf("BuildInfoProperties.ordinaryPublicMethod dead_code_root_kinds present, want absent")
	}
}

func TestDefaultEngineParsePathJavaEmitsMethodReferenceCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/BootExtension.java")
	writeTestFile(t, filePath, `package example;

public class BootExtension {
    public void buildInfo() {
        tasks.register("bootBuildInfo", BuildInfo.class, this::configureBuildInfoTask);
        project.provider(this::determineArtifactBaseName);
    }

    private void configureBuildInfoTask(BuildInfo task) {
    }

    private String determineArtifactBaseName() {
        return "demo";
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

	configure := assertBucketItemByName(t, got, "function_calls", "configureBuildInfoTask")
	assertStringFieldValue(t, configure, "call_kind", "java.method_reference")
	assertStringFieldValue(t, configure, "full_name", "this.configureBuildInfoTask")
	assertStringFieldValue(t, configure, "class_context", "BootExtension")

	determine := assertBucketItemByName(t, got, "function_calls", "determineArtifactBaseName")
	assertStringFieldValue(t, determine, "call_kind", "java.method_reference")
	assertStringFieldValue(t, determine, "full_name", "this.determineArtifactBaseName")
	assertStringFieldValue(t, determine, "class_context", "BootExtension")
}

func TestDefaultEngineParsePathJavaEmitsTypedMethodReferenceMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/BootZipCopyAction.java")
	writeTestFile(t, filePath, `package example;

public class BootZipCopyAction {
    void run(CopyActionProcessingStream copyActions) {
        Processor processor = new Processor();
        copyActions.process(processor::process);
    }

    private static final class Processor {
        void process(FileCopyDetails details) {
        }

        void helper() {
        }
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

	processCall := assertJavaFunctionCallByNameAndKind(t, got, "process", "java.method_reference")
	assertStringFieldValue(t, processCall, "call_kind", "java.method_reference")
	assertStringFieldValue(t, processCall, "full_name", "processor.process")
	assertStringFieldValue(t, processCall, "inferred_obj_type", "Processor")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "process", "Processor"), "dead_code_root_kinds", "java.method_reference_target")
	if _, ok := assertFunctionByNameAndClass(t, got, "helper", "Processor")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent")
	}
}

func TestDefaultEngineParsePathJavaMarksDeclaredTypeMethodReferenceTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/ProcessorPipeline.java")
	writeTestFile(t, filePath, `package example;

import java.util.stream.Stream;

public class ProcessorPipeline {
    void run(Stream<Processor> processors) {
        processors.forEach(Processor::process);
        processors.forEach(MissingProcessor::process);
    }
}

final class Processor {
    void process() {
    }

    void helper() {
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

	processCall := assertJavaFunctionCallByNameAndKind(t, got, "process", "java.method_reference")
	assertStringFieldValue(t, processCall, "full_name", "Processor.process")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "process", "Processor"), "dead_code_root_kinds", "java.method_reference_target")
	if _, ok := assertFunctionByNameAndClass(t, got, "helper", "Processor")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent")
	}
}

func TestDefaultEngineParsePathJavaMarksInterfaceEnumAndRecordMethodReferenceTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/DeclaredTypes.java")
	writeTestFile(t, filePath, `package example;

import java.util.stream.Stream;

interface ProcessorContract {
    default void run(Stream<InterfaceProcessor> processors) {
        processors.forEach(InterfaceProcessor::process);
    }
}

interface InterfaceProcessor {
    void process();
}

enum EnumProcessor {
    INSTANCE;

    void process() {
    }
}

record RecordProcessor() {
    void process() {
    }
}

final class Pipeline {
    void run(Stream<EnumProcessor> enums, Stream<RecordProcessor> records) {
        enums.forEach(EnumProcessor::process);
        records.forEach(RecordProcessor::process);
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

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "process", "InterfaceProcessor"), "dead_code_root_kinds", "java.method_reference_target")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "process", "EnumProcessor"), "dead_code_root_kinds", "java.method_reference_target")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "process", "RecordProcessor"), "dead_code_root_kinds", "java.method_reference_target")
}

func TestDefaultEngineParsePathJavaDoesNotMarkDuplicateDeclaredTypeMethodReferenceTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/DuplicateTypes.java")
	writeTestFile(t, filePath, `package example;

import java.util.stream.Stream;

final class First {
    static final class Processor {
        void process() {
        }
    }
}

final class Second {
    static final class Processor {
        void process() {
        }
    }
}

final class Pipeline {
    void run(Stream<Processor> processors) {
        processors.forEach(Processor::process);
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

	for _, item := range javaFunctionsByNameAndClass(t, got, "process", "Processor") {
		if item["dead_code_root_kinds"] != nil {
			t.Fatalf("duplicate Processor.process dead_code_root_kinds = %#v, want nil", item["dead_code_root_kinds"])
		}
	}
}

func assertJavaFunctionCallByNameAndKind(t *testing.T, payload map[string]any, name string, kind string) map[string]any {
	t.Helper()

	items, ok := payload["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", payload["function_calls"])
	}
	for _, item := range items {
		itemName, _ := item["name"].(string)
		callKind, _ := item["call_kind"].(string)
		if itemName == name && callKind == kind {
			return item
		}
	}
	t.Fatalf("function_calls missing name %q with call_kind %q in %#v", name, kind, items)
	return nil
}

func javaFunctionsByNameAndClass(t *testing.T, payload map[string]any, name string, classContext string) []map[string]any {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	var matches []map[string]any
	for _, function := range functions {
		functionName, _ := function["name"].(string)
		functionClassContext, _ := function["class_context"].(string)
		if functionName == name && functionClassContext == classContext {
			matches = append(matches, function)
		}
	}
	if len(matches) == 0 {
		t.Fatalf("functions missing name %q with class_context %q in %#v", name, classContext, functions)
	}
	return matches
}

func TestDefaultEngineParsePathJavaInfersLocalReceiverTypes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/CLI.java")
	writeTestFile(t, filePath, `package example;

public class CLI {
    public static int run(String auth) {
        CLIConnectionFactory factory = new CLIConnectionFactory().noCertificateCheck(false);
        factory.basicAuth(auth);
        return 0;
    }
}

public class CLIConnectionFactory {
    public CLIConnectionFactory noCertificateCheck(boolean value) {
        return this;
    }

    public CLIConnectionFactory basicAuth(String userInfo) {
        return this;
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

	noCertificateCheck := assertBucketItemByName(t, got, "function_calls", "noCertificateCheck")
	assertStringFieldValue(t, noCertificateCheck, "inferred_obj_type", "CLIConnectionFactory")
	assertIntFieldValue(t, noCertificateCheck, "argument_count", 1)
	basicAuth := assertBucketItemByName(t, got, "function_calls", "basicAuth")
	assertStringFieldValue(t, basicAuth, "inferred_obj_type", "CLIConnectionFactory")
	assertIntFieldValue(t, basicAuth, "argument_count", 1)
	assertIntFieldValue(t, assertFunctionByName(t, got, "basicAuth"), "parameter_count", 1)
}

func TestDefaultEngineParsePathJavaAddsClassContextToUnqualifiedMethodCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/JavaPluginAction.java")
	writeTestFile(t, filePath, `package example;

public class JavaPluginAction {
    private void configureAdditionalMetadataLocations(Project project) {
    }

    private static final class AdditionalMetadataLocationsConfigurer implements Action<Task> {
        public void execute(Task task) {
            configureAdditionalMetadataLocations((JavaCompile) task);
        }

        private void configureAdditionalMetadataLocations(JavaCompile compile) {
        }
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

	call := assertBucketItemByName(t, got, "function_calls", "configureAdditionalMetadataLocations")
	assertStringFieldValue(t, call, "class_context", "AdditionalMetadataLocationsConfigurer")
}
