// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package csharp_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathCSharpEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Service.cs")
	parsertest.WriteFile(
		t,
		sourcePath,
		`using System.Runtime.Serialization;
using System.Threading;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Mvc;
using Microsoft.Extensions.Hosting;
using Xunit;

public interface IJob {
    void Run();
}

public class ReportJob : IJob {
    public ReportJob() {}
    public void Run() {}
    public void Run(int retries) {}
    private void Helper() {}
}

public abstract class BaseJob {
    public abstract void Execute();
}

public sealed class ScheduledJob : BaseJob {
    public override void Execute() {}
}

public sealed class OrdersController : ControllerBase {
    [HttpGet]
    public string Get() => "ok";
    [NonAction]
    public string HelperAction() => "skip";
    private string Helper() => "private";
}

public sealed class Worker : BackgroundService {
    protected override Task ExecuteAsync(CancellationToken stoppingToken) {
        return Task.CompletedTask;
    }
}

public sealed class ServiceTests {
    [Fact]
    public void RunsFromTestRunner() {}
    [Trait("kind", "unit"), Fact]
    public void RunsFromMultipleAttributes() {}
}

public sealed class SerializedState {
    [OnDeserialized]
    private void Restore(StreamingContext context) {}
}

public static class Program {
    public static void Main(string[] args) {}
}

public interface Order {
    void Handle();
}

public interface IHandler<T> {}

public sealed class Processor : IHandler<Order> {
    public void Handle() {}
}

public sealed class TextOnlyRoots {
    public void MentionsFact() {
        var marker = "[Fact]";
    }
    public void MentionsOverride() {
        var marker = "override";
    }
    public void Main() {}
}

public static class InvalidReturnProgram {
    public static string Main() => "";
}

public static class InvalidParameterProgram {
    public static void Main(int bad) {}
}

public static class AsyncProgram {
    public static Task<int> Main(string[] args) => Task.FromResult(0);
}

public static class FullyQualifiedTaskProgram {
    public static System.Threading.Tasks.Task Main(string[] args) => System.Threading.Tasks.Task.CompletedTask;
}

namespace HostedNamespace {
    public sealed class Worker : BackgroundService {
        protected override Task ExecuteAsync(CancellationToken stoppingToken) {
            return Task.CompletedTask;
        }
    }
}

namespace PlainNamespace {
    public sealed class Worker {
        public void ExecuteAsync(CancellationToken stoppingToken) {
            var marker = "not hosted";
        }
    }
}
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

	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Run", "IJob"), "dead_code_root_kinds", "csharp.interface_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "ReportJob", "ReportJob"), "dead_code_root_kinds", "csharp.constructor")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Run", "ReportJob"), "dead_code_root_kinds", "csharp.interface_implementation_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Execute", "ScheduledJob"), "dead_code_root_kinds", "csharp.override_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Get", "OrdersController"), "dead_code_root_kinds", "csharp.aspnet_controller_action")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "ExecuteAsync", "Worker"), "dead_code_root_kinds", "csharp.hosted_service_entrypoint")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "RunsFromTestRunner", "ServiceTests"), "dead_code_root_kinds", "csharp.test_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "RunsFromMultipleAttributes", "ServiceTests"), "dead_code_root_kinds", "csharp.test_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Restore", "SerializedState"), "dead_code_root_kinds", "csharp.serialization_callback")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Main", "Program"), "dead_code_root_kinds", "csharp.main_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Main", "AsyncProgram"), "dead_code_root_kinds", "csharp.main_method")
	parsertest.AssertStringSliceContains(t, assertFunctionBySourceContains(t, got, "System.Threading.Tasks.Task Main"), "dead_code_root_kinds", "csharp.main_method")
	parsertest.AssertStringSliceContains(t, assertFunctionBySourceContains(t, got, "return Task.CompletedTask;"), "dead_code_root_kinds", "csharp.hosted_service_entrypoint")
	if helper := parsertest.AssertFunctionByNameAndClass(t, got, "Helper", "ReportJob"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("ReportJob.Helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
	if helper := parsertest.AssertFunctionByNameAndClass(t, got, "Helper", "OrdersController"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("OrdersController.Helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
	for _, tc := range []struct {
		name           string
		classContext   string
		sourceContains string
	}{
		{name: "HelperAction", classContext: "OrdersController"},
		{name: "Handle", classContext: "Processor"},
		{name: "MentionsFact", classContext: "TextOnlyRoots"},
		{name: "MentionsOverride", classContext: "TextOnlyRoots"},
		{name: "Main", classContext: "TextOnlyRoots"},
		{name: "Main", classContext: "InvalidReturnProgram"},
		{name: "Main", classContext: "InvalidParameterProgram"},
		{name: "Run", classContext: "ReportJob", sourceContains: "Run(int retries)"},
		{name: "ExecuteAsync", classContext: "Worker", sourceContains: "not hosted"},
	} {
		function := parsertest.AssertFunctionByNameAndClass(t, got, tc.name, tc.classContext)
		if tc.sourceContains != "" {
			function = assertFunctionBySourceContains(t, got, tc.sourceContains)
		}
		if function["dead_code_root_kinds"] != nil {
			t.Fatalf("%s.%s dead_code_root_kinds = %#v, want nil", tc.classContext, tc.name, function["dead_code_root_kinds"])
		}
	}
}

func TestDefaultEngineParsePathCSharpDeadCodeFixtureExpectedRoots(t *testing.T) {
	t.Parallel()

	repoRoot := csharpFixturePath(t, "deadcode", "csharp")
	sourcePath := csharpFixturePath(t, "deadcode", "csharp", "Fixture.cs")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Main", "Program"), "dead_code_root_kinds", "csharp.main_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Get", "PublicController"), "dead_code_root_kinds", "csharp.aspnet_controller_action")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "ExecuteAsync", "Worker"), "dead_code_root_kinds", "csharp.hosted_service_entrypoint")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Run", "IJob"), "dead_code_root_kinds", "csharp.interface_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Run", "ReportJob"), "dead_code_root_kinds", "csharp.interface_implementation_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "ExercisedByTestRunner", "FixtureTests"), "dead_code_root_kinds", "csharp.test_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Restore", "SerializationHooks"), "dead_code_root_kinds", "csharp.serialization_callback")
	if helper := parsertest.AssertBucketItemByName(t, got, "functions", "UnusedCleanupCandidate"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("UnusedCleanupCandidate dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
	if helper := parsertest.AssertFunctionByNameAndClass(t, got, "InternalHelper", "PublicController"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("PublicController.InternalHelper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func assertFunctionBySourceContains(t *testing.T, payload map[string]any, sourceContains string) map[string]any {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, function := range functions {
		source, _ := function["source"].(string)
		if strings.Contains(source, sourceContains) {
			return function
		}
	}
	t.Fatalf("functions missing source containing %q in %#v", sourceContains, functions)
	return nil
}
