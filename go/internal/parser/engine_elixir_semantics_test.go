package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathElixirModuleKindsAndFunctionKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "parity.ex")
	writeTestFile(
		t,
		filePath,
		`defmodule Demo.Worker do
  def greet(name), do: name
end

defprotocol Demo.Serializable do
  def serialize(data)
end

defimpl Demo.Serializable, for: Demo.Worker do
  def serialize(worker), do: worker
end
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "modules", "Demo.Worker")
	assertNamedBucketContains(t, got, "protocols", "Demo.Serializable")
	assertBucketContainsFieldValue(t, got, "modules", "type", "defmodule")
	assertBucketContainsFieldValue(t, got, "modules", "type", "defimpl")
	assertBucketContainsFieldValue(t, got, "modules", "module_kind", "module")
	assertBucketContainsFieldValue(t, got, "modules", "module_kind", "protocol_implementation")
	assertBucketContainsFieldValue(t, got, "modules", "protocol", "Demo.Serializable")
	assertBucketContainsFieldValue(t, got, "modules", "implemented_for", "Demo.Worker")
	assertBucketContainsFieldValue(t, got, "protocols", "type", "defprotocol")
	assertBucketContainsFieldValue(t, got, "protocols", "module_kind", "protocol")
	assertBucketContainsFieldValue(t, got, "functions", "type", "def")
}

func TestDefaultEngineParsePathElixirFunctionMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "macros.ex")
	writeTestFile(
		t,
		filePath,
		`defmodule Demo.Macros do
  @doc "Macro docs."
  defmacro expand(expr) do
    expr
  end

  defmacrop reduce(expr), do: expr

  defdelegate size(values), to: Enum

  defguard is_even(value) when rem(value, 2) == 0
end
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if functions, ok := got["functions"].([]map[string]any); !ok {
		t.Fatalf("functions = %T, want []map[string]any", got["functions"])
	} else if len(functions) != 4 {
		t.Fatalf("functions = %#v, want 4 entries", functions)
	}

	expand := assertBucketItemByName(t, got, "functions", "expand")
	assertStringFieldValue(t, expand, "type", "defmacro")
	assertStringFieldValue(t, expand, "semantic_kind", "macro")
	assertStringFieldValue(t, expand, "visibility", "public")
	assertStringFieldValue(t, expand, "class_context", "Demo.Macros")
	assertStringFieldValue(t, expand, "docstring", `@doc "Macro docs."`)
	assertStringSliceFieldValue(t, expand, "args", []string{"expr"})

	reduce := assertBucketItemByName(t, got, "functions", "reduce")
	assertStringFieldValue(t, reduce, "type", "defmacrop")
	assertStringFieldValue(t, reduce, "semantic_kind", "macro")
	assertStringFieldValue(t, reduce, "visibility", "private")
	assertStringFieldValue(t, reduce, "class_context", "Demo.Macros")
	assertStringSliceFieldValue(t, reduce, "args", []string{"expr"})

	size := assertBucketItemByName(t, got, "functions", "size")
	assertStringFieldValue(t, size, "type", "defdelegate")
	assertStringFieldValue(t, size, "semantic_kind", "delegate")
	assertStringFieldValue(t, size, "visibility", "public")
	assertStringFieldValue(t, size, "class_context", "Demo.Macros")
	assertStringSliceFieldValue(t, size, "args", []string{"values"})

	isEven := assertBucketItemByName(t, got, "functions", "is_even")
	assertStringFieldValue(t, isEven, "type", "defguard")
	assertStringFieldValue(t, isEven, "semantic_kind", "guard")
	assertStringFieldValue(t, isEven, "visibility", "public")
	assertStringFieldValue(t, isEven, "class_context", "Demo.Macros")
	assertStringSliceFieldValue(t, isEven, "args", []string{"value"})
}

func TestDefaultEngineParsePathElixirImportAndCallMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "imports_and_calls.ex")
	writeTestFile(
		t,
		filePath,
		`defmodule Demo.Worker do
  use GenServer
  alias Demo.Repo
  import Demo.Patterns, only: [classify: 1]
  require Logger

  def start(user) do
    Logger.info("starting")
    Demo.Basic.greet(user)
    classify(user)
  end
end
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if imports, ok := got["imports"].([]map[string]any); !ok {
		t.Fatalf("imports = %T, want []map[string]any", got["imports"])
	} else if len(imports) != 4 {
		t.Fatalf("imports = %#v, want 4 entries", imports)
	}

	genServer := assertBucketItemByName(t, got, "imports", "GenServer")
	assertStringFieldValue(t, genServer, "import_type", "use")
	assertStringFieldValue(t, genServer, "full_import_name", "use GenServer")

	repo := assertBucketItemByName(t, got, "imports", "Demo.Repo")
	assertStringFieldValue(t, repo, "import_type", "alias")
	assertStringFieldValue(t, repo, "alias", "Repo")
	assertStringFieldValue(t, repo, "full_import_name", "alias Demo.Repo")

	patterns := assertBucketItemByName(t, got, "imports", "Demo.Patterns")
	assertStringFieldValue(t, patterns, "import_type", "import")
	assertStringFieldValue(t, patterns, "full_import_name", "import Demo.Patterns")

	logger := assertBucketItemByName(t, got, "imports", "Logger")
	assertStringFieldValue(t, logger, "import_type", "require")
	assertStringFieldValue(t, logger, "full_import_name", "require Logger")

	info := assertBucketItemByName(t, got, "function_calls", "info")
	assertStringFieldValue(t, info, "full_name", "Logger.info")
	assertStringSliceFieldValue(t, info, "args", []string{`"starting"`})
	assertStringFieldValue(t, info, "inferred_obj_type", "Logger")
	assertStringFieldValue(t, info, "class_context", "Demo.Worker")

	greet := assertBucketItemByName(t, got, "function_calls", "greet")
	assertStringFieldValue(t, greet, "full_name", "Demo.Basic.greet")
	assertStringSliceFieldValue(t, greet, "args", []string{"user"})
	assertStringFieldValue(t, greet, "inferred_obj_type", "Demo.Basic")
	assertStringFieldValue(t, greet, "class_context", "Demo.Worker")

	classify := assertBucketItemByName(t, got, "function_calls", "classify")
	assertStringSliceFieldValue(t, classify, "args", []string{"user"})
	assertStringFieldValue(t, classify, "class_context", "Demo.Worker")
	assertStringFieldValue(t, classify, "name", "classify")

	assertBucketMissingName(t, got, "function_calls", "start")
}

func TestDefaultEngineParsePathElixirAliasBraceExpansionAndGuardCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "brace_and_guard.ex")
	writeTestFile(
		t,
		filePath,
		`defmodule Demo.Braces do
  alias Demo.{Basic, Worker, User}

  defguard is_even(value) when rem(value, 2) == 0
end
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if imports, ok := got["imports"].([]map[string]any); !ok {
		t.Fatalf("imports = %T, want []map[string]any", got["imports"])
	} else if len(imports) != 3 {
		t.Fatalf("imports = %#v, want 3 entries", imports)
	}

	basic := assertBucketItemByName(t, got, "imports", "Demo.Basic")
	assertStringFieldValue(t, basic, "import_type", "alias")
	assertStringFieldValue(t, basic, "alias", "Basic")
	assertStringFieldValue(t, basic, "full_import_name", "alias Demo.Basic")

	worker := assertBucketItemByName(t, got, "imports", "Demo.Worker")
	assertStringFieldValue(t, worker, "import_type", "alias")
	assertStringFieldValue(t, worker, "alias", "Worker")
	assertStringFieldValue(t, worker, "full_import_name", "alias Demo.Worker")

	user := assertBucketItemByName(t, got, "imports", "Demo.User")
	assertStringFieldValue(t, user, "import_type", "alias")
	assertStringFieldValue(t, user, "alias", "User")
	assertStringFieldValue(t, user, "full_import_name", "alias Demo.User")

	assertBucketContainsFieldValue(t, got, "functions", "name", "is_even")
	assertBucketContainsFieldValue(t, got, "function_calls", "name", "rem")
}

func TestDefaultEngineParsePathElixirEmitsModuleAttributes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "attributes.ex")
	writeTestFile(
		t,
		filePath,
		`defmodule Demo.Attributes do
  @timeout 5_000
  @service_name "worker"

  def run, do: :ok
end
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	timeout := assertBucketItemByName(t, got, "variables", "@timeout")
	assertStringFieldValue(t, timeout, "class_context", "Demo.Attributes")
	assertStringFieldValue(t, timeout, "context_type", "module")
	assertStringFieldValue(t, timeout, "attribute_kind", "module_attribute")
	assertStringFieldValue(t, timeout, "value", "5_000")

	serviceName := assertBucketItemByName(t, got, "variables", "@service_name")
	assertStringFieldValue(t, serviceName, "class_context", "Demo.Attributes")
	assertStringFieldValue(t, serviceName, "context_type", "module")
	assertStringFieldValue(t, serviceName, "attribute_kind", "module_attribute")
	assertStringFieldValue(t, serviceName, "value", `"worker"`)
}

func TestDefaultEngineParsePathElixirMultilineSourceSpans(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "source_spans.ex")
	writeTestFile(
		t,
		filePath,
		`defmodule Demo.Source do
  @moduledoc "Source docs."

  @doc "Render docs."
  def render(value) do
    value
  end
end
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	module := assertBucketItemByName(t, got, "modules", "Demo.Source")
	assertIntFieldValue(t, module, "line_number", 1)
	assertIntFieldValue(t, module, "end_line", 8)
	assertStringFieldValue(
		t,
		module,
		"source",
		`defmodule Demo.Source do
  @moduledoc "Source docs."

  @doc "Render docs."
  def render(value) do
    value
  end
end`,
	)

	render := assertBucketItemByName(t, got, "functions", "render")
	assertIntFieldValue(t, render, "line_number", 5)
	assertIntFieldValue(t, render, "end_line", 7)
	assertStringFieldValue(
		t,
		render,
		"source",
		`  def render(value) do
    value
  end`,
	)
	assertStringFieldValue(t, render, "docstring", `@doc "Render docs."`)
}

func assertStringSliceFieldValue(
	t *testing.T,
	item map[string]any,
	field string,
	want []string,
) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", field, item[field])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

func assertBucketMissingName(t *testing.T, payload map[string]any, key string, name string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		itemName, _ := item["name"].(string)
		if itemName == name {
			t.Fatalf("%s unexpectedly contains name %q in %#v", key, name, items)
		}
	}
}
