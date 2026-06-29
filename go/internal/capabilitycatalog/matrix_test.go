// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadMatrixMergesMainAndFragments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "capability-matrix.v1.yaml"), `
version: v1
capabilities:
  - capability: code_search.exact_symbol
    tools: [find_code]
    profiles:
      local_lightweight: {status: supported, max_truth_level: exact, required_runtime: local_host, verification: [{go_test: ./internal/query}]}
      local_authoritative: {status: unsupported, max_truth_level: unsupported, required_runtime: local_host_plus_graph, p95_latency_ms: null, max_scope_size: none, verification: [{go_test: ./internal/query}]}
      production: {status: supported, max_truth_level: exact, required_runtime: deployed_services, p95_latency_ms: 800, max_scope_size: multi_repo_platform, verification: [{remote_validation: prod-code-search-exact}]}
`)
	writeFile(t, filepath.Join(dir, "capability-matrix", "extra.v1.yaml"), `
capabilities:
  - capability: platform_metrics.timeseries
    tools: [get_metrics_time_series]
    profiles:
      production: {status: supported, max_truth_level: derived, required_runtime: deployed_services, verification: [{go_test: ./internal/query}]}
`)

	matrix, err := LoadMatrix(dir)
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	if got, want := len(matrix.Capabilities), 2; got != want {
		t.Fatalf("capabilities = %d, want %d", got, want)
	}

	first := matrix.Capabilities[0]
	if first.Capability != "code_search.exact_symbol" {
		t.Fatalf("capabilities not sorted: first = %q", first.Capability)
	}
	if got := first.Profiles["production"].Verification; len(got) != 1 || got[0].Kind != "remote_validation" || got[0].Ref != "prod-code-search-exact" {
		t.Fatalf("verification parse = %+v", got)
	}
	if got := first.Profiles["production"].P95LatencyMS; got == nil || *got != 800 {
		t.Fatalf("production p95 latency = %v, want 800", got)
	}
	if got := first.Profiles["production"].MaxScopeSize; got != "multi_repo_platform" {
		t.Fatalf("production max scope = %q, want multi_repo_platform", got)
	}
	if got := first.Profiles["local_authoritative"].P95LatencyMS; got != nil {
		t.Fatalf("unsupported p95 latency = %v, want nil", *got)
	}
	if first.Tools[0] != "find_code" {
		t.Fatalf("tools = %v", first.Tools)
	}
}

func TestLoadMatrixRejectsDuplicateCapability(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "capability-matrix.v1.yaml"), `
capabilities:
  - capability: dup.capability
    tools: [x]
    profiles: {production: {status: supported, max_truth_level: exact}}
`)
	writeFile(t, filepath.Join(dir, "capability-matrix", "frag.v1.yaml"), `
capabilities:
  - capability: dup.capability
    tools: [y]
    profiles: {production: {status: supported, max_truth_level: exact}}
`)

	if _, err := LoadMatrix(dir); err == nil {
		t.Fatal("LoadMatrix() error = nil, want duplicate error")
	}
}

func TestLoadMatrixReadsRealSpecs(t *testing.T) {
	t.Parallel()

	matrix, err := LoadMatrix(repoSpecsDir(t))
	if err != nil {
		t.Fatalf("LoadMatrix(real specs): %v", err)
	}
	if len(matrix.Capabilities) < 90 {
		t.Fatalf("real specs capability count = %d, want >= 90", len(matrix.Capabilities))
	}
	for _, capability := range matrix.Capabilities {
		if capability.Capability == "" {
			t.Fatal("empty capability id in real specs")
		}
		if len(capability.Profiles) == 0 {
			t.Fatalf("capability %q has no profiles", capability.Capability)
		}
	}
}

func TestProfileClaimsSupport(t *testing.T) {
	cases := []struct {
		name    string
		profile MatrixProfile
		want    bool
	}{
		{"explicit supported", MatrixProfile{Status: "supported"}, true},
		{"explicit experimental", MatrixProfile{Status: "experimental"}, true},
		{"explicit unsupported", MatrixProfile{Status: "unsupported"}, false},
		// Truth-ceiling-only rows omit status; effectiveStatus infers supported
		// from a non-unsupported ceiling, so ProfileClaimsSupport must agree.
		{"blank status with truth ceiling", MatrixProfile{MaxTruthLevel: "exact"}, true},
		{"blank status no ceiling", MatrixProfile{}, false},
		{"blank status unsupported ceiling", MatrixProfile{MaxTruthLevel: "unsupported"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProfileClaimsSupport(tc.profile); got != tc.want {
				t.Errorf("ProfileClaimsSupport(%+v) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}
