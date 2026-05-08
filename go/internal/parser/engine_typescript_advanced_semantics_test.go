package parser

import "path/filepath"
import "testing"

func TestDefaultEngineParsePathTypeScriptCapturesAdvancedTypeSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "types.ts")
	writeTestFile(
		t,
		filePath,
		`namespace API {
  export type ReadonlyMap<T> = {
    readonly [K in keyof T]: T[K];
  };

  export type Response<T> = T extends string
    ? { ok: true; value: T }
    : { ok: false };
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

	namespace := findNamedBucketItem(t, got, "modules", "API")
	assertStringFieldValue(t, namespace, "module_kind", "namespace")

	mappedAlias := findNamedBucketItem(t, got, "type_aliases", "ReadonlyMap")
	assertStringFieldValue(t, mappedAlias, "type_alias_kind", "mapped_type")
	assertStringSliceFieldValue(t, mappedAlias, "type_parameters", []string{"T"})

	conditionalAlias := findNamedBucketItem(t, got, "type_aliases", "Response")
	assertStringFieldValue(t, conditionalAlias, "type_alias_kind", "conditional_type")
	assertStringSliceFieldValue(t, conditionalAlias, "type_parameters", []string{"T"})
}

func TestDefaultEngineParsePathTypeScriptEmitsTypeReferenceMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "client.ts")
	writeTestFile(
		t,
		filePath,
		`interface ClientOptions {
  apiKey: string;
}

type Completion = {
  text: string;
};

export class Client {
  constructor(options: ClientOptions) {
    this.options = options;
  }

  async complete(): Promise<Completion> {
    return { text: "" };
  }
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

	optionsRef := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "ClientOptions")
	assertStringFieldValue(t, optionsRef, "call_kind", "typescript.type_reference")
	completionRef := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "Completion")
	assertStringFieldValue(t, completionRef, "call_kind", "typescript.type_reference")
}

func TestDefaultEngineParsePathTypeScriptEmitsTypeAssertionReferenceMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "plugin.ts")
	writeTestFile(
		t,
		filePath,
		`interface AppConfig {
  services?: Record<string, string>;
}

interface ServerWithConfig {
  app?: AppConfig;
}

export function register(server: unknown) {
  const typed = server as ServerWithConfig;
  return typed.app as AppConfig | undefined;
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

	serverRef := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "ServerWithConfig")
	assertStringFieldValue(t, serverRef, "call_kind", "typescript.type_reference")
	configRef := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "AppConfig")
	assertStringFieldValue(t, configRef, "call_kind", "typescript.type_reference")
}

func TestDefaultEngineParsePathTypeScriptDoesNotEmitInterfaceMethodSignaturesAsFunctions(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "config.ts")
	writeTestFile(
		t,
		filePath,
		`export interface ConfigPlugin {
  get(key: string): string;
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

	assertNamedBucketContains(t, got, "interfaces", "ConfigPlugin")
	if functions := got["functions"].([]map[string]any); len(functions) != 0 {
		t.Fatalf("functions = %#v, want empty for interface method signatures", functions)
	}
}

func TestDefaultEngineParsePathTypeScriptCapturesDeclarationMerging(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "merge.ts")
	writeTestFile(
		t,
		filePath,
		`class Service {
  run() {
    return true;
  }
}

namespace Service {
  export const version = "1";
}

function buildLabel() {
  return "label";
}

namespace buildLabel {
  export const suffix = "!";
}

interface Response {
  ok: boolean;
}

interface Response {
  data: string;
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

	serviceClass := findNamedBucketItem(t, got, "classes", "Service")
	assertStringFieldValue(t, serviceClass, "declaration_merge_group", "Service")
	assertIntFieldValue(t, serviceClass, "declaration_merge_count", 2)
	assertStringSliceFieldValue(t, serviceClass, "declaration_merge_kinds", []string{"class", "namespace"})

	serviceNamespace := findNamedBucketItem(t, got, "modules", "Service")
	assertStringFieldValue(t, serviceNamespace, "declaration_merge_group", "Service")
	assertIntFieldValue(t, serviceNamespace, "declaration_merge_count", 2)
	assertStringSliceFieldValue(t, serviceNamespace, "declaration_merge_kinds", []string{"class", "namespace"})

	buildLabelFn := findNamedBucketItem(t, got, "functions", "buildLabel")
	assertStringFieldValue(t, buildLabelFn, "declaration_merge_group", "buildLabel")
	assertIntFieldValue(t, buildLabelFn, "declaration_merge_count", 2)
	assertStringSliceFieldValue(t, buildLabelFn, "declaration_merge_kinds", []string{"function", "namespace"})

	buildLabelNamespace := findNamedBucketItem(t, got, "modules", "buildLabel")
	assertStringFieldValue(t, buildLabelNamespace, "declaration_merge_group", "buildLabel")
	assertIntFieldValue(t, buildLabelNamespace, "declaration_merge_count", 2)
	assertStringSliceFieldValue(t, buildLabelNamespace, "declaration_merge_kinds", []string{"function", "namespace"})

	responseInterfaces := findAllNamedBucketItems(t, got, "interfaces", "Response")
	if len(responseInterfaces) != 2 {
		t.Fatalf("interfaces.Response entries = %#v, want 2 items", responseInterfaces)
	}
	for _, responseInterface := range responseInterfaces {
		assertStringFieldValue(t, responseInterface, "declaration_merge_group", "Response")
		assertIntFieldValue(t, responseInterface, "declaration_merge_count", 2)
		assertStringSliceFieldValue(t, responseInterface, "declaration_merge_kinds", []string{"interface"})
	}
}

func TestDefaultEngineParsePathTypeScriptCapturesGenericInterfaceTypeParameters(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "interfaces.ts")
	writeTestFile(
		t,
		filePath,
		`interface Box<T> {
  value: T;
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

	boxInterface := findNamedBucketItem(t, got, "interfaces", "Box")
	assertStringSliceFieldValue(t, boxInterface, "type_parameters", []string{"T"})
}

func TestDefaultEngineParsePathTypeScriptCapturesWrappedConditionalTypeSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "wrapped.ts")
	writeTestFile(
		t,
		filePath,
		`type Wrapped<T> = (T extends string ? { value: T } : { value: never });
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

	wrappedAlias := findNamedBucketItem(t, got, "type_aliases", "Wrapped")
	assertStringFieldValue(t, wrappedAlias, "type_alias_kind", "conditional_type")
	assertStringSliceFieldValue(t, wrappedAlias, "type_parameters", []string{"T"})
}
