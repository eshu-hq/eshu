// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoEmitsDeadCodeRegistrationRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "registrations.go")
	writeTestFile(
		t,
		filePath,
		`package roots

import (
	rootcobra "github.com/spf13/cobra"
	handler "net/http"
)

func ServeDirect(w handler.ResponseWriter, r *handler.Request) {}
func ServeMuxed(w handler.ResponseWriter, r *handler.Request) {}
func ServeStatus(w handler.ResponseWriter, r *handler.Request) {}
func runDirect(cmd *rootcobra.Command, args []string) {}
func runAssigned(cmd *rootcobra.Command, args []string) {}

func wire() {
	handler.HandleFunc("/payments", ServeDirect)
	handler.HandleFunc("GET /status", ServeStatus)
	mux := handler.NewServeMux()
	mux.Handle("/health", handler.HandlerFunc(ServeMuxed))
	rootCmd := &rootcobra.Command{Run: runDirect}
	rootCmd.RunE = runAssigned
	_ = rootCmd
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "ServeDirect"), "dead_code_root_kinds", "go.net_http_handler_registration")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "ServeMuxed"), "dead_code_root_kinds", "go.net_http_handler_registration")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "ServeStatus"), "dead_code_root_kinds", "go.net_http_handler_registration")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "runDirect"), "dead_code_root_kinds", "go.cobra_run_registration")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "runAssigned"), "dead_code_root_kinds", "go.cobra_run_registration")
	assertFrameworksEqual(t, got, "net_http")
	assertNestedStringSliceEqual(t, got, "net_http", "route_methods", []string{"ANY", "GET"})
	assertNestedStringSliceEqual(t, got, "net_http", "route_paths", []string{"/payments", "/status", "/health"})
	assertNestedRouteEntriesEqual(t, got, "net_http", []map[string]string{
		{"method": "ANY", "path": "/payments", "handler": "ServeDirect"},
		{"method": "GET", "path": "/status", "handler": "ServeStatus"},
		{"method": "ANY", "path": "/health", "handler": "ServeMuxed"},
	})
}

func TestDefaultEngineParsePathGoIgnoresUnknownHandleFuncReceivers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "unknown_mux.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type fakeMux struct{}

func (m *fakeMux) HandleFunc(_ string, _ func()) {}

func maybeHTTP() {}

func wire() {
	mux := &fakeMux{}
	mux.HandleFunc("/payments", maybeHTTP)
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	functionItem := assertFunctionByName(t, got, "maybeHTTP")
	if semantics, ok := got["framework_semantics"]; ok {
		t.Fatalf("framework_semantics = %#v, want absent for unknown mux receiver", semantics)
	}
	assertParserStringSliceContains(t, functionItem, "dead_code_root_kinds", "go.function_value_reference")
	assertParserStringSliceNotContains(
		t,
		functionItem,
		"dead_code_root_kinds",
		"go.net_http_handler_registration",
	)
}

func TestDefaultEngineParsePathGoEmitsMixedCaseServeMuxRouteEntry(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "mixed_case_mux.go")
	writeTestFile(
		t,
		filePath,
		`package roots

import handler "net/http"

func ServeReady(w handler.ResponseWriter, r *handler.Request) {}

func wire() {
	apiMux := handler.NewServeMux()
	apiMux.HandleFunc("GET /ready", ServeReady)
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "ServeReady"), "dead_code_root_kinds", "go.net_http_handler_registration")
	assertNestedRouteEntriesEqual(t, got, "net_http", []map[string]string{
		{"method": "GET", "path": "/ready", "handler": "ServeReady"},
	})
}

func assertParserStringSliceContains(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", field, item[field])
	}
	for _, value := range got {
		if value == want {
			return
		}
	}
	t.Fatalf("%s = %#v, want to contain %#v", field, got, want)
}

func assertParserStringSliceNotContains(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		return
	}
	for _, value := range got {
		if value == want {
			t.Fatalf("%s = %#v, want not to contain %#v", field, got, want)
		}
	}
}
