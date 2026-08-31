// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elixir_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathElixirModuleKindsAndFunctionKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "parity.ex")
	writeElixirTestFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertNamedBucketContains(t, got, "modules", "Demo.Worker")
	parsertest.AssertNamedBucketContains(t, got, "protocols", "Demo.Serializable")
	parsertest.AssertBucketContainsFieldValue(t, got, "modules", "type", "defmodule")
	parsertest.AssertBucketContainsFieldValue(t, got, "modules", "type", "defimpl")
	parsertest.AssertBucketContainsFieldValue(t, got, "modules", "module_kind", "module")
	parsertest.AssertBucketContainsFieldValue(t, got, "modules", "module_kind", "protocol_implementation")
	parsertest.AssertBucketContainsFieldValue(t, got, "modules", "protocol", "Demo.Serializable")
	parsertest.AssertBucketContainsFieldValue(t, got, "modules", "implemented_for", "Demo.Worker")
	parsertest.AssertBucketContainsFieldValue(t, got, "protocols", "type", "defprotocol")
	parsertest.AssertBucketContainsFieldValue(t, got, "protocols", "module_kind", "protocol")
	parsertest.AssertBucketContainsFieldValue(t, got, "functions", "type", "def")
}

func TestDefaultEngineParsePathElixirFunctionMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "macros.ex")
	writeElixirTestFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if functions, ok := got["functions"].([]map[string]any); !ok {
		t.Fatalf("functions = %T, want []map[string]any", got["functions"])
	} else if len(functions) != 4 {
		t.Fatalf("functions = %#v, want 4 entries", functions)
	}

	expand := parsertest.AssertBucketItemByName(t, got, "functions", "expand")
	assertStringFieldValue(t, expand, "type", "defmacro")
	assertStringFieldValue(t, expand, "semantic_kind", "macro")
	assertStringFieldValue(t, expand, "visibility", "public")
	assertStringFieldValue(t, expand, "class_context", "Demo.Macros")
	assertStringFieldValue(t, expand, "docstring", `@doc "Macro docs."`)
	assertStringSliceFieldValue(t, expand, "args", []string{"expr"})

	reduce := parsertest.AssertBucketItemByName(t, got, "functions", "reduce")
	assertStringFieldValue(t, reduce, "type", "defmacrop")
	assertStringFieldValue(t, reduce, "semantic_kind", "macro")
	assertStringFieldValue(t, reduce, "visibility", "private")
	assertStringFieldValue(t, reduce, "class_context", "Demo.Macros")
	assertStringSliceFieldValue(t, reduce, "args", []string{"expr"})

	size := parsertest.AssertBucketItemByName(t, got, "functions", "size")
	assertStringFieldValue(t, size, "type", "defdelegate")
	assertStringFieldValue(t, size, "semantic_kind", "delegate")
	assertStringFieldValue(t, size, "visibility", "public")
	assertStringFieldValue(t, size, "class_context", "Demo.Macros")
	assertStringSliceFieldValue(t, size, "args", []string{"values"})

	isEven := parsertest.AssertBucketItemByName(t, got, "functions", "is_even")
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
	writeElixirTestFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if imports, ok := got["imports"].([]map[string]any); !ok {
		t.Fatalf("imports = %T, want []map[string]any", got["imports"])
	} else if len(imports) != 4 {
		t.Fatalf("imports = %#v, want 4 entries", imports)
	}

	genServer := parsertest.AssertBucketItemByName(t, got, "imports", "GenServer")
	assertStringFieldValue(t, genServer, "import_type", "use")
	assertStringFieldValue(t, genServer, "full_import_name", "use GenServer")

	repo := parsertest.AssertBucketItemByName(t, got, "imports", "Demo.Repo")
	assertStringFieldValue(t, repo, "import_type", "alias")
	assertStringFieldValue(t, repo, "alias", "Repo")
	assertStringFieldValue(t, repo, "full_import_name", "alias Demo.Repo")

	patterns := parsertest.AssertBucketItemByName(t, got, "imports", "Demo.Patterns")
	assertStringFieldValue(t, patterns, "import_type", "import")
	assertStringFieldValue(t, patterns, "full_import_name", "import Demo.Patterns")

	logger := parsertest.AssertBucketItemByName(t, got, "imports", "Logger")
	assertStringFieldValue(t, logger, "import_type", "require")
	assertStringFieldValue(t, logger, "full_import_name", "require Logger")

	info := parsertest.AssertBucketItemByName(t, got, "function_calls", "info")
	assertStringFieldValue(t, info, "full_name", "Logger.info")
	assertStringSliceFieldValue(t, info, "args", []string{`"starting"`})
	assertStringFieldValue(t, info, "inferred_obj_type", "Logger")
	assertStringFieldValue(t, info, "class_context", "Demo.Worker")

	greet := parsertest.AssertBucketItemByName(t, got, "function_calls", "greet")
	assertStringFieldValue(t, greet, "full_name", "Demo.Basic.greet")
	assertStringSliceFieldValue(t, greet, "args", []string{"user"})
	assertStringFieldValue(t, greet, "inferred_obj_type", "Demo.Basic")
	assertStringFieldValue(t, greet, "class_context", "Demo.Worker")

	classify := parsertest.AssertBucketItemByName(t, got, "function_calls", "classify")
	assertStringSliceFieldValue(t, classify, "args", []string{"user"})
	assertStringFieldValue(t, classify, "class_context", "Demo.Worker")
	assertStringFieldValue(t, classify, "name", "classify")

	assertBucketMissingName(t, got, "function_calls", "start")
}

