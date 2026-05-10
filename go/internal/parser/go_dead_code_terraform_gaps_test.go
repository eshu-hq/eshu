package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoMarksGenericConstraintMethodRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "generic_constraints.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type UniqueKey interface {
	UniqueKey() string
}

type Address struct{}

func (a Address) UniqueKey() string { return "" }
func (a Address) unused() {}

type Box[T UniqueKey] struct {
	value T
}

func (b Box[T]) Key() string {
	return b.value.UniqueKey()
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

	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "interfaces", "UniqueKey"), "dead_code_root_kinds", "go.interface_type_reference")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "UniqueKey", "Address"), "dead_code_root_kinds", "go.generic_constraint_method")
	if classContext, _ := assertFunctionByNameAndClass(t, got, "Key", "Box")["class_context"].(string); classContext != "Box" {
		t.Fatalf("Box.Key class_context = %q, want Box", classContext)
	}
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "Address")["dead_code_root_kinds"]; ok {
		t.Fatalf("Address.unused dead_code_root_kinds present, want absent outside generic constraint")
	}
}

func TestDefaultEngineParsePathGoDoesNotRootTypeParameterNamesAsConstraints(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "generic_parameter_name.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type T interface {
	Mark() string
}

type Widget struct{}

func (w Widget) Mark() string { return "" }

type Box[T any] struct {
	value T
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

	if _, ok := assertFunctionByNameAndClass(t, got, "Mark", "Widget")["dead_code_root_kinds"]; ok {
		t.Fatalf("Widget.Mark dead_code_root_kinds present, want absent when T is a type parameter name")
	}
}

func TestDefaultEngineParsePathGoMarksPackageGenericConstraintMethodRoots(t *testing.T) {
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
	interfacePath := filepath.Join(addrsDir, "unique_key.go")
	genericPath := filepath.Join(addrsDir, "map.go")
	methodPath := filepath.Join(addrsDir, "module.go")
	writeTestFile(
		t,
		interfacePath,
		`package addrs

type UniqueKey interface{}

type UniqueKeyer interface {
	UniqueKey() UniqueKey
}
`,
	)
	writeTestFile(
		t,
		genericPath,
		`package addrs

type Map[K UniqueKeyer, V any] struct{}

func (m Map[K, V]) Has(key K) bool {
	_ = key.UniqueKey()
	return true
}
`,
	)
	writeTestFile(
		t,
		methodPath,
		`package addrs

type Module string

func (m Module) UniqueKey() UniqueKey { return nil }
func (m Module) unused() {}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	packageTargets, err := engine.PreScanGoPackageSemanticRoots(
		repoRoot,
		[]string{interfacePath, genericPath, methodPath},
	)
	if err != nil {
		t.Fatalf("PreScanGoPackageSemanticRoots() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, methodPath, false, Options{
		GoPackageImportPath:             "example.com/root/internal/addrs",
		GoDirectMethodCallRoots:         packageTargets[addrsDir].DirectMethodCallRoots,
		GoImportedInterfaceParamMethods: packageTargets[addrsDir].ImportedInterfaceParamMethods,
	})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "UniqueKey", "Module"), "dead_code_root_kinds", "go.generic_constraint_method")
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "Module")["dead_code_root_kinds"]; ok {
		t.Fatalf("Module.unused dead_code_root_kinds present, want absent outside package generic constraint")
	}
}

func TestDefaultEngineParsePathGoDoesNotRootUnknownReceiverByMethodName(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "unknown_receiver.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type Controller struct{}

func (c Controller) Build() {}

func run(x any) {
	x.Build()
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

	if _, ok := assertFunctionByNameAndClass(t, got, "Build", "Controller")["dead_code_root_kinds"]; ok {
		t.Fatalf("Controller.Build dead_code_root_kinds present, want absent for unknown receiver")
	}
}

func TestDefaultEngineParsePathGoScopesDirectMethodReceiverTypesByFunction(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "scoped_receivers.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type Controller struct{}
type Other struct{}

func (c Controller) Build() {}
func (o Other) Other() {}

func run(x Controller) {
	x.Build()
}

func skip(x Other) {
	x.Other()
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

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "Build", "Controller"), "dead_code_root_kinds", "go.direct_method_call")
}

func TestDefaultEngineParsePathGoMarksFmtStringerRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "fmt_stringer.go")
	writeTestFile(
		t,
		filePath,
		`package roots

import "fmt"

type Address struct{}
type Token struct{}
type Writer struct{}

func (a Address) String() string { return "" }
func (a Address) unused() {}
func (t Token) String() string { return "" }
func (w Writer) Write(_ []byte) (int, error) { return 0, nil }
func (w Writer) String() string { return "" }

func render(addr Address, w Writer) string {
	fmt.Fprintf(w, "address=%s", addr)
	fmt.Sprintf("address=%s", &addr)
	return fmt.Sprintf("address=%s", addr)
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

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "String", "Address"), "dead_code_root_kinds", "go.fmt_stringer_method")
	if _, ok := assertFunctionByNameAndClass(t, got, "String", "Token")["dead_code_root_kinds"]; ok {
		t.Fatalf("Token.String dead_code_root_kinds present, want absent for unformatted type")
	}
	if _, ok := assertFunctionByNameAndClass(t, got, "String", "Writer")["dead_code_root_kinds"]; ok {
		t.Fatalf("Writer.String dead_code_root_kinds present, want absent for fmt writer argument")
	}
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "Address")["dead_code_root_kinds"]; ok {
		t.Fatalf("Address.unused dead_code_root_kinds present, want absent outside fmt stringer")
	}
}

