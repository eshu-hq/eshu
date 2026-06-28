// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestFrameworkAPIEndpointSignalsPreserveRouteEntryMethodPairs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		entries any
	}{
		{
			name: "json decoded entries",
			entries: []any{
				map[string]any{"method": "ANY", "path": "/payments", "handler": "ServePayments"},
				map[string]any{"method": "GET", "path": "/status", "handler": "ServeStatus"},
				map[string]any{"method": "ANY", "path": "/health", "handler": "ServeHealth"},
			},
		},
		{
			name: "parser emitted entries",
			entries: []map[string]string{
				{"method": "ANY", "path": "/payments", "handler": "ServePayments"},
				{"method": "GET", "path": "/status", "handler": "ServeStatus"},
				{"method": "ANY", "path": "/health", "handler": "ServeHealth"},
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			signals := frameworkAPIEndpointSignals("cmd/server/routes.go", map[string]any{
				"framework_semantics": map[string]any{
					"frameworks": []any{"net_http"},
					"net_http": map[string]any{
						"route_methods": []any{"ANY", "GET"},
						"route_paths":   []any{"/payments", "/status", "/health"},
						"route_entries": tc.entries,
					},
				},
			})

			if got, want := len(signals), 3; got != want {
				t.Fatalf("len(signals) = %d, want %d: %#v", got, want, signals)
			}
			methodsByPath := map[string][]string{}
			for _, signal := range signals {
				methodsByPath[signal.Path] = signal.Methods
			}
			assertEndpointMethods(t, methodsByPath, "/payments", []string{"any"})
			assertEndpointMethods(t, methodsByPath, "/status", []string{"get"})
			assertEndpointMethods(t, methodsByPath, "/health", []string{"any"})
		})
	}
}

func TestFrameworkAPIEndpointSignalsPreferNextJSRouteEntries(t *testing.T) {
	t.Parallel()

	signals := frameworkAPIEndpointSignals("src/app/api/accounts/[id]/route.ts", map[string]any{
		"framework_semantics": map[string]any{
			"frameworks": []any{"nextjs"},
			"nextjs": map[string]any{
				"module_kind":    "route",
				"route_segments": []any{"api", "accounts", "[id]"},
				"route_verbs":    []any{"GET", "POST"},
				"route_entries": []any{
					map[string]any{"method": "GET", "path": "/api/accounts/[id]", "handler": "GET"},
					map[string]any{"method": "POST", "path": "/api/accounts/[id]", "handler": "POST"},
				},
			},
		},
	})

	if got, want := len(signals), 2; got != want {
		t.Fatalf("len(signals) = %d, want %d: %#v", got, want, signals)
	}
	methodsByPath := map[string][]string{}
	for _, signal := range signals {
		methodsByPath[signal.Path] = append(methodsByPath[signal.Path], signal.Methods...)
	}
	assertEndpointMethods(t, methodsByPath, "/api/accounts/[id]", []string{"get", "post"})
}

func assertEndpointMethods(t *testing.T, got map[string][]string, path string, want []string) {
	t.Helper()

	methods, ok := got[path]
	if !ok {
		t.Fatalf("methodsByPath missing %q: %#v", path, got)
	}
	if len(methods) != len(want) {
		t.Fatalf("methodsByPath[%q] = %#v, want %#v", path, methods, want)
	}
	for i := range methods {
		if methods[i] != want[i] {
			t.Fatalf("methodsByPath[%q] = %#v, want %#v", path, methods, want)
		}
	}
}

func BenchmarkFrameworkAPIEndpointSignalsRouteEntries(b *testing.B) {
	entries := make([]any, 0, 100)
	for i := 0; i < 100; i++ {
		entries = append(entries, map[string]any{
			"method": "GET",
			"path":   "/route",
		})
	}
	fileData := map[string]any{
		"framework_semantics": map[string]any{
			"frameworks": []any{"net_http"},
			"net_http": map[string]any{
				"route_entries": entries,
			},
		},
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		signals := frameworkAPIEndpointSignals("cmd/server/routes.go", fileData)
		if len(signals) != len(entries) {
			b.Fatalf("len(signals) = %d, want %d", len(signals), len(entries))
		}
	}
}
