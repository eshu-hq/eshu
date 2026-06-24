// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesTypeScriptInterfaceTypedReceiver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "plugins.ts")
	writeReducerTestFile(t, filePath, `interface Transport {
  request(): Promise<Response>;
}

class FetchTransport implements Transport {
  request() {
    return Promise.resolve(new Response());
  }
}

class AuditSink {
  request() {
    return Promise.resolve(new Response());
  }
}

export function run(transport: Transport) {
  return transport.request();
}
`)

	payload := parsedTypeScriptResolverPayload(t, repoRoot, filePath)
	assignReducerTestInterfaceUID(t, payload, "Transport", "content-entity:transport")
	assignReducerTestClassUID(t, payload, "FetchTransport", "content-entity:fetch-transport")
	assignReducerTestClassUID(t, payload, "AuditSink", "content-entity:audit-sink")
	assignReducerTestFunctionUID(t, payload, "run", "content-entity:run")
	assignReducerTestClassFunctionUID(t, payload, "FetchTransport", "request", "content-entity:fetch-request")
	assignReducerTestClassFunctionUID(t, payload, "AuditSink", "request", "content-entity:audit-request")

	_, rows := ExtractCodeCallRows([]facts.Envelope{typeScriptResolverEnvelope(t, repoRoot, filePath, payload)})

	if got := resolutionMethodForCallee(t, rows, "content-entity:fetch-request"); got != codeprovenance.MethodTypeInferred {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodTypeInferred)
	}
	assertReducerNoCodeCallRow(t, rows, "content-entity:run", "content-entity:audit-request")
}

func TestExtractCodeCallRowsLeavesTypeScriptAmbiguousInterfaceReceiverUnresolved(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "plugins.ts")
	writeReducerTestFile(t, filePath, `interface Transport {
  request(): Promise<Response>;
}

class FetchTransport implements Transport {
  request() {
    return Promise.resolve(new Response());
  }
}

class MemoryTransport implements Transport {
  request() {
    return Promise.resolve(new Response());
  }
}

export function run(transport: Transport) {
  return transport.request();
}
`)

	payload := parsedTypeScriptResolverPayload(t, repoRoot, filePath)
	assignReducerTestInterfaceUID(t, payload, "Transport", "content-entity:transport")
	assignReducerTestClassUID(t, payload, "FetchTransport", "content-entity:fetch-transport")
	assignReducerTestClassUID(t, payload, "MemoryTransport", "content-entity:memory-transport")
	assignReducerTestFunctionUID(t, payload, "run", "content-entity:run")
	assignReducerTestClassFunctionUID(t, payload, "FetchTransport", "request", "content-entity:fetch-request")
	assignReducerTestClassFunctionUID(t, payload, "MemoryTransport", "request", "content-entity:memory-request")

	_, rows := ExtractCodeCallRows([]facts.Envelope{typeScriptResolverEnvelope(t, repoRoot, filePath, payload)})

	assertReducerNoCodeCallRow(t, rows, "content-entity:run", "content-entity:fetch-request")
	assertReducerNoCodeCallRow(t, rows, "content-entity:run", "content-entity:memory-request")
}

func TestExtractCodeCallRowsLeavesTypeScriptExternalInterfaceReceiverUnresolved(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "plugins.ts")
	writeReducerTestFile(t, filePath, `import type { Transport } from "@example/contracts";

class FetchTransport implements Transport {
  request() {
    return Promise.resolve(new Response());
  }
}

export function run(transport: Transport) {
  return transport.request();
}
`)

	payload := parsedTypeScriptResolverPayload(t, repoRoot, filePath)
	assignReducerTestClassUID(t, payload, "FetchTransport", "content-entity:fetch-transport")
	assignReducerTestFunctionUID(t, payload, "run", "content-entity:run")
	assignReducerTestClassFunctionUID(t, payload, "FetchTransport", "request", "content-entity:fetch-request")

	_, rows := ExtractCodeCallRows([]facts.Envelope{typeScriptResolverEnvelope(t, repoRoot, filePath, payload)})

	assertReducerNoCodeCallRow(t, rows, "content-entity:run", "content-entity:fetch-request")
}

func parsedTypeScriptResolverPayload(t *testing.T, repoRoot string, filePath string) map[string]any {
	t.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}
	return payload
}

func typeScriptResolverEnvelope(t *testing.T, repoRoot string, filePath string, payload map[string]any) facts.Envelope {
	t.Helper()

	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          "repo-ts",
			"relative_path":    reducerTestRelativePath(t, repoRoot, filePath),
			"parsed_file_data": payload,
		},
	}
}

func assignReducerTestClassFunctionUID(t *testing.T, payload map[string]any, className string, name string, uid string) {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("payload functions = %T, want []map[string]any", payload["functions"])
	}
	for i := range functions {
		if functions[i]["name"] == name && functions[i]["class_context"] == className {
			functions[i]["uid"] = uid
			payload["functions"] = functions
			return
		}
	}
	t.Fatalf("payload missing %s.%s in %#v", className, name, functions)
}
