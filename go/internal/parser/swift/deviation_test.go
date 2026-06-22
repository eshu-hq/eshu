package swift

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// The AST migration (issue #3589) replaced the line-scan call regex with a
// tree-sitter call_expression walk. The walk corrects three classes of
// regex defect in the function_calls bucket. These tests assert the NEW correct
// behavior so the fixes cannot silently regress, independent of the parity
// golden. Each test fails against the old regex extractor and passes against the
// AST walk.

// callRow returns the first function_calls row with the given full_name.
func callRow(t *testing.T, payload map[string]any, fullName string) (map[string]any, bool) {
	t.Helper()
	rows, _ := payload["function_calls"].([]map[string]any)
	for _, row := range rows {
		if name, _ := row["full_name"].(string); name == fullName {
			return row, true
		}
	}
	return nil, false
}

func parseSwiftDeviationSource(t *testing.T, source string) map[string]any {
	t.Helper()
	parser := newSwiftParityParser(t)
	defer parser.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "deviation.swift")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	payload, err := Parse(path, false, shared.Options{IndexSource: true}, parser)
	if err != nil {
		t.Fatalf("Parse error = %v, want nil", err)
	}
	return payload
}

func assertCallArgs(t *testing.T, payload map[string]any, fullName string, wantArgs []string) {
	t.Helper()
	row, ok := callRow(t, payload, fullName)
	if !ok {
		t.Fatalf("function_calls missing full_name=%q in %#v", fullName, payload["function_calls"])
	}
	args, _ := row["args"].([]string)
	if len(args) != len(wantArgs) {
		t.Fatalf("args for %q = %#v, want %#v", fullName, args, wantArgs)
	}
	for index, want := range wantArgs {
		if args[index] != want {
			t.Fatalf("args for %q = %#v, want %#v", fullName, args, wantArgs)
		}
	}
}

func assertNoCall(t *testing.T, payload map[string]any, fullName string) {
	t.Helper()
	if _, ok := callRow(t, payload, fullName); ok {
		t.Fatalf("function_calls unexpectedly contains full_name=%q in %#v", fullName, payload["function_calls"])
	}
}

// TestSwiftCallWalkSkipsDeclarationLines proves declaration headers no longer
// masquerade as calls. The old plain-call regex skipped only `func `/`init(`
// prefixes, so `override func`, `mutating func`, and `private func` lines leaked
// false call rows.
func TestSwiftCallWalkSkipsDeclarationLines(t *testing.T) {
	payload := parseSwiftDeviationSource(t, `class Worker {
    override func start() {}
    private func helper() {}
}

struct Point {
    mutating func translate(dx: Double) {}
}

private func unusedCleanupCandidate() {}
`)
	for _, name := range []string{"start", "helper", "translate", "unusedCleanupCandidate"} {
		assertNoCall(t, payload, name)
	}
}

// TestSwiftCallWalkSkipsEnumCases proves enum case declarations are not read as
// calls. `case serverError(code: Int, ...)` is an enum_entry, not a
// call_expression.
func TestSwiftCallWalkSkipsEnumCases(t *testing.T) {
	payload := parseSwiftDeviationSource(t, `enum NetworkError: Error {
    case notFound
    case serverError(code: Int, message: String)
    case invalidInput(String)
}
`)
	for _, name := range []string{"serverError", "invalidInput", "notFound"} {
		assertNoCall(t, payload, name)
	}
}

// TestSwiftCallWalkSkipsModifierKeyword proves `private(set)` access modifiers no
// longer produce a phantom `private` call.
func TestSwiftCallWalkSkipsModifierKeyword(t *testing.T) {
	payload := parseSwiftDeviationSource(t, `class Animal {
    private(set) var species: String = "x"
}
`)
	assertNoCall(t, payload, "private")
}

// TestSwiftCallWalkCorrectsNestedArguments proves nested call arguments are read
// from the AST argument node, not by slicing to the last paren. The old regex
// produced args ["value)"] for transform(value) inside an outer call.
func TestSwiftCallWalkCorrectsNestedArguments(t *testing.T) {
	payload := parseSwiftDeviationSource(t, `enum Result<Success, Failure: Error> {
    case success(Success)
    func map<T>(_ transform: (Success) -> T) -> Result<T, Failure> {
        switch self {
        case .success(let value):
            return .success(transform(value))
        case .failure(let error):
            return .failure(error)
        }
    }
}
`)
	assertCallArgs(t, payload, "transform", []string{"value"})
}

// TestSwiftCallWalkCapturesTrailingClosureCalls proves trailing-closure calls
// with no parentheses are captured. The old regex required `name(` and missed
// `WindowGroup { ... }`.
func TestSwiftCallWalkCapturesTrailingClosureCalls(t *testing.T) {
	payload := parseSwiftDeviationSource(t, `struct DemoApp {
    var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}
`)
	if _, ok := callRow(t, payload, "WindowGroup"); !ok {
		t.Fatalf("function_calls missing WindowGroup trailing-closure call in %#v", payload["function_calls"])
	}
}

// TestSwiftCallWalkPreservesSuperAndSelfReceiverCalls proves genuine
// `super.method(...)` and `self.method(...)` calls survive the migration with
// their receiver-prefixed full_name and exact argument strings.
func TestSwiftCallWalkPreservesSuperAndSelfReceiverCalls(t *testing.T) {
	payload := parseSwiftDeviationSource(t, `class Dog {
    init(name: String) {
        super.init(name: name, species: "Canine")
    }
    func run() {
        self.helper()
    }
    func helper() {}
}
`)
	assertCallArgs(t, payload, "super.init", []string{"name: name", "species: \"Canine\""})
	if _, ok := callRow(t, payload, "self.helper"); !ok {
		t.Fatalf("function_calls missing self.helper receiver call in %#v", payload["function_calls"])
	}
}

// TestSwiftCallWalkKeepsAttributeArgsOutOfCalls proves attribute arguments such
// as @available(iOS 13, *) and @Test("...") never produce call rows, because
// attribute argument lists are not call_expression nodes.
func TestSwiftCallWalkKeepsAttributeArgsOutOfCalls(t *testing.T) {
	payload := parseSwiftDeviationSource(t, `protocol Runnable {
    @available(iOS 13, *)
    init()
}

@Test("runs from runner")
func swiftTestingRunsFromRunner() {}
`)
	assertNoCall(t, payload, "available")
	assertNoCall(t, payload, "Test")
}

// TestSwiftFunctionLineSkipsLeadingAttributeLines proves a declaration reports
// its keyword line, not an attribute line placed above it, preserving the line
// numbers the prior extractor recorded for attributed declarations.
func TestSwiftFunctionLineSkipsLeadingAttributeLines(t *testing.T) {
	payload := parseSwiftDeviationSource(t, `@Test("runs from runner")
func runner() {}
`)
	rows, _ := payload["functions"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("functions = %#v, want exactly one row", rows)
	}
	if got := shared.IntValue(rows[0]["line_number"]); got != 2 {
		t.Fatalf("line_number = %d, want 2 (the func keyword line, not the @Test line)", got)
	}
}
