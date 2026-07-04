// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perfcontract

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// repoRoot resolves the repository root from this test file's location
// (go/internal/perfcontract -> three levels up), so the doc files can be read
// regardless of the working directory the test runs from.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

func readDoc(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read doc %s: %v", rel, err)
	}
	return string(b)
}

// TestPerformanceContractMatchesDocs is the B-5 (#3798) lockstep gate. It reads
// each published performance doc and asserts every threshold is still present
// verbatim and consistent with its in-code value. A doc edit that changes a
// number (or a code edit that diverges from the doc) fails CI here — the
// documented contract and the code cannot silently drift.
func TestPerformanceContractMatchesDocs(t *testing.T) {
	root := repoRoot(t)
	docCache := map[string]string{}
	doc := func(rel string) string {
		if _, ok := docCache[rel]; !ok {
			docCache[rel] = readDoc(t, root, rel)
		}
		return docCache[rel]
	}

	thresholds := ContractThresholds()
	if len(thresholds) == 0 {
		t.Fatal("no contract thresholds defined")
	}

	seen := map[string]bool{}
	for _, th := range thresholds {
		if seen[th.Name] {
			t.Errorf("duplicate threshold name %q", th.Name)
		}
		seen[th.Name] = true

		// 1. The token must appear inside the phrase (binds code Value's written
		//    form to the phrase we then look for in the doc).
		if !strings.Contains(th.Phrase, th.Token) {
			t.Errorf("%s: token %q not contained in phrase %q", th.Name, th.Token, th.Phrase)
		}

		// 2. The phrase must appear verbatim in the doc (the doc-reading lockstep:
		//    a missed/changed threshold in the doc fails here).
		if !strings.Contains(doc(th.Doc), th.Phrase) {
			t.Errorf("%s: phrase %q not found in %s — doc and code performance contract drifted", th.Name, th.Phrase, th.Doc)
		}

		// 3. The numeric prefix of the token must equal the in-code Value (a code
		//    edit that changes Value without the doc/token fails here).
		if got := leadingNumber(t, th.Token); got != th.Value {
			t.Errorf("%s: token %q parses to %g but Value is %g", th.Name, th.Token, got, th.Value)
		}

		if th.Enforcement != EnforcementHermeticGate && th.Enforcement != EnforcementOperatorGated {
			t.Errorf("%s: invalid enforcement %q", th.Name, th.Enforcement)
		}
	}
}

// leadingNumber parses the numeric prefix of a token like "15s", "1.10x",
// "50 ms", "0.60", or "60 seconds".
func leadingNumber(t *testing.T, token string) float64 {
	t.Helper()
	i := 0
	for i < len(token) {
		c := token[i]
		if (c >= '0' && c <= '9') || c == '.' {
			i++
			continue
		}
		break
	}
	if i == 0 {
		t.Fatalf("token %q has no numeric prefix", token)
	}
	v, err := strconv.ParseFloat(token[:i], 64)
	if err != nil {
		t.Fatalf("parse token %q: %v", token, err)
	}
	return v
}

// TestClaimLatencyWithinBudget exercises the executable claim-latency rule so the
// operator/remote run has a tested evaluator to feed real measurements into.
func TestClaimLatencyWithinBudget(t *testing.T) {
	c := ReducerClaimLatency()

	cases := []struct {
		name                               string
		baseP95, measP95, baseMax, measMax float64 // seconds
		want                               bool
	}{
		{"both unchanged", 100, 100, 200, 200, true},
		{"p95 within 10%, max flat", 100, 109, 200, 200, true},
		{"p95 exactly 10%, max flat", 100, 110, 200, 200, true},
		{"p95 over 10% multiplier", 100, 111, 200, 200, false},
		// The bug codex caught: p95 fine, but max grew beyond +60s must fail.
		{"p95 fine, max +59s passes", 100, 100, 200, 259, true},
		{"p95 fine, max +60s passes", 100, 100, 200, 260, true},
		{"p95 fine, max +61s fails", 100, 100, 200, 261, false},
		{"p95 over and max over", 100, 200, 200, 400, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.WithinBudget(
				durSeconds(tc.baseP95), durSeconds(tc.measP95),
				durSeconds(tc.baseMax), durSeconds(tc.measMax),
			)
			if got != tc.want {
				t.Errorf("WithinBudget(p95 %vs->%vs, max %vs->%vs) = %v, want %v",
					tc.baseP95, tc.measP95, tc.baseMax, tc.measMax, got, tc.want)
			}
		})
	}
}

func durSeconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}
