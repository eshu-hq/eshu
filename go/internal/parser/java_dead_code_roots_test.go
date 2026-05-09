package parser

import (
	"path/filepath"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
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

func TestJavaCallInferenceIndexResolvesReceiversFromSinglePass(t *testing.T) {
	t.Parallel()

	source := []byte(`package example;

public class SearchController {
    private SearchService fieldService;

    public void handle(SearchService parameterService) {
        SearchService localService = new SearchService();
        localService.search();
        parameterService.search();
        fieldService.search();
    }
}
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	parser, err := engine.runtime.Parser("java")
	if err != nil {
		t.Fatalf("Parser(java) error = %v, want nil", err)
	}
	defer parser.Close()
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("Parse() returned nil tree")
	}
	defer tree.Close()

	index := buildJavaCallInferenceIndex(tree.RootNode(), source)
	got := map[string]string{}
	walkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if node.Kind() != "method_invocation" {
			return
		}
		fullName := javaCallFullName(node, source)
		if fullName == "" {
			return
		}
		got[fullName] = javaCallInferredObjectType(node, source, index)
	})

	want := map[string]string{
		"localService.search":     "SearchService",
		"parameterService.search": "SearchService",
		"fieldService.search":     "SearchService",
	}
	if len(got) != len(want) {
		t.Fatalf("inferred calls = %#v, want %#v", got, want)
	}
	for call, wantType := range want {
		if gotType := got[call]; gotType != wantType {
			t.Fatalf("inferred type for %s = %q, want %q; all calls %#v", call, gotType, wantType, got)
		}
	}
}
