// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package swift_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathSwiftEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Sources", "App", "App.swift")
	writeSwiftTestFile(
		t,
		sourcePath,
		`import SwiftUI
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
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "structs", "DemoApp"), "dead_code_root_kinds", "swift.main_type")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "structs", "DemoApp"), "dead_code_root_kinds", "swift.swiftui_app_type")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "variables", "body"), "dead_code_root_kinds", "swift.swiftui_body")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "protocols", "Runnable"), "dead_code_root_kinds", "swift.protocol_type")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "run", "Runnable"), "dead_code_root_kinds", "swift.protocol_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "init", "Runnable"), "dead_code_root_kinds", "swift.protocol_method")
	parsertest.AssertStringSliceNotContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "init", "Runnable"), "dead_code_root_kinds", "swift.constructor")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "init", "Worker"), "dead_code_root_kinds", "swift.constructor")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "start", "Worker"), "dead_code_root_kinds", "swift.override_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "run", "Worker"), "dead_code_root_kinds", "swift.protocol_implementation_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "classes", "AppDelegate"), "dead_code_root_kinds", "swift.ui_application_delegate_type")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "application", "AppDelegate"), "dead_code_root_kinds", "swift.ui_application_delegate_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "health"), "dead_code_root_kinds", "swift.vapor_route_handler")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "testRunsFromXCTest", "ServiceTests"), "dead_code_root_kinds", "swift.xctest_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "swiftTestingRunsFromRunner"), "dead_code_root_kinds", "swift.swift_testing_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "main"), "dead_code_root_kinds", "swift.main_function")
	assertBucketMissingItemByName(t, got, "function_calls", "available")
	assertBucketMissingItemByName(t, got, "function_calls", "Test")

	for _, tc := range []struct {
		name         string
		classContext string
	}{
		{name: "helper", classContext: "Worker"},
		{name: "unusedCleanupCandidate"},
	} {
		function := parsertest.AssertBucketItemByName(t, got, "functions", tc.name)
		if tc.classContext != "" {
			function = parsertest.AssertFunctionByNameAndClass(t, got, tc.name, tc.classContext)
		}
		if function["dead_code_root_kinds"] != nil {
			t.Fatalf("%s.%s dead_code_root_kinds = %#v, want nil", tc.classContext, tc.name, function["dead_code_root_kinds"])
		}
	}
}

// TestDefaultEngineParsePathSwiftRequiresVaporImportForRouteHandlerRoot
// characterizes issue #5337 Detector 2: collectSwiftVaporRouteHandler records
// any `use:` labeled call argument as a Vapor route handler with zero gating
// on whether the file actually imports Vapor, so a same-shaped `use:` call
// from an unrelated framework/DSL fabricates a swift.vapor_route_handler
// dead-code root. Without `import Vapor`, the identical
// `app.get("health", use: health)` call shape must not root health.
func TestDefaultEngineParsePathSwiftRequiresVaporImportForRouteHandlerRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Sources", "App", "NoVaporApp.swift")
	writeSwiftTestFile(
		t,
		sourcePath,
		`func configure(_ app: Application) throws {
    app.get("health", use: health)
}

func health(_ req: Request) async throws -> String {
    "ok"
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	if health := parsertest.AssertBucketItemByName(t, got, "functions", "health"); health["dead_code_root_kinds"] != nil {
		t.Fatalf("health dead_code_root_kinds = %#v, want nil (no import Vapor)", health["dead_code_root_kinds"])
	}
}

func assertBucketMissingItemByName(t *testing.T, payload map[string]any, bucket string, name string) {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		itemName, _ := item["name"].(string)
		if itemName == name {
			t.Fatalf("%s has unexpected name %q in %#v", bucket, name, items)
		}
	}
}
