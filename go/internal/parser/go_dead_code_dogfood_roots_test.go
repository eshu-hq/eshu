package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoEmitsDependencyInjectionCallbackRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "callbacks.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type openFn func() error
type applyFn func() error

func run(open openFn, apply applyFn, direct func() error) error {
	if err := open(); err != nil {
		return err
	}
	return apply()
}

func openBootstrapDB() error { return nil }
func applySchema() error { return nil }
func directlyCalled() error { return nil }
func unusedCallback() error { return nil }

func main() {
	_ = run(openBootstrapDB, applySchema, directlyCalled)
	_ = directlyCalled()
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

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "openBootstrapDB"), "dead_code_root_kinds", "go.dependency_injection_callback")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "applySchema"), "dead_code_root_kinds", "go.dependency_injection_callback")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "directlyCalled"), "dead_code_root_kinds", "go.dependency_injection_callback")
	if _, ok := assertFunctionByName(t, got, "unusedCallback")["dead_code_root_kinds"]; ok {
		t.Fatalf("unusedCallback dead_code_root_kinds present, want absent for unreferenced function")
	}
}

func TestDefaultEngineParsePathGoEmitsInterfaceRootsFromInterfaceReturn(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "interface_return.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type bootstrapDB interface {
	Close() error
}

type bootstrapSQLDB struct{}

func (b *bootstrapSQLDB) Close() error { return nil }
func (b *bootstrapSQLDB) unused() {}

func openBootstrapDB() bootstrapDB {
	return &bootstrapSQLDB{}
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

	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "interfaces", "bootstrapDB"), "dead_code_root_kinds", "go.interface_type_reference")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "structs", "bootstrapSQLDB"), "dead_code_root_kinds", "go.type_reference")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "structs", "bootstrapSQLDB"), "dead_code_root_kinds", "go.interface_implementation_type")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "Close", "bootstrapSQLDB"), "dead_code_root_kinds", "go.interface_method_implementation")
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "bootstrapSQLDB")["dead_code_root_kinds"]; ok {
		t.Fatalf("bootstrapSQLDB.unused dead_code_root_kinds present, want absent outside local interface")
	}
}

func TestDefaultEngineParsePathGoMarksImportedInterfaceReturnImplementations(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "imported_interface_return.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type closerImpl struct{}

func (c closerImpl) Close() error { return nil }
func (c closerImpl) unused() {}

func openCloser() io.Closer {
	return closerImpl{}
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

	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "structs", "closerImpl"), "dead_code_root_kinds", "go.interface_implementation_type")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "Close", "closerImpl"), "dead_code_root_kinds", "go.interface_method_implementation")
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "closerImpl")["dead_code_root_kinds"]; ok {
		t.Fatalf("closerImpl.unused dead_code_root_kinds present, want absent outside imported interface return")
	}
}

func TestDefaultEngineParsePathGoEmitsImportedInterfaceAssignmentRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "imported_interface.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type neo4jDeps struct {
	executor graph.CypherExecutor
}

type neo4jSchemaExecutor struct{}

func (e *neo4jSchemaExecutor) ExecuteCypher() error { return nil }

func openNeo4j() neo4jDeps {
	return neo4jDeps{
		executor: &neo4jSchemaExecutor{},
	}
}

func drain(workSource projector.ProjectorWorkSource) {}

type drainingWorkSource struct{}

func (d *drainingWorkSource) Claim() error { return nil }

func wire() {
	dws := &drainingWorkSource{}
	drain(dws)
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

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "ExecuteCypher", "neo4jSchemaExecutor"), "dead_code_root_kinds", "go.interface_method_implementation")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "structs", "neo4jDeps"), "dead_code_root_kinds", "go.type_reference")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "structs", "neo4jSchemaExecutor"), "dead_code_root_kinds", "go.type_reference")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "structs", "neo4jSchemaExecutor"), "dead_code_root_kinds", "go.interface_implementation_type")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "structs", "drainingWorkSource"), "dead_code_root_kinds", "go.type_reference")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "structs", "drainingWorkSource"), "dead_code_root_kinds", "go.interface_implementation_type")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "Claim", "drainingWorkSource"), "dead_code_root_kinds", "go.interface_method_implementation")
}

