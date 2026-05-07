package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoMarksPackageImportedInterfaceParameterImplementations(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "wiring.go")
	calleePath := filepath.Join(repoRoot, "nornicdb_wiring.go")
	writeTestFile(
		t,
		calleePath,
		`package roots

func bootstrapCanonicalExecutorForGraphBackend(rawExecutor sourcecypher.Executor) sourcecypher.Executor {
	return rawExecutor
}
`,
	)
	writeTestFile(
		t,
		callerPath,
		`package roots

type bootstrapNeo4jExecutor struct{}

func (e bootstrapNeo4jExecutor) Execute() error { return nil }
func (e bootstrapNeo4jExecutor) ExecuteGroup() error { return nil }
func (e bootstrapNeo4jExecutor) unused() {}

func wireAPI() {
	rawExecutor := bootstrapNeo4jExecutor{}
	_ = bootstrapCanonicalExecutorForGraphBackend(rawExecutor)
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	packageTargets, err := engine.PreScanGoPackageImportedInterfaceParamMethods(
		repoRoot,
		[]string{callerPath, calleePath},
	)
	if err != nil {
		t.Fatalf("PreScanGoPackageImportedInterfaceParamMethods() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, callerPath, false, Options{
		GoImportedInterfaceParamMethods: packageTargets[repoRoot],
	})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "structs", "bootstrapNeo4jExecutor"), "dead_code_root_kinds", "go.interface_implementation_type")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "Execute", "bootstrapNeo4jExecutor"), "dead_code_root_kinds", "go.interface_method_implementation")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "ExecuteGroup", "bootstrapNeo4jExecutor"), "dead_code_root_kinds", "go.interface_method_implementation")
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "bootstrapNeo4jExecutor")["dead_code_root_kinds"]; ok {
		t.Fatalf("bootstrapNeo4jExecutor.unused dead_code_root_kinds present, want absent outside the imported interface contract")
	}
}
