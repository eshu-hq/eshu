// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestGHSAWindowGoldenVectorsMintAndParse asserts the shared golden file
// against the scope grammar itself: every planned vector's bounds mint
// exactly its golden scope id and parse back to the same bounds. The
// coordinator planner test asserts the same file against the planner, and
// the satellite asserts its pinned copy, so the three cannot drift.
func TestGHSAWindowGoldenVectorsMintAndParse(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("testdata", "ghsa_window_golden.json"))
	if err != nil {
		t.Fatalf("read ghsa golden vectors: %v", err)
	}
	var vectors []struct {
		Name          string `json:"name"`
		AdvisoryType  string `json:"advisory_type"`
		WantScope     string `json:"want_scope"`
		WantStartUnix int64  `json:"want_start_unix_us"`
		WantEndUnix   int64  `json:"want_end_unix_us"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("decode ghsa golden vectors: %v", err)
	}
	if len(vectors) == 0 {
		t.Fatal("ghsa golden vectors are empty")
	}
	for _, vector := range vectors {
		if vector.WantScope == "" {
			continue // hold vector: no scope is minted.
		}
		start := time.UnixMicro(vector.WantStartUnix).UTC()
		end := time.UnixMicro(vector.WantEndUnix).UTC()
		if got := GHSAWindowScopeID(vector.AdvisoryType, start, end); got != vector.WantScope {
			t.Errorf("%s: mint = %q, want golden %q", vector.Name, got, vector.WantScope)
		}
		advisoryType, gotStart, gotEnd, ok := ParseGHSAWindowScope(vector.WantScope)
		if !ok || advisoryType != vector.AdvisoryType || !gotStart.Equal(start) || !gotEnd.Equal(end) {
			t.Errorf("%s: parse(%q) = (%q, %v, %v, %t)", vector.Name, vector.WantScope, advisoryType, gotStart, gotEnd, ok)
		}
	}
}
