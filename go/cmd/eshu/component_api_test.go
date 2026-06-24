// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestComponentInventoryCommandReadsCanonicalAPIEnvelope(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v0/component-extensions"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("limit"), "1"; got != want {
			t.Fatalf("limit query = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Accept"), eshuEnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"schema_version": "eshu.component_extensions.v1",
				"status": "available",
				"components": [{
					"id": "dev.eshu.collector.aws",
					"version": "0.1.0",
					"manifest_digest": "sha256:abc",
					"states": ["installed", "enabled", "claim_capable"],
					"activations": [{
						"instance_id": "prod-aws",
						"mode": "scheduled",
						"claims_enabled": true,
						"config_handle": "component-config:abc"
					}]
				}],
				"count": 1
			},
			"truth": {
				"level": "exact",
				"capability": "component_extensions.inventory",
				"profile": "production",
				"freshness": {"state": "fresh"}
			},
			"error": null
		}`))
	}))
	t.Cleanup(server.Close)

	out := &bytes.Buffer{}
	cmd := newComponentAPICommandForTest(out)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("set service-url: %v", err)
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set json: %v", err)
	}
	if err := cmd.Flags().Set("limit", "1"); err != nil {
		t.Fatalf("set limit: %v", err)
	}

	if err := runComponentInventory(cmd, nil); err != nil {
		t.Fatalf("runComponentInventory() error = %v, want nil", err)
	}

	raw := out.String()
	if strings.Contains(raw, "manifest_path") || strings.Contains(raw, "config_path") {
		t.Fatalf("inventory output leaked private path fields: %s", raw)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output=%s", err, raw)
	}
	if envelope["truth"] == nil {
		t.Fatalf("truth envelope missing from output: %s", raw)
	}
}

func TestComponentDiagnosticsCommandReadsComponentDrilldown(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v0/component-extensions/dev.eshu.collector.aws/diagnostics"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"schema_version": "eshu.component_extensions.v1",
				"status": "available",
				"component": {
					"id": "dev.eshu.collector.aws",
					"version": "0.1.0",
					"states": ["installed", "failed"],
					"diagnostics": {
						"policy_allowed": false,
						"policy_code": "revoked_package",
						"policy_reason": "component is revoked"
					}
				}
			},
			"truth": {
				"level": "exact",
				"capability": "component_extensions.diagnostics",
				"profile": "production",
				"freshness": {"state": "fresh"}
			},
			"error": null
		}`))
	}))
	t.Cleanup(server.Close)

	out := &bytes.Buffer{}
	cmd := newComponentAPICommandForTest(out)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("set service-url: %v", err)
	}

	if err := runComponentDiagnostics(cmd, []string{"dev.eshu.collector.aws"}); err != nil {
		t.Fatalf("runComponentDiagnostics() error = %v, want nil", err)
	}
	if got := out.String(); !strings.Contains(got, "dev.eshu.collector.aws@0.1.0 states=installed,failed") {
		t.Fatalf("diagnostics text = %q, want component state summary", got)
	}
}

func TestRenderComponentAPISummaryShowsTruncationMetadata(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	envelope := componentAPIEnvelope{
		Data: map[string]any{
			"components":  []any{},
			"count":       float64(1),
			"total_count": float64(2),
			"limit":       float64(1),
			"truncated":   true,
		},
	}

	if err := renderComponentAPISummary(out, envelope); err != nil {
		t.Fatalf("renderComponentAPISummary() error = %v, want nil", err)
	}
	if got, want := out.String(), "Component extensions: 1 of 2 (limit=1, truncated=true)\n"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func newComponentAPICommandForTest(out *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	addComponentAPIFlags(cmd)
	cmd.Flags().Int("limit", componentInventoryDefaultLimit, "Maximum number of component rows to return")
	return cmd
}