func TestDefaultEngineParsePathElixirAliasBraceExpansionAndGuardCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "brace_and_guard.ex")
	writeElixirTestFile(
		t,
		filePath,
		`defmodule Demo.Braces do
  alias Demo.{Basic, Worker, User}

  defguard is_even(value) when rem(value, 2) == 0
end
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if imports, ok := got["imports"].([]map[string]any); !ok {
		t.Fatalf("imports = %T, want []map[string]any", got["imports"])
	} else if len(imports) != 3 {
		t.Fatalf("imports = %#v, want 3 entries", imports)
	}

	basic := parsertest.AssertBucketItemByName(t, got, "imports", "Demo.Basic")
	assertStringFieldValue(t, basic, "import_type", "alias")
	assertStringFieldValue(t, basic, "alias", "Basic")
	assertStringFieldValue(t, basic, "full_import_name", "alias Demo.Basic")

	worker := parsertest.AssertBucketItemByName(t, got, "imports", "Demo.Worker")
	assertStringFieldValue(t, worker, "import_type", "alias")
	assertStringFieldValue(t, worker, "alias", "Worker")
	assertStringFieldValue(t, worker, "full_import_name", "alias Demo.Worker")

	user := parsertest.AssertBucketItemByName(t, got, "imports", "Demo.User")
	assertStringFieldValue(t, user, "import_type", "alias")
	assertStringFieldValue(t, user, "alias", "User")
	assertStringFieldValue(t, user, "full_import_name", "alias Demo.User")

	parsertest.AssertBucketContainsFieldValue(t, got, "functions", "name", "is_even")
	parsertest.AssertBucketContainsFieldValue(t, got, "function_calls", "name", "rem")
}

func TestDefaultEngineParsePathElixirEmitsModuleAttributes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "attributes.ex")
	writeElixirTestFile(
		t,
		filePath,
		`defmodule Demo.Attributes do
  @timeout 5_000
  @service_name "worker"

  def run, do: :ok
end
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	timeout := parsertest.AssertBucketItemByName(t, got, "variables", "@timeout")
	assertStringFieldValue(t, timeout, "class_context", "Demo.Attributes")
	assertStringFieldValue(t, timeout, "context_type", "module")
	assertStringFieldValue(t, timeout, "attribute_kind", "module_attribute")
	assertStringFieldValue(t, timeout, "value", "5_000")

	serviceName := parsertest.AssertBucketItemByName(t, got, "variables", "@service_name")
	assertStringFieldValue(t, serviceName, "class_context", "Demo.Attributes")
	assertStringFieldValue(t, serviceName, "context_type", "module")
	assertStringFieldValue(t, serviceName, "attribute_kind", "module_attribute")
	assertStringFieldValue(t, serviceName, "value", `"worker"`)
}

func TestDefaultEngineParsePathElixirMultilineSourceSpans(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "source_spans.ex")
	writeElixirTestFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	module := parsertest.AssertBucketItemByName(t, got, "modules", "Demo.Source")
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

	render := parsertest.AssertBucketItemByName(t, got, "functions", "render")
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

func TestDefaultEngineParsePathElixirEndLinesDoNotRequireSourceIndex(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "spans_without_source.ex")
	writeElixirTestFile(
		t,
		filePath,
		`defmodule Demo.Worker do
  def caller do
    Demo.Basic.greet()
  end
end
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	caller := parsertest.AssertBucketItemByName(t, got, "functions", "caller")
	assertIntFieldValue(t, caller, "line_number", 2)
	assertIntFieldValue(t, caller, "end_line", 4)
	if _, ok := caller["source"]; ok {
		t.Fatalf("source present without IndexSource: %#v", caller["source"])
	}
}

func TestDefaultEngineParsePathElixirMultilineCallbackSignature(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "multiline_callback.ex")
	writeElixirTestFile(
		t,
		filePath,
		`defmodule Demo.Worker do
  use GenServer

  @impl true
  def handle_call(
        {:run, value},
        _from,
        state
      ) do
    {:reply, Demo.Helper.normalize(value), state}
  end
end
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	handleCall := parsertest.AssertBucketItemByName(t, got, "functions", "handle_call")
	assertIntFieldValue(t, handleCall, "line_number", 5)
	assertIntFieldValue(t, handleCall, "end_line", 11)
	assertStringFieldValue(t, handleCall, "class_context", "Demo.Worker")
	assertStringSliceFieldValue(t, handleCall, "args", []string{"{:run, value}", "_from", "state"})
	parsertest.AssertStringSliceContains(t, handleCall, "dead_code_root_kinds", "elixir.genserver_callback")

	normalize := parsertest.AssertBucketItemByName(t, got, "function_calls", "normalize")
	assertIntFieldValue(t, normalize, "line_number", 10)
	assertStringFieldValue(t, normalize, "full_name", "Demo.Helper.normalize")
	assertStringFieldValue(t, normalize, "class_context", "Demo.Worker")
	assertAnySliceFieldValue(t, normalize, "context", []any{"handle_call", "function", 5})
}

func assertAnySliceFieldValue(t *testing.T, item map[string]any, field string, want []any) {
	t.Helper()

	got, ok := item[field].([]any)
	if !ok {
		t.Fatalf("%s = %T, want []any", field, item[field])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
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
