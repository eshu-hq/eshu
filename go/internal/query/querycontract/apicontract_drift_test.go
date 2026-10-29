// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// goldenDir locates the committed SDK wire goldens. The test runs with the
// package directory as its working directory, the same as every other test
// that reads repo files by relative path (see envregistry's coverage gate).
func goldenDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "sdk", "go", "apicontract", "testdata", "golden")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("golden dir %s: %v (run the S2 generator or sync the worktree)", dir, err)
	}
	return dir
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(goldenDir(t), name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return raw
}

func marshalGolden(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return append(raw, '\n')
}

// TestApicontractGoldensMatchLiveTypes locks the published SDK wire goldens
// to the live contract types. A field rename, retag, new enum member default,
// or dropped omitempty in querycontract fails here until the SDK goldens and
// the sdk/go/apicontract shapes move in the same change. The SDK module's own
// parity tests lock the other direction (goldens to SDK shapes) without
// importing this package, so the two one-sided equalities compose into a
// machine-checked transitivity proof.
func TestApicontractGoldensMatchLiveTypes(t *testing.T) {
	t.Parallel()
	truth := &TruthEnvelope{
		Level:      TruthLevelDerived,
		Capability: "search_code",
		Profile:    ProfileProduction,
		Basis:      TruthBasisHybrid,
		Backend:    GraphBackendNeo4j,
		Freshness: TruthFreshness{
			State:      FreshnessStale,
			ObservedAt: "2026-10-29T00:00:00Z",
			Detail:     "reducer backlog",
			Cause:      FreshnessCauseReducerBacklog,
			NextCheck: &FreshnessNextCheck{
				Tool:   "get_reducer_status",
				Route:  "/api/v0/admin/reducer/status",
				Reason: "drain state",
				Params: map[string]string{"scope": "repo"},
			},
		},
		Reason:    "hybrid evidence",
		Truncated: true,
		Omissions: []TruthOmission{
			{Section: "citations", Detail: "handles", Total: 7},
		},
	}
	cases := map[string]ResponseEnvelope{
		"envelope_truth.json": {
			Data:  map[string]any{"truncated": true, "rows": []any{"a", "b"}},
			Truth: truth,
		},
		"envelope_error.json": {
			Error: &ErrorEnvelope{
				Code:       ErrorCodeNotFound,
				Message:    "no such service",
				Capability: "service_story",
				Profiles: &ErrorProfiles{
					Current:  ProfileProduction,
					Required: ProfileLocalAuthoritative,
				},
				Details: map[string]any{"service": "api"},
			},
		},
	}
	for name, envelope := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got, want := marshalGolden(t, envelope), readGolden(t, name); string(got) != string(want) {
				t.Fatalf("live types drifted from golden %s:\n got: %s\nwant: %s", name, got, want)
			}
		})
	}
}