func TestDefaultEngineParsePathGoMarksDirectMethodCallRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "method_calls.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type BasicAuthCache struct{}

func (c *BasicAuthCache) GetFromHeader() {
	c.get()
}

func (c *BasicAuthCache) SetFromHeader() {
	c.set()
}

func (c *BasicAuthCache) get() {}
func (c *BasicAuthCache) set() {}

type tokenCache struct{}

func (c *tokenCache) get() {}
func (c *tokenCache) set() {}

type Server struct {
	config *Config
}

type Config struct{}

func (s *Server) Hello() string {
	return s.config.serverAnnouncement()
}

func (c *Config) serverAnnouncement() string { return "bolt" }
func (c *Config) unused() {}
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

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "get", "BasicAuthCache"), "dead_code_root_kinds", "go.direct_method_call")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "set", "BasicAuthCache"), "dead_code_root_kinds", "go.direct_method_call")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "serverAnnouncement", "Config"), "dead_code_root_kinds", "go.direct_method_call")
	if _, ok := assertFunctionByNameAndClass(t, got, "get", "tokenCache")["dead_code_root_kinds"]; ok {
		t.Fatalf("tokenCache.get dead_code_root_kinds present, want absent for uncalled same-name method")
	}
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "Config")["dead_code_root_kinds"]; ok {
		t.Fatalf("Config.unused dead_code_root_kinds present, want absent for uncalled method")
	}
}

func TestDefaultEngineParsePathGoMarksImportedPackageMethodCallRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(
		t,
		filepath.Join(repoRoot, "go.mod"),
		`module example.com/root

go 1.24
`,
	)
	addrsDir := filepath.Join(repoRoot, "internal", "addrs")
	refDir := filepath.Join(repoRoot, "internal", "ref")
	addrsPath := filepath.Join(addrsDir, "module.go")
	refPath := filepath.Join(refDir, "use.go")
	writeTestFile(
		t,
		addrsPath,
		`package addrs

type Module []string

func (m Module) String() string { return "" }
func (m Module) Child(name string) Module { return append(m, name) }
func (m Module) unused() {}
`,
	)
	writeTestFile(
		t,
		refPath,
		`package ref

import "example.com/root/internal/addrs"

func render(m addrs.Module) string {
	child := addrs.Module{}.Child("child")
	return m.String() + child.String()
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	packageTargets, err := engine.PreScanGoPackageSemanticRoots(
		repoRoot,
		[]string{addrsPath, refPath},
	)
	if err != nil {
		t.Fatalf("PreScanGoPackageSemanticRoots() error = %v, want nil", err)
	}
	if len(packageTargets[addrsDir].DirectMethodCallRoots) == 0 {
		t.Fatalf("DirectMethodCallRoots empty, want imported addrs method roots: %#v", packageTargets)
	}

	got, err := engine.ParsePath(repoRoot, addrsPath, false, Options{
		GoPackageImportPath:             "example.com/root/internal/addrs",
		GoDirectMethodCallRoots:         packageTargets[addrsDir].DirectMethodCallRoots,
		GoImportedInterfaceParamMethods: packageTargets[addrsDir].ImportedInterfaceParamMethods,
	})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "String", "Module"), "dead_code_root_kinds", "go.imported_direct_method_call")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "Child", "Module"), "dead_code_root_kinds", "go.imported_direct_method_call")
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "Module")["dead_code_root_kinds"]; ok {
		t.Fatalf("Module.unused dead_code_root_kinds present, want absent for uncalled imported-package method")
	}
}

func TestDefaultEngineParsePathGoMarksLocalInterfaceFieldReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "interface_field.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type bootstrapCommitter interface {
	Commit() error
}

type collectorDeps struct {
	committer bootstrapCommitter
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

	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "interfaces", "bootstrapCommitter"), "dead_code_root_kinds", "go.interface_type_reference")
}
