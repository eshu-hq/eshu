package parser

import (
	"testing"
)

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

func TestRuntimeParserLoadsHaskellGrammar(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime()
	parser, err := runtime.Parser("haskell")
	if err != nil {
		t.Fatalf("Parser(haskell) error = %v, want nil", err)
	}
	defer parser.Close()

	tree := parser.Parse([]byte("module Main where\nmain = pure ()\n"), nil)
	if tree == nil {
		t.Fatalf("Parser(haskell).Parse returned nil tree")
	}
	defer tree.Close()
	if got, want := tree.RootNode().Kind(), "haskell"; got != want {
		t.Fatalf("Haskell root node kind = %q, want %q", got, want)
	}
}

func TestRuntimeParserLoadsPerlGrammar(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime()
	parser, err := runtime.Parser("perl")
	if err != nil {
		t.Fatalf("Parser(perl) error = %v, want nil", err)
	}
	defer parser.Close()

	tree := parser.Parse([]byte("package App::Worker;\nsub run { return 1; }\n"), nil)
	if tree == nil {
		t.Fatalf("Parser(perl).Parse returned nil tree")
	}
	defer tree.Close()
	if got, want := tree.RootNode().Kind(), "source_file"; got != want {
		t.Fatalf("Perl root node kind = %q, want %q", got, want)
	}
}

func TestRuntimeParserLoadsGroovyGrammar(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime()
	parser, err := runtime.Parser("groovy")
	if err != nil {
		t.Fatalf("Parser(groovy) error = %v, want nil", err)
	}
	defer parser.Close()

	tree := parser.Parse([]byte("class Demo { def run() { deploy() } }"), nil)
	if tree == nil {
		t.Fatalf("Parser(groovy).Parse returned nil tree")
	}
	defer tree.Close()
	if got, want := tree.RootNode().Kind(), "source_file"; got != want {
		t.Fatalf("Groovy root node kind = %q, want %q", got, want)
	}
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

// TestRuntimePutParserAllowsReuse proves that a pooled parser can be borrowed,
// used, returned via PutParser, and then borrowed again with correct behaviour.
func TestRuntimePutParserAllowsReuse(t *testing.T) {
	t.Parallel()

	rt := NewRuntime()

	// Borrow and use parser1.
	parser1, err := rt.Parser("python")
	if err != nil {
		t.Fatalf("Parser(python) first call: %v", err)
	}
	snippet := []byte("def hello(): pass\n")
	tree1 := parser1.Parse(snippet, nil)
	if tree1 == nil {
		parser1.Close()
		t.Fatal("Parser(python) first parse returned nil tree")
	}
	tree1.Close()

	// Return parser1 to the pool.
	rt.PutParser("python", parser1)

	// Borrow parser2; with a pool it may be the same underlying object.
	parser2, err := rt.Parser("python")
	if err != nil {
		t.Fatalf("Parser(python) second call: %v", err)
	}
	defer rt.PutParser("python", parser2)

	// The pooled/reset parser must still be able to parse correctly.
	tree2 := parser2.Parse(snippet, nil)
	if tree2 == nil {
		t.Fatal("Parser(python) reused parse returned nil tree — pool Reset broke the parser")
	}
	defer tree2.Close()
	if got, want := tree2.RootNode().Kind(), "module"; got != want {
		t.Fatalf("Python root node kind = %q, want %q after pool reuse", got, want)
	}
}

// BenchmarkRuntimeParserPoolReuse measures the per-call CGO cost of Parser +
// PutParser on a warm Runtime.  A pooled implementation should converge to
// 0-1 allocs/op on steady-state iterations; a fresh-NewParser implementation
// allocates 3+ objects per call.
func BenchmarkRuntimeParserPoolReuse(b *testing.B) {
	rt := NewRuntime()
	// Warm the pool with one initial borrow/return so the first bench iteration
	// does not pay the language-load cost.
	p, err := rt.Parser("python")
	if err != nil {
		b.Fatalf("Parser(python) warm-up: %v", err)
	}
	rt.PutParser("python", p)

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		parser, err := rt.Parser("python")
		if err != nil {
			b.Fatalf("Parser(python): %v", err)
		}
		rt.PutParser("python", parser)
	}
}
