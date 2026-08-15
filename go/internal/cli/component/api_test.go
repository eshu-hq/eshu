// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"bytes"
	"errors"
	"testing"
)

// TestRenderAPISummaryShowsTruncationMetadata pins the truncation header the
// inventory summary prints when the server reports paging metadata. It moved
// here from go/cmd/eshu/component_api_test.go with the renderer.
func TestRenderAPISummaryShowsTruncationMetadata(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	envelope := Envelope{
		Data: map[string]any{
			"components":  []any{},
			"count":       float64(1),
			"total_count": float64(2),
			"limit":       float64(1),
			"truncated":   true,
		},
	}

	if err := renderAPISummary(out, envelope); err != nil {
		t.Fatalf("renderAPISummary() error = %v, want nil", err)
	}
	if got, want := out.String(), "Component extensions: 1 of 2 (limit=1, truncated=true)\n"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestRenderAPISummaryRendersDrilldownRowWithPolicyDiagnostics(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	envelope := Envelope{
		Data: map[string]any{
			"component": map[string]any{
				"id":      "dev.eshu.collector.aws",
				"version": "0.1.0",
				"states":  []any{"installed", "failed"},
				"diagnostics": map[string]any{
					"policy_code":   "revoked_package",
					"policy_reason": "component is revoked",
				},
			},
		},
	}

	if err := renderAPISummary(out, envelope); err != nil {
		t.Fatalf("renderAPISummary() error = %v, want nil", err)
	}
	want := "dev.eshu.collector.aws@0.1.0 states=installed,failed\n" +
		"  policy=revoked_package reason=component is revoked\n"
	if got := out.String(); got != want {
		t.Fatalf("drilldown = %q, want %q", got, want)
	}
}

// fakeFetcher records the one request FetchInventory/FetchDiagnostics make.
type fakeFetcher struct {
	path string
	err  error
}

func (f *fakeFetcher) GetEnvelope(path string, _ any) error {
	f.path = path
	return f.err
}

func TestFetchInventoryDefaultsAndSendsLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		limit    int
		wantPath string
	}{
		{name: "zero limit falls back to the default", limit: 0, wantPath: "/api/v0/component-extensions?limit=100"},
		{name: "explicit limit is sent", limit: 7, wantPath: "/api/v0/component-extensions?limit=7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fetcher := &fakeFetcher{}
			if _, err := FetchInventory(fetcher, tc.limit); err != nil {
				t.Fatalf("FetchInventory() error = %v, want nil", err)
			}
			if fetcher.path != tc.wantPath {
				t.Fatalf("request path = %q, want %q", fetcher.path, tc.wantPath)
			}
		})
	}
}

func TestFetchDiagnosticsEscapesComponentID(t *testing.T) {
	t.Parallel()

	fetcher := &fakeFetcher{}
	if _, err := FetchDiagnostics(fetcher, "dev.eshu/../../etc"); err != nil {
		t.Fatalf("FetchDiagnostics() error = %v, want nil", err)
	}
	want := "/api/v0/component-extensions/dev.eshu%2F..%2F..%2Fetc/diagnostics"
	if fetcher.path != want {
		t.Fatalf("request path = %q, want %q", fetcher.path, want)
	}
}

func TestFetchInventoryReturnsTransportErrorUnchanged(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("request failed: connection refused")
	fetcher := &fakeFetcher{err: transportErr}
	_, err := FetchInventory(fetcher, 1)
	if !errors.Is(err, transportErr) {
		t.Fatalf("FetchInventory() error = %v, want the transport error unchanged", err)
	}
}
