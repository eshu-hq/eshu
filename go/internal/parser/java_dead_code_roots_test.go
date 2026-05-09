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
