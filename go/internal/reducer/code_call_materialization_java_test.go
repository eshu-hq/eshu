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

func TestExtractCodeCallRowsResolvesJavaTypedMethodReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "build-plugin", "src", "main", "java", "example", "BootZipCopyAction.java")
	writeReducerTestFile(t, filePath, `package example;

public class BootZipCopyAction {
    void run(CopyActionProcessingStream copyActions) {
        Processor processor = new Processor();
        copyActions.process(processor::process);
    }

    private static final class Processor {
        void process(FileCopyDetails details) {
        }
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
	assignReducerTestFunctionUID(t, payload, "run", "content-entity:java-run")
	assignReducerTestFunctionUID(t, payload, "process", "content-entity:java-process")

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-java"},
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

	assertReducerCodeCallRow(t, rows, "content-entity:java-run", "content-entity:java-process")
}

func TestExtractCodeCallRowsResolvesJavaUnqualifiedSameClassCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "build-plugin", "src", "main", "java", "example", "JavaPluginAction.java")
	writeReducerTestFile(t, filePath, `package example;

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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, payload, "execute", "content-entity:java-execute")
	assignReducerTestDuplicateFunctionUIDs(
		t,
		payload,
		"configureAdditionalMetadataLocations",
		"content-entity:java-project-configurer",
		"content-entity:java-compile-configurer",
	)

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-java"},
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

	assertReducerCodeCallRow(t, rows, "content-entity:java-execute", "content-entity:java-compile-configurer")
	assertReducerNoCodeCallRow(t, rows, "content-entity:java-execute", "content-entity:java-project-configurer")
}

func TestExtractCodeCallRowsResolvesJavaEnhancedForReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "build-plugin", "src", "main", "java", "example", "ProtobufPluginAction.java")
	writeReducerTestFile(t, filePath, `package example;

public class ProtobufPluginAction {
    private VersionAlignment versionAlignmentFor(DependencyResolveDetails details) {
        for (VersionAlignment alignment : versionAlignment) {
            if (alignment.accepts(details)) {
                return alignment;
            }
        }
        return null;
    }

    private record VersionAlignment(Dependency target, Dependency source) {
        boolean accepts(DependencyResolveDetails details) {
            return true;
        }
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
	assignReducerTestFunctionUID(t, payload, "versionAlignmentFor", "content-entity:java-version-alignment-for")
	assignReducerTestFunctionUID(t, payload, "accepts", "content-entity:java-accepts")

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-java"},
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

	assertReducerCodeCallRow(t, rows, "content-entity:java-version-alignment-for", "content-entity:java-accepts")
}

func TestExtractCodeCallRowsResolvesJavaUnqualifiedOuterClassCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "build-plugin", "src", "main", "java", "example", "BootJar.java")
	writeReducerTestFile(t, filePath, `package example;

public class BootJar {
    protected ZipCompression resolveZipCompression(FileCopyDetails details) {
        return ZipCompression.STORED;
    }

    private final class ZipCompressionResolver implements Function<FileCopyDetails, ZipCompression> {
        public ZipCompression apply(FileCopyDetails details) {
            return resolveZipCompression(details);
        }
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
	assignReducerTestFunctionUID(t, payload, "apply", "content-entity:java-apply")
	assignReducerTestFunctionUID(t, payload, "resolveZipCompression", "content-entity:java-resolve-zip-compression")

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-java"},
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

	assertReducerCodeCallRow(t, rows, "content-entity:java-apply", "content-entity:java-resolve-zip-compression")
}
