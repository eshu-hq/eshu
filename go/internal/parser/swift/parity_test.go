package swift

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter_swift "github.com/indigo-net/Brf.it/pkg/parser/treesitter/grammars/swift"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// parityCase names one Swift source whose full Parse payload is locked by the
// golden snapshot. Fixture cases read from the shared swift_comprehensive
// corpus; inline cases embed the exact source used by parent-package tests so
// the byte-parity guard covers attribute, dead-code-root, and call edge cases.
type parityCase struct {
	name    string
	fixture string
	source  string
}

func swiftParityCases() []parityCase {
	return []parityCase{
		{name: "Classes", fixture: "Classes.swift"},
		{name: "Structs", fixture: "Structs.swift"},
		{name: "Actors", fixture: "Actors.swift"},
		{name: "Enums", fixture: "Enums.swift"},
		{name: "Protocols", fixture: "Protocols.swift"},
		{name: "Generics", fixture: "Generics.swift"},
		{name: "Basic", fixture: "Basic.swift"},
		{name: "DeadCodeRoots", source: deadCodeRootsSource},
		{name: "MultilineScope", source: multilineScopeSource},
		{name: "ReceiverInference", source: receiverInferenceSource},
		{name: "ExtensionContext", source: extensionContextSource},
	}
}

// TestSwiftParseParityGolden locks the full Parse payload (all buckets, all
// rows, all keys, after SortNamedBucket) for representative Swift sources. It is
// the byte-parity regression guard for the AST migration: it must stay green
// before and after extraction moves from the line-scan regex to the tree-sitter
// node walk. Regenerate the golden with -update only for documented deviations.
func TestSwiftParseParityGolden(t *testing.T) {
	parser := newSwiftParityParser(t)
	defer parser.Close()

	for _, tc := range swiftParityCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			path := swiftParitySourcePath(t, tc)
			payload, err := Parse(path, false, shared.Options{IndexSource: true}, parser)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v, want nil", path, err)
			}
			got := canonicalParityJSON(t, payload)

			goldenPath := filepath.Join("testdata", "parity", tc.name+".json")
			if shouldUpdateGolden() {
				writeGolden(t, goldenPath, got)
				return
			}
			want := readGolden(t, goldenPath)
			if got != want {
				t.Fatalf("parity payload for %s changed.\n--- got ---\n%s\n--- want ---\n%s", tc.name, got, want)
			}
		})
	}
}

func newSwiftParityParser(t *testing.T) *tree_sitter.Parser {
	t.Helper()
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_swift.Language())); err != nil {
		parser.Close()
		t.Fatalf("set swift language: %v", err)
	}
	return parser
}

func swiftParitySourcePath(t *testing.T, tc parityCase) string {
	t.Helper()
	if tc.fixture != "" {
		return filepath.Join("..", "..", "..", "..", "tests", "fixtures", "ecosystems", "swift_comprehensive", tc.fixture)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, tc.name+".swift")
	if err := os.WriteFile(path, []byte(tc.source), 0o600); err != nil {
		t.Fatalf("write inline source %q: %v", path, err)
	}
	return path
}

// canonicalParityJSON serializes the payload with every map key sorted and the
// volatile "path" field dropped so inline temp-dir sources stay comparable.
func canonicalParityJSON(t *testing.T, payload map[string]any) string {
	t.Helper()
	cleaned := make(map[string]any, len(payload))
	for key, value := range payload {
		if key == "path" {
			continue
		}
		cleaned[key] = normalizeParityValue(value)
	}
	encoded, err := json.MarshalIndent(cleaned, "", "  ")
	if err != nil {
		t.Fatalf("marshal parity payload: %v", err)
	}
	return string(encoded)
}

// normalizeParityValue converts payload values into plain JSON-friendly types
// with deterministic ordering so encoding is stable across runs.
func normalizeParityValue(value any) any {
	switch typed := value.(type) {
	case []map[string]any:
		rows := make([]any, 0, len(typed))
		for _, row := range typed {
			rows = append(rows, normalizeParityValue(row))
		}
		return rows
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, inner := range typed {
			out[key] = normalizeParityValue(inner)
		}
		return out
	case []string:
		// Preserve the nil/empty distinction: a nil slice marshals to null and an
		// empty non-nil slice marshals to [], and the migration must keep the
		// exact same shape for fields like decorators and bases.
		if typed == nil {
			return nil
		}
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	default:
		return value
	}
}

func shouldUpdateGolden() bool {
	return os.Getenv("UPDATE_SWIFT_GOLDEN") == "1"
}

func writeGolden(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir golden dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content+"\n"), 0o600); err != nil {
		t.Fatalf("write golden %q: %v", path, err)
	}
}

func readGolden(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %q: %v (run with UPDATE_SWIFT_GOLDEN=1 to create)", path, err)
	}
	trimmed := string(body)
	for len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed
}

const deadCodeRootsSource = `import SwiftUI
import UIKit
import Vapor
import XCTest

@main struct DemoApp: App {
    public var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}

public protocol Runnable {
    @available(iOS 13, *)
    init()

    func run()
}

public final class Worker: BaseWorker, Runnable {
    init(name: String) {}

    override func start() {}

    func run() {}

    private func helper() {}
}

open class AppDelegate: NSObject, UIApplicationDelegate {
    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        true
    }
}

func configure(_ app: Application) throws {
    app.get("health", use: health)
}

func health(_ req: Request) async throws -> String {
    "ok"
}

class ServiceTests: XCTestCase {
    func testRunsFromXCTest() {}
}

@Test("runs from runner")
func swiftTestingRunsFromRunner() {}

func main() {}

private func unusedCleanupCandidate() {}
`

const multilineScopeSource = `import SwiftUI

@main
struct DemoApp:
    App
{
    var body: some Scene { WindowGroup { ContentView() } }
}

protocol Runnable
{
    func run()
}

class Worker:
    Runnable
{
    let logger: Logger

    init(
        logger: Logger
    ) {
        self.logger = logger
    }

    func run(
        id: String
    ) -> Result {
        logger.info("run")
    }
}
`

const receiverInferenceSource = `import Foundation

protocol Runnable {
    func run()
}

class Logger {
    func info(_ message: String) {}
}

class Worker: Runnable {
    let logger: Logger

    init(logger: Logger) {
        self.logger = logger
    }

    func run() {
        logger.info("running")
    }
}
`

const extensionContextSource = `import Foundation

protocol Logger {
    func log(_ level: String)
}

extension Logger {
    func info(_ message: String) { log("INFO") }
    func warn(_ message: String) { log("WARN") }
}

struct Point {
    var x: Double
}

extension Point: Equatable {
    func translated() -> Point { return self }
}
`
