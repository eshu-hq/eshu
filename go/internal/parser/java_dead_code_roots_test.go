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
