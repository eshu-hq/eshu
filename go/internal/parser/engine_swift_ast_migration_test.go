package parser

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultEngineParsePathSwiftASTCallExtractionFixesRegexFalsePositives proves
// the tree-sitter AST migration (#3589) removes the line-scan regex false-positive
// calls and keeps only genuine call_expression nodes. The prior scanner mis-read
// enum case declarations, `mutating func`/`override func` declaration lines,
// `private(set)` modifiers, and string interpolation as calls. The AST walk reads
// only real call_expression nodes, so those rows must not appear.
func TestDefaultEngineParsePathSwiftASTCallExtractionFixesRegexFalsePositives(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Sample.swift")
	writeTestFile(
		t,
		filePath,
		`import Foundation

enum Outcome {
    case success(Value)
    case failure(Error)
    case detail(code: Int, message: String)
}

struct Point {
    var x: Double

    func distance() -> Double {
        return (x * x).squareRoot()
    }

    mutating func translate(dx: Double) {
        x += dx
    }
}

class Animal {
    private(set) var species: String

    func describe() -> String { return "an \(species)" }
}

class Dog: Animal {
    override func describe() -> String { return "a dog" }

    func bark(times: Int) {
        for _ in 0..<times { print("woof") }
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	// Regex false positives the AST migration removes.
	for _, name := range []string{
		"success",   // enum case declaration
		"failure",   // enum case declaration
		"detail",    // enum case declaration with labels
		"translate", // `mutating func translate` declaration line
		"describe",  // `override func describe` declaration line
		"private",   // `private(set)` modifier
		"bark",      // `func bark` declaration line
	} {
		assertBucketMissingItemByName(t, payload, "function_calls", name)
	}

	// Genuine calls the AST walk captures, including subscript/method calls the
	// prior scanner missed on chained expressions.
	assertSwiftCallMetadata(t, payload, "squareRoot", "squareRoot")
	assertSwiftCallMetadata(t, payload, "print", "print")
}

// TestDefaultEngineParsePathSwiftASTFunctionSourceSpansFullBody proves the AST
// migration records the full function-body source for IndexSource, not just the
// signature line the prior line scanner captured. The body must be part of the
// emitted source so downstream snippet and complexity consumers see the real span.
func TestDefaultEngineParsePathSwiftASTFunctionSourceSpansFullBody(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Worker.swift")
	writeTestFile(
		t,
		filePath,
		`actor Worker {
    func run() {
        print("running")
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, filePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	run := assertFunctionByNameAndClass(t, payload, "run", "Worker")
	source, _ := run["source"].(string)
	if source == "" {
		t.Fatalf("run source = %#v, want non-empty", run["source"])
	}
	if !strings.Contains(source, `print("running")`) {
		t.Fatalf("run source = %q, want it to span the function body", source)
	}
}
