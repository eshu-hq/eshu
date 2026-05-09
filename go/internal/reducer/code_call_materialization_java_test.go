package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesJavaSameFileMethodCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "cli", "src", "main", "java", "hudson", "cli", "CLI.java")
	writeReducerTestFile(t, filePath, `package hudson.cli;

public class CLI {
    public static void main(String[] args) {
        System.exit(_main(args));
    }

    static int _main(String[] args) {
        printUsage(computeVersion());
        return 0;
    }

    static String computeVersion() {
        return "dev";
    }

    static void printUsage(String version) {
    }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, payload, "main", "content-entity:java-main")
	assignReducerTestFunctionUID(t, payload, "_main", "content-entity:java-_main")
	assignReducerTestFunctionUID(t, payload, "computeVersion", "content-entity:java-compute-version")
	assignReducerTestFunctionUID(t, payload, "printUsage", "content-entity:java-print-usage")

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-java",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-java",
				"relative_path":    reducerTestRelativePath(t, repoRoot, filePath),
				"parsed_file_data": payload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "content-entity:java-main", "content-entity:java-_main")
	assertReducerCodeCallRow(t, rows, "content-entity:java-_main", "content-entity:java-compute-version")
	assertReducerCodeCallRow(t, rows, "content-entity:java-_main", "content-entity:java-print-usage")
}

func TestExtractCodeCallRowsResolvesJavaReceiverCallsUsingInferredType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "cli", "src", "main", "java", "hudson", "cli", "CLI.java")
	calleePath := filepath.Join(repoRoot, "cli", "src", "main", "java", "hudson", "cli", "CLIConnectionFactory.java")
	writeReducerTestFile(t, callerPath, `package hudson.cli;

public class CLI {
    public static int _main(String[] args) {
        CLIConnectionFactory factory = new CLIConnectionFactory().noCertificateCheck(false);
        factory.basicAuth("user:token");
        return 0;
    }
}
`)
	writeReducerTestFile(t, calleePath, `package hudson.cli;

public class CLIConnectionFactory {
    public CLIConnectionFactory noCertificateCheck(boolean value) {
        return this;
    }

    public CLIConnectionFactory basicAuth(String username, String password) {
        return authorization("Basic " + username + ":" + password);
    }

    public CLIConnectionFactory basicAuth(String userInfo) {
        return authorization("Basic " + userInfo);
    }

    public CLIConnectionFactory authorization(String value) {
        return this;
    }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{callerPath, calleePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(caller) error = %v, want nil", err)
	}
	calleePayload, err := engine.ParsePath(repoRoot, calleePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(callee) error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, callerPayload, "_main", "content-entity:java-_main")
	assignReducerTestFunctionUID(t, calleePayload, "noCertificateCheck", "content-entity:java-no-cert")
	assignReducerTestDuplicateFunctionUIDs(
		t,
		calleePayload,
		"basicAuth",
		"content-entity:java-basic-auth-two-arg",
		"content-entity:java-basic-auth-one-arg",
	)
	assignReducerTestFunctionUID(t, calleePayload, "authorization", "content-entity:java-authorization")

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-java",
				"imports_map": importsMap,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-java",
				"relative_path":    reducerTestRelativePath(t, repoRoot, callerPath),
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-java",
				"relative_path":    reducerTestRelativePath(t, repoRoot, calleePath),
				"parsed_file_data": calleePayload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "content-entity:java-_main", "content-entity:java-no-cert")
	assertReducerCodeCallRow(t, rows, "content-entity:java-_main", "content-entity:java-basic-auth-one-arg")
	assertReducerNoCodeCallRow(t, rows, "content-entity:java-_main", "content-entity:java-basic-auth-two-arg")
	assertReducerCodeCallRow(t, rows, "content-entity:java-basic-auth-one-arg", "content-entity:java-authorization")
}
