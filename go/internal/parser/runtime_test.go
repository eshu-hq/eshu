package parser

import "testing"

func TestRuntimeParserLoadsKotlinGrammar(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime()
	parser, err := runtime.Parser("kotlin")
	if err != nil {
		t.Fatalf("Parser(kotlin) error = %v, want nil", err)
	}
	parser.Close()
}

func TestRuntimeParserLoadsDartGrammar(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime()
	parser, err := runtime.Parser("dart")
	if err != nil {
		t.Fatalf("Parser(dart) error = %v, want nil", err)
	}
	defer parser.Close()

	tree := parser.Parse([]byte("class Demo { void run() {} }"), nil)
	if tree == nil {
		t.Fatalf("Parser(dart).Parse returned nil tree")
	}
	defer tree.Close()
	if got, want := tree.RootNode().Kind(), "program"; got != want {
		t.Fatalf("Dart root node kind = %q, want %q", got, want)
	}
}

func TestRuntimeParserLoadsElixirGrammar(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime()
	parser, err := runtime.Parser("elixir")
	if err != nil {
		t.Fatalf("Parser(elixir) error = %v, want nil", err)
	}
	defer parser.Close()

	tree := parser.Parse([]byte("defmodule Demo do\n  def run, do: :ok\nend\n"), nil)
	if tree == nil {
		t.Fatalf("Parser(elixir).Parse returned nil tree")
	}
	tree.Close()
}

func TestRuntimeParserLoadsSwiftGrammar(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime()
	parser, err := runtime.Parser("swift")
	if err != nil {
		t.Fatalf("Parser(swift) error = %v, want nil", err)
	}
	defer parser.Close()

	tree := parser.Parse([]byte("struct Demo { func run() {} }"), nil)
	if tree == nil {
		t.Fatalf("Parser(swift).Parse returned nil tree")
	}
	defer tree.Close()
	if got, want := tree.RootNode().Kind(), "source_file"; got != want {
		t.Fatalf("Swift root node kind = %q, want %q", got, want)
	}
}