func TestDefaultEngineParsePathGoMarksImportedChainedReceiverMethodRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(
		t,
		filepath.Join(repoRoot, "go.mod"),
		`module example.com/root

go 1.24
`,
	)
	actionsDir := filepath.Join(repoRoot, "internal", "actions")
	terraformDir := filepath.Join(repoRoot, "internal", "terraform")
	actionsPath := filepath.Join(actionsDir, "actions.go")
	terraformPath := filepath.Join(terraformDir, "node.go")
	writeTestFile(
		t,
		actionsPath,
		`package actions

type Actions struct{}

func (a *Actions) GetActionInstance(name string) bool { return true }
func (a *Actions) unused() {}
`,
	)
	writeTestFile(
		t,
		terraformPath,
		`package terraform

import "example.com/root/internal/actions"

type EvalContext interface {
	Actions() *actions.Actions
}

func plan(ctx EvalContext) bool {
	return ctx.Actions().GetActionInstance("apply")
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	packageTargets, err := engine.PreScanGoPackageSemanticRoots(
		repoRoot,
		[]string{actionsPath, terraformPath},
	)
	if err != nil {
		t.Fatalf("PreScanGoPackageSemanticRoots() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, actionsPath, false, Options{
		GoPackageImportPath:             "example.com/root/internal/actions",
		GoDirectMethodCallRoots:         packageTargets[actionsDir].DirectMethodCallRoots,
		GoImportedInterfaceParamMethods: packageTargets[actionsDir].ImportedInterfaceParamMethods,
	})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "GetActionInstance", "Actions"), "dead_code_root_kinds", "go.imported_direct_method_call")
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "Actions")["dead_code_root_kinds"]; ok {
		t.Fatalf("Actions.unused dead_code_root_kinds present, want absent for uncalled chained receiver method")
	}
}

func TestDefaultEngineParsePathGoMarksPackageImportedChainedReceiverMethodRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(
		t,
		filepath.Join(repoRoot, "go.mod"),
		`module example.com/root

go 1.24
`,
	)
	actionsDir := filepath.Join(repoRoot, "internal", "actions")
	otherDir := filepath.Join(repoRoot, "internal", "other")
	terraformDir := filepath.Join(repoRoot, "internal", "terraform")
	actionsPath := filepath.Join(actionsDir, "actions.go")
	otherPath := filepath.Join(otherDir, "actions.go")
	contextPath := filepath.Join(terraformDir, "eval_context.go")
	nodePath := filepath.Join(terraformDir, "node.go")
	writeTestFile(
		t,
		actionsPath,
		`package actions

type Actions struct{}

func (a *Actions) GetActionInstance(name string) bool { return true }
func (a *Actions) unused() {}
`,
	)
	writeTestFile(
		t,
		otherPath,
		`package other

type Actions struct{}

func (a *Actions) Ignored(name string) bool { return true }
`,
	)
	writeTestFile(
		t,
		contextPath,
		`package terraform

import "example.com/root/internal/actions"
import otheractions "example.com/root/internal/other"

type EvalContext interface {
	Actions() *actions.Actions
}

type OtherContext interface {
	Actions() *otheractions.Actions
}
`,
	)
	writeTestFile(
		t,
		nodePath,
		`package terraform

func plan(ctx EvalContext) bool {
	return ctx.Actions().GetActionInstance("apply")
}

func other(ctx OtherContext) bool {
	return ctx.Actions().Ignored("skip")
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	packageTargets, err := engine.PreScanGoPackageSemanticRoots(
		repoRoot,
		[]string{actionsPath, otherPath, contextPath, nodePath},
	)
	if err != nil {
		t.Fatalf("PreScanGoPackageSemanticRoots() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, actionsPath, false, Options{
		GoPackageImportPath:             "example.com/root/internal/actions",
		GoDirectMethodCallRoots:         packageTargets[actionsDir].DirectMethodCallRoots,
		GoImportedInterfaceParamMethods: packageTargets[actionsDir].ImportedInterfaceParamMethods,
	})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "GetActionInstance", "Actions"), "dead_code_root_kinds", "go.imported_direct_method_call")
	if _, ok := assertFunctionByNameAndClass(t, got, "unused", "Actions")["dead_code_root_kinds"]; ok {
		t.Fatalf("Actions.unused dead_code_root_kinds present, want absent for package-level chained receiver method")
	}
}
