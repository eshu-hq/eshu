// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package quality

import (
	"testing"
)

func TestRequestNormalizeDefaultsCheckAndFloors(t *testing.T) {
	var req Request
	req.Normalize()
	if req.Check != CheckRefactor {
		t.Fatalf("Check = %q, want default refactor sweep", req.Check)
	}
	if req.Limit != DefaultLimit || req.MinLines != DefaultLines || req.MinArguments != DefaultArgs || req.MinComplexity != DefaultComplexity {
		t.Fatalf("floors = %+v, want handler bounds", req)
	}
}

func TestRequestNormalizeComplexityCheckFloor(t *testing.T) {
	req := Request{Check: CheckComplex}
	req.Normalize()
	if req.MinComplexity != 1 {
		t.Fatalf("MinComplexity = %d, want 1 for the complexity check", req.MinComplexity)
	}
}

func TestSupportedCheckNamesSweep(t *testing.T) {
	for _, check := range []string{CheckComplex, CheckLength, CheckArgs, CheckRefactor} {
		if !SupportedCheck(check) {
			t.Fatalf("SupportedCheck(%q) = false, want true", check)
		}
	}
	if SupportedCheck("halstead") {
		t.Fatal("SupportedCheck(halstead) = true, want false")
	}
}

func TestTrimResultsBoundsAndReports(t *testing.T) {
	rows := []map[string]any{{"a": 1}, {"a": 2}, {"a": 3}}
	kept, truncated := TrimResults(rows, 3)
	if truncated || len(kept) != 3 {
		t.Fatalf("at limit: truncated=%v len=%d, want false/3", truncated, len(kept))
	}
	kept, truncated = TrimResults(rows, 2)
	if !truncated || len(kept) != 2 {
		t.Fatalf("past limit: truncated=%v len=%d, want true/2", truncated, len(kept))
	}
}
