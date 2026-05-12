package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathElixirEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "lib/demo_web/controllers/page_controller.ex")
	writeTestFile(
		t,
		sourcePath,
		`defmodule DemoWeb.PageController do
  use DemoWeb, :controller

  def index(conn, params), do: render(conn, "index.html", params: params)
  defp helper(), do: :unused
end

defmodule Demo.Worker do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

  @impl true
  def init(opts), do: {:ok, opts}

  @impl GenServer
  def handle_call(:status, _from, state), do: {:reply, :ok, state}

  def helper(), do: :unused
end

defmodule Mix.Tasks.Demo.Sync do
  use Mix.Task

  def run(args), do: IO.inspect(args)
end

defprotocol Demo.Serializable do
  def serialize(data)
end

defimpl Demo.Serializable, for: Demo.Worker do
  def serialize(worker), do: worker
end

defmodule DemoWeb.CounterLive do
  use Phoenix.LiveView

  def mount(_params, _session, socket), do: {:ok, socket}
  def handle_event("inc", _params, socket), do: {:noreply, socket}
  def render(assigns), do: ~H"<div></div>"
end

defmodule Demo.Macros do
  def start(_type, _args), do: :ok
  def main(args), do: args
  defmacro expose(expr), do: expr
  defguard is_even(value) when rem(value, 2) == 0
  def one_line_dispatch(name), do: apply(__MODULE__, name, [])
  defp private_macro_candidate(), do: :unused
end
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "index", "DemoWeb.PageController"), "dead_code_root_kinds", "elixir.phoenix_controller_action")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "init", "Demo.Worker"), "dead_code_root_kinds", "elixir.behaviour_callback")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "init", "Demo.Worker"), "dead_code_root_kinds", "elixir.genserver_callback")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "handle_call", "Demo.Worker"), "dead_code_root_kinds", "elixir.behaviour_callback")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "handle_call", "Demo.Worker"), "dead_code_root_kinds", "elixir.genserver_callback")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Mix.Tasks.Demo.Sync"), "dead_code_root_kinds", "elixir.mix_task_run")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "serialize", "Demo.Serializable"), "dead_code_root_kinds", "elixir.protocol_function")
	assertElixirFunctionRootKindExists(t, got, "serialize", "Demo.Serializable", "elixir.protocol_implementation_function")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "mount", "DemoWeb.CounterLive"), "dead_code_root_kinds", "elixir.phoenix_liveview_callback")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "handle_event", "DemoWeb.CounterLive"), "dead_code_root_kinds", "elixir.phoenix_liveview_callback")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "render", "DemoWeb.CounterLive"), "dead_code_root_kinds", "elixir.phoenix_liveview_callback")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "start", "Demo.Macros"), "dead_code_root_kinds", "elixir.application_start")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "main", "Demo.Macros"), "dead_code_root_kinds", "elixir.main_function")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "expose", "Demo.Macros"), "dead_code_root_kinds", "elixir.public_macro")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "is_even", "Demo.Macros"), "dead_code_root_kinds", "elixir.public_guard")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "one_line_dispatch", "Demo.Macros"), "exactness_blockers", "dynamic_dispatch_unresolved")

	if helper := assertFunctionByNameAndClass(t, got, "helper", "DemoWeb.PageController"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("PageController.helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
	if helper := assertFunctionByNameAndClass(t, got, "helper", "Demo.Worker"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("Worker.helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
	if helper := assertFunctionByNameAndClass(t, got, "private_macro_candidate", "Demo.Macros"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("Demo.Macros.private_macro_candidate dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathElixirDeadCodeFixtureExpectedRoots(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("deadcode", "elixir")
	sourcePath := repoFixturePath("deadcode", "elixir", "app.ex")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "start", "DeadCodeFixture.DynamicElixir"), "dead_code_root_kinds", "elixir.application_start")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "init", "DeadCodeFixture.DynamicElixir"), "dead_code_root_kinds", "elixir.behaviour_callback")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "init", "DeadCodeFixture.DynamicElixir"), "dead_code_root_kinds", "elixir.genserver_callback")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "handle_call", "DeadCodeFixture.DynamicElixir"), "dead_code_root_kinds", "elixir.behaviour_callback")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "handle_call", "DeadCodeFixture.DynamicElixir"), "dead_code_root_kinds", "elixir.genserver_callback")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "dynamic_elixir_dispatch", "DeadCodeFixture.DynamicElixir"), "exactness_blockers", "dynamic_dispatch_unresolved")

	for _, name := range []string{"public_elixir_api", "unused_elixir_helper", "generated_elixir_stub"} {
		if function := assertFunctionByNameAndClass(t, got, name, "DeadCodeFixture.DynamicElixir"); function["dead_code_root_kinds"] != nil {
			t.Fatalf("%s dead_code_root_kinds = %#v, want nil", name, function["dead_code_root_kinds"])
		}
	}
}

func assertElixirFunctionRootKindExists(t *testing.T, payload map[string]any, name string, classContext string, rootKind string) {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, function := range functions {
		functionName, _ := function["name"].(string)
		functionClassContext, _ := function["class_context"].(string)
		if functionName != name || functionClassContext != classContext {
			continue
		}
		if stringSliceFieldContains(function, "dead_code_root_kinds", rootKind) {
			return
		}
	}
	t.Fatalf("functions missing root kind %q for name %q with class_context %q in %#v", rootKind, name, classContext, functions)
}

func stringSliceFieldContains(item map[string]any, field string, value string) bool {
	switch values := item[field].(type) {
	case []string:
		for _, got := range values {
			if got == value {
				return true
			}
		}
	case []any:
		for _, got := range values {
			if got == value {
				return true
			}
		}
	}
	return false
}
