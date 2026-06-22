package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathElixirModuleSpanCoversNestedEnds proves the AST
// extractor reports the module end line from the definition node span rather
// than the first matching `end` keyword. The former line scanner popped the
// module scope at the inner `case ... end`, recording end_line 15 and dropping
// the trailing function's module context. The AST span is authoritative.
func TestDefaultEngineParsePathElixirModuleSpanCoversNestedEnds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "nested_end.ex")
	writeTestFile(
		t,
		filePath,
		`defmodule Demo.Patterns do
  def classify(value) do
    case value do
      :ok -> :good
      _ -> :unknown
    end
  end

  def trailing(items) do
    items
  end
end
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

	module := assertBucketItemByName(t, got, "modules", "Demo.Patterns")
	assertIntFieldValue(t, module, "line_number", 1)
	assertIntFieldValue(t, module, "end_line", 12)

	trailing := assertFunctionByNameAndClass(t, got, "trailing", "Demo.Patterns")
	assertStringFieldValue(t, trailing, "context_type", "module")
	assertAnySliceFieldValue(t, trailing, "context", []any{"Demo.Patterns", "module", 1})
}

// TestDefaultEngineParsePathElixirProtocolImplFunctionsKeepOwnContext proves the
// AST extractor attaches each same-named function to its own enclosing module
// span and root kinds. The former name-keyed overlay collided the protocol
// `describe/1` with its implementations, leaking a protocol-implementation root
// onto the protocol declaration and copying one module's context line to all.
func TestDefaultEngineParsePathElixirProtocolImplFunctionsKeepOwnContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "protocol_impls.ex")
	writeTestFile(
		t,
		filePath,
		`defprotocol Demo.Describable do
  def describe(data)
end

defmodule Demo.User do
  defstruct [:name]
end

defimpl Demo.Describable, for: Demo.User do
  def describe(user), do: user
end

defimpl Demo.Describable, for: Map do
  def describe(map), do: map
end
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

	functions, ok := got["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", got["functions"])
	}

	type describeCase struct {
		line          int
		contextLine   int
		wantRoot      string
		forbiddenRoot string
	}
	cases := map[int]describeCase{
		2:  {line: 2, contextLine: 1, wantRoot: "elixir.protocol_function", forbiddenRoot: "elixir.protocol_implementation_function"},
		10: {line: 10, contextLine: 9, wantRoot: "elixir.protocol_implementation_function", forbiddenRoot: "elixir.protocol_function"},
		14: {line: 14, contextLine: 13, wantRoot: "elixir.protocol_implementation_function", forbiddenRoot: "elixir.protocol_function"},
	}

	seen := map[int]bool{}
	for _, function := range functions {
		if function["name"] != "describe" {
			continue
		}
		line, _ := function["line_number"].(int)
		want, ok := cases[line]
		if !ok {
			t.Fatalf("unexpected describe at line %d: %#v", line, function)
		}
		seen[line] = true
		assertAnySliceFieldValue(t, function, "context", []any{"Demo.Describable", "module", want.contextLine})
		if !stringSliceFieldContains(function, "dead_code_root_kinds", want.wantRoot) {
			t.Fatalf("describe@%d missing root %q: %#v", line, want.wantRoot, function)
		}
		if stringSliceFieldContains(function, "dead_code_root_kinds", want.forbiddenRoot) {
			t.Fatalf("describe@%d unexpectedly has root %q: %#v", line, want.forbiddenRoot, function)
		}
	}
	for line := range cases {
		if !seen[line] {
			t.Fatalf("missing describe at line %d", line)
		}
	}
}

// TestDefaultEngineParsePathElixirOneLineDoBodyOmitsCalls proves that a one-line
// `def ..., do: body` definition does not register calls from its body, matching
// the prior extractor, while still flagging dynamic dispatch for reachability.
func TestDefaultEngineParsePathElixirOneLineDoBodyOmitsCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "one_line.ex")
	writeTestFile(
		t,
		filePath,
		`defmodule Demo.Math do
  def fib(n), do: fib(n - 1) + fib(n - 2)
  def dispatch(name), do: apply(__MODULE__, name, [])
end
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

	assertBucketMissingName(t, got, "function_calls", "fib")
	assertBucketMissingName(t, got, "function_calls", "apply")

	dispatch := assertFunctionByNameAndClass(t, got, "dispatch", "Demo.Math")
	assertParserStringSliceContains(t, dispatch, "exactness_blockers", "dynamic_dispatch_unresolved")
}

// TestDefaultEngineParsePathElixirFieldAccessIsNotCall proves dotted field
// access with no argument list (`state.items`) and control-flow special forms
// (`case`, `for`) are not emitted as function calls, matching the parenthesized
// call requirement of the prior extractor.
func TestDefaultEngineParsePathElixirFieldAccessIsNotCall(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "field_access.ex")
	writeTestFile(
		t,
		filePath,
		`defmodule Demo.Worker do
  def update(state, item) do
    new_state = %{state | items: [item | state.items]}

    case new_state do
      %{items: items} -> length(items)
      _ -> 0
    end
  end
end
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

	assertBucketMissingName(t, got, "function_calls", "items")
	assertBucketMissingName(t, got, "function_calls", "case")
	length := assertBucketItemByName(t, got, "function_calls", "length")
	assertStringFieldValue(t, length, "class_context", "Demo.Worker")
}
