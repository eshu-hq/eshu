// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func gatePayload(environment, outcome, intentID string) map[string]any {
	return cicdRunCorrelationPayload(CICDRunCorrelationWrite{IntentID: intentID, ScopeID: "s", GenerationID: "g", Cause: intentID},
		CICDRunCorrelationDecision{
			Provider: "github_actions", RunID: "1", RunAttempt: "1", RepositoryID: "repository:r",
			ArtifactDigest: "sha256:a", ImageRef: "img:1", Environment: environment,
			Outcome: CICDRunCorrelationOutcome(outcome), EvidenceFactIDs: []string{intentID},
		})
}

// TestCompletionDigestIgnoresPerPassFields: two passes that derive the same
// consumer truth differ in intent, cause and evidence ids, and must hash alike;
// a consumer-read field must not.
func TestCompletionDigestIgnoresPerPassFields(t *testing.T) {
	t.Parallel()

	first := completionDigest(gatePayload("prod", "exact", "intent-1"))
	if first == "" || first != completionDigest(gatePayload("prod", "exact", "intent-2")) {
		t.Fatal("per-pass fields changed the consumer digest")
	}
	if first == completionDigest(gatePayload("staging", "exact", "intent-1")) {
		t.Fatal("a changed environment kept the consumer digest")
	}
	if first == completionDigest(gatePayload("prod", "rejected", "intent-1")) {
		t.Fatal("a changed outcome kept the consumer digest")
	}
}

// TestCompletionChangedVerdict pins the verdict rule: no rows, a new row, a
// revived tombstone or a different digest is changed; the same digests are not.
func TestCompletionChangedVerdict(t *testing.T) {
	t.Parallel()

	digest := completionDigest(gatePayload("prod", "exact", "i"))
	cases := []struct {
		name   string
		rows   map[string]string
		stored map[string]storedCompletionRow
		want   bool
	}{
		{"no rows has no baseline", nil, nil, true},
		{"new row", map[string]string{"f": digest}, nil, true},
		{"revived tombstone", map[string]string{"f": digest}, map[string]storedCompletionRow{"f": {tombstone: true, digest: digest}}, true},
		{"different digest", map[string]string{"f": digest}, map[string]storedCompletionRow{"f": {digest: "other"}}, true},
		{"identical rewrite", map[string]string{"f": digest}, map[string]storedCompletionRow{"f": {digest: digest}}, false},
	}
	for _, tc := range cases {
		if got := completionChanged(tc.rows, tc.stored); got != tc.want {
			t.Fatalf("%s: changed = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestShouldEmitCompletion: a missing verdict emits, 0 suppresses, 1 emits.
func TestShouldEmitCompletion(t *testing.T) {
	t.Parallel()

	if !ShouldEmitCompletion(reducercontract.Result{}) {
		t.Fatal("a result without the verdict must emit")
	}
	if ShouldEmitCompletion(reducercontract.Result{SubSignals: map[string]float64{CompletionChangedSignal: 0}}) {
		t.Fatal("an unchanged verdict must not emit")
	}
	if !ShouldEmitCompletion(reducercontract.Result{SubSignals: map[string]float64{CompletionChangedSignal: 1}}) {
		t.Fatal("a changed verdict must emit")
	}
}
