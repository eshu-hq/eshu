package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathElixirImplPrefixAttributeIsNotCallback(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "impl_prefix.ex")
	writeTestFile(
		t,
		filePath,
		`defmodule Demo.Worker do
  @behaviour Demo.Callbacks
  @implementation true
  def init(state) do
    {:ok, state}
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

	assertElixirFunctionWithoutRootKind(t, got, "init", "Demo.Worker", "elixir.behaviour_callback", 1)
}

func TestDefaultEngineParsePathElixirGuardedMultilineCallbackSignature(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "guarded_multiline_callback.ex")
	writeTestFile(
		t,
		filePath,
		`defmodule Demo.Worker do
  use GenServer

  @impl true
  def handle_call(
        {:run, value},
        _from,
        state
      ) when is_integer(value) do
    {:reply, Demo.Helper.normalize(value), state}
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

	handleCall := assertBucketItemByName(t, got, "functions", "handle_call")
	assertIntFieldValue(t, handleCall, "line_number", 5)
	assertIntFieldValue(t, handleCall, "end_line", 11)
	assertStringFieldValue(t, handleCall, "class_context", "Demo.Worker")
	assertStringSliceFieldValue(t, handleCall, "args", []string{"{:run, value}", "_from", "state"})
	assertParserStringSliceContains(t, handleCall, "dead_code_root_kinds", "elixir.behaviour_callback")
	assertParserStringSliceContains(t, handleCall, "dead_code_root_kinds", "elixir.genserver_callback")

	normalize := assertBucketItemByName(t, got, "function_calls", "normalize")
	assertIntFieldValue(t, normalize, "line_number", 10)
	assertStringFieldValue(t, normalize, "full_name", "Demo.Helper.normalize")
	assertStringFieldValue(t, normalize, "class_context", "Demo.Worker")
	assertAnySliceFieldValue(t, normalize, "context", []any{"handle_call", "function", 5})
}
