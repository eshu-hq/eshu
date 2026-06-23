package parser

import (
	"sync"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
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

// TestRuntimePutParserBoundsFreeList proves the per-language free-list never
// grows past its fixed capacity. Returning more parsers than the capacity must
// not grow the channel; the surplus parsers are Closed instead of retained, so
// a long-running process cannot accumulate idle native TSParser allocations.
func TestRuntimePutParserBoundsFreeList(t *testing.T) {
	t.Parallel()

	rt := NewRuntime()

	// Borrow more parsers than the free-list can hold, then return them all.
	// The first parserFreeListCapacity returns are retained; the rest are
	// Closed by PutParser. None are leaked and the channel never overflows.
	borrowed := make([]*tree_sitter.Parser, 0, parserFreeListCapacity*2)
	for range parserFreeListCapacity * 2 {
		p, err := rt.Parser("python")
		if err != nil {
			t.Fatalf("Parser(python): %v", err)
		}
		borrowed = append(borrowed, p)
	}
	for _, p := range borrowed {
		rt.PutParser("python", p)
	}

	freeList := rt.freeListLenForTest("python")
	if freeList != parserFreeListCapacity {
		t.Fatalf("free-list length = %d, want %d (bounded)", freeList, parserFreeListCapacity)
	}
}

// TestRuntimePutParserUnknownLanguageDoesNotPanic proves PutParser closes a
// parser whose language has no free-list rather than dropping or retaining it.
func TestRuntimePutParserUnknownLanguageDoesNotPanic(t *testing.T) {
	t.Parallel()

	rt := NewRuntime()
	// Borrow a real parser, then return it under a canonical name that was
	// never borrowed so no free-list exists. PutParser must Close it safely.
	p, err := rt.Parser("python")
	if err != nil {
		t.Fatalf("Parser(python): %v", err)
	}
	rt.PutParser("ruby", p) // ruby free-list does not exist yet; must Close p.
}

// TestRuntimeParserConcurrentBorrowReturnIsSafe exercises the per-language
// free-list under the concurrent parse worker fan-out it must support. With
// -race this proves borrow/return has no data race and that the free-list stays
// bounded after many concurrent goroutines return parsers.
func TestRuntimeParserConcurrentBorrowReturnIsSafe(t *testing.T) {
	t.Parallel()

	rt := NewRuntime()
	snippet := []byte("def hello(): pass\n")

	const workers = 16
	const iterations = 64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				p, err := rt.Parser("python")
				if err != nil {
					t.Errorf("Parser(python): %v", err)
					return
				}
				tree := p.Parse(snippet, nil)
				if tree != nil {
					tree.Close()
				}
				rt.PutParser("python", p)
			}
		}()
	}
	wg.Wait()

	if got := rt.freeListLenForTest("python"); got > parserFreeListCapacity {
		t.Fatalf("free-list length = %d, want <= %d after concurrent use", got, parserFreeListCapacity)
	}
}

// BenchmarkRuntimeParserPoolReuse measures the per-call CGO cost of Parser +
// PutParser on a warm Runtime.  A free-list implementation should converge to
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
