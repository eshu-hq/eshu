// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"testing"
)

// The vulnerability collector fills response_digest on every
// vulnerability.source_snapshot it emits
// (eshu-hq/eshu-collector-vulnerability-intelligence#125), so the cassette
// walker holds the field to the same digest pattern as every other digest
// field. An empty value is no longer treated as absence: it is a malformed
// digest, exactly like an empty subject_digest.
func TestResponseDigestIsHeldToTheDigestPattern(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		location string
		value    string
		want     bool
	}{
		{"an empty response_digest is malformed", "scopes[0].facts[1].payload.response_digest", "", false},
		{"an empty response_digest in another cassette shape is malformed", "payload.snapshot.response_digest", "", false},
		{"a truncated response_digest is malformed", "scopes[0].facts[1].payload.response_digest", "sha256:abc", false},
		{"a response_digest without the algorithm prefix is malformed", "scopes[0].facts[1].payload.response_digest", hex64, false},
		{"a well-formed response_digest passes", "scopes[0].facts[1].payload.response_digest", "sha256:" + hex64, true},
		{"an empty subject_digest is still a bug", "scopes[0].facts[0].payload.subject_digest", "", false},
	} {
		if !isDigestField(tc.location) {
			t.Errorf("%s: isDigestField(%q) = false, want the walker to check this field", tc.name, tc.location)
			continue
		}
		if got := digestFieldValueIsWellFormed(tc.location, tc.value); got != tc.want {
			t.Errorf("%s: digestFieldValueIsWellFormed(%q, %q) = %v, want %v", tc.name, tc.location, tc.value, got, tc.want)
		}
	}
}

const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// The walker above validates only the digest keys that exist, so on its own
// it passes when a snapshot loses its response_digest key altogether. The
// collector-recorded vulnerability cassette must carry the key on every
// vulnerability.source_snapshot, so presence is asserted here, not just
// shape. Other cassettes (supply-chain-demo, advisorygroup) are hand-written
// and legitimately omit the field, so this holds only the recorded one.
const recordedVulnerabilityCassette = "../../../testdata/cassettes/vulnerabilityintelligence/vuln-collector-nvd.json"

// recordedSourceSnapshotCount is the number of source snapshots the recorder
// emits: five nvd windows (three synthetic, two over real public records), two
// cisa_kev (one synthetic, one real) and two first_epss (one synthetic, one
// real). A change here means the cassette was re-recorded with a different
// shape on purpose.
const recordedSourceSnapshotCount = 9

var responseDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// sourceSnapshotResponseDigestProblems returns the number of
// vulnerability.source_snapshot facts in a cassette document and one problem
// per snapshot whose response_digest key is missing or malformed.
func sourceSnapshotResponseDigestProblems(raw []byte) (int, []string, error) {
	var cassette struct {
		Scopes []struct {
			ScopeID string `json:"scope_id"`
			Facts   []struct {
				FactKind string                     `json:"fact_kind"`
				Payload  map[string]json.RawMessage `json:"payload"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &cassette); err != nil {
		return 0, nil, err
	}
	count := 0
	var problems []string
	for _, scope := range cassette.Scopes {
		for _, fact := range scope.Facts {
			if fact.FactKind != "vulnerability.source_snapshot" {
				continue
			}
			count++
			rawDigest, present := fact.Payload["response_digest"]
			if !present {
				problems = append(problems, fmt.Sprintf("%s: response_digest key is absent", scope.ScopeID))
				continue
			}
			var digest string
			if err := json.Unmarshal(rawDigest, &digest); err != nil || !responseDigestPattern.MatchString(digest) {
				problems = append(problems, fmt.Sprintf("%s: response_digest %s is not sha256:<64 lowercase hex>", scope.ScopeID, rawDigest))
			}
		}
	}
	return count, problems, nil
}

func TestRecordedVulnerabilityCassetteCarriesResponseDigestOnEverySnapshot(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(recordedVulnerabilityCassette)
	if err != nil {
		t.Fatalf("read recorded vulnerability cassette: %v", err)
	}
	count, problems, err := sourceSnapshotResponseDigestProblems(raw)
	if err != nil {
		t.Fatalf("parse recorded vulnerability cassette: %v", err)
	}
	// Vacuity guard: zero snapshots would pass the loop below proving nothing.
	if count == 0 {
		t.Fatal("no vulnerability.source_snapshot facts found; this check passed vacuously")
	}
	if count != recordedSourceSnapshotCount {
		t.Errorf("source snapshots = %d, want %d (5 nvd, 2 cisa_kev, 2 first_epss)", count, recordedSourceSnapshotCount)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
}

// Mutants of the real cassette: dropping the key or emptying the value must
// each be reported, so the presence check cannot silently regress to the
// shape-only walker.
func TestSourceSnapshotResponseDigestCheckCatchesMutants(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(recordedVulnerabilityCassette)
	if err != nil {
		t.Fatalf("read recorded vulnerability cassette: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(payload map[string]any)
	}{
		{"deleted key", func(p map[string]any) { delete(p, "response_digest") }},
		{"emptied value", func(p map[string]any) { p["response_digest"] = "" }},
		{"truncated value", func(p map[string]any) { p["response_digest"] = "sha256:abc" }},
	} {
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: unmarshal: %v", tc.name, err)
		}
		mutated := false
		for _, scope := range doc["scopes"].([]any) {
			for _, fact := range scope.(map[string]any)["facts"].([]any) {
				f := fact.(map[string]any)
				if f["fact_kind"] == "vulnerability.source_snapshot" && !mutated {
					tc.mutate(f["payload"].(map[string]any))
					mutated = true
				}
			}
		}
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("%s: marshal: %v", tc.name, err)
		}
		count, problems, err := sourceSnapshotResponseDigestProblems(out)
		if err != nil {
			t.Fatalf("%s: parse: %v", tc.name, err)
		}
		if count != recordedSourceSnapshotCount || len(problems) != 1 {
			t.Errorf("%s: count=%d problems=%v, want %d snapshots and exactly 1 problem", tc.name, count, problems, recordedSourceSnapshotCount)
		}
	}
}
