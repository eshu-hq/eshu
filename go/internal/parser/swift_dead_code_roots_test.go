package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathSwiftEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Sources", "App", "App.swift")
	writeTestFile(
		t,
		sourcePath,
		`import SwiftUI
import UIKit
import Vapor
import XCTest

@main
struct DemoApp: App {
    public var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}

public protocol Runnable {
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

@Test
func swiftTestingRunsFromRunner() {}

func main() {}

private func unusedCleanupCandidate() {}
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

	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "structs", "DemoApp"), "dead_code_root_kinds", "swift.main_type")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "structs", "DemoApp"), "dead_code_root_kinds", "swift.swiftui_app_type")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "variables", "body"), "dead_code_root_kinds", "swift.swiftui_body")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "protocols", "Runnable"), "dead_code_root_kinds", "swift.protocol_type")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Runnable"), "dead_code_root_kinds", "swift.protocol_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "init", "Worker"), "dead_code_root_kinds", "swift.constructor")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "start", "Worker"), "dead_code_root_kinds", "swift.override_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Worker"), "dead_code_root_kinds", "swift.protocol_implementation_method")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "classes", "AppDelegate"), "dead_code_root_kinds", "swift.ui_application_delegate_type")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "application", "AppDelegate"), "dead_code_root_kinds", "swift.ui_application_delegate_method")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "health"), "dead_code_root_kinds", "swift.vapor_route_handler")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "testRunsFromXCTest", "ServiceTests"), "dead_code_root_kinds", "swift.xctest_method")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "swiftTestingRunsFromRunner"), "dead_code_root_kinds", "swift.swift_testing_method")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "main"), "dead_code_root_kinds", "swift.main_function")

	for _, tc := range []struct {
		name         string
		classContext string
	}{
		{name: "helper", classContext: "Worker"},
		{name: "unusedCleanupCandidate"},
	} {
		function := assertFunctionByName(t, got, tc.name)
		if tc.classContext != "" {
			function = assertFunctionByNameAndClass(t, got, tc.name, tc.classContext)
		}
		if function["dead_code_root_kinds"] != nil {
			t.Fatalf("%s.%s dead_code_root_kinds = %#v, want nil", tc.classContext, tc.name, function["dead_code_root_kinds"])
		}
	}
}
