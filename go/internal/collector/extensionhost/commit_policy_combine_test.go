// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCombineClaimCommitPoliciesFansOutByFamily proves the composite seam:
// a GHSA window scope reaches the GHSA policy, a baseline scope reaches
// the baseline policy, and a foreign scope reaches neither — so one wired
// seam serves both rolling families without either handling the other's
// scopes.
func TestCombineClaimCommitPoliciesFansOutByFamily(t *testing.T) {
	t.Parallel()

	ghsa, err := NewGHSAWindowCommitPolicy("scorecard-instance", nil, nil)
	if err != nil {
		t.Fatalf("NewGHSAWindowCommitPolicy() error = %v", err)
	}
	baseline, err := NewNVDBaselineCommitPolicy("scorecard-instance", nil, nil)
	if err != nil {
		t.Fatalf("NewNVDBaselineCommitPolicy() error = %v", err)
	}
	combined := CombineClaimCommitPolicies(ghsa, baseline)

	ghsaItem := testWorkItem()
	ghsaItem.ScopeID = testGHSAWindowScope
	ghsaEnvelopes := []facts.Envelope{ghsaSnapshotEnvelope(map[string]any{
		"source": "ghsa", "complete": true, "covered_through": "2026-11-02T00:00:00Z",
	})}
	if _, err := combined.CommitPolicy(ghsaItem, time.Now().UTC(), ghsaEnvelopes); err != nil {
		t.Fatalf("combined GHSA CommitPolicy() error = %v, want the GHSA policy to handle it", err)
	}

	baselineItem := testWorkItem()
	baselineItem.ScopeID = testNVDBaselineScopeForCommit
	baselineEnvelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(testNVDBaselineVerdictPayload())}
	decision, err := combined.CommitPolicy(baselineItem, time.Now().UTC(), baselineEnvelopes)
	if err != nil {
		t.Fatalf("combined baseline CommitPolicy() error = %v, want the baseline policy to handle it", err)
	}
	if !decision.Handled || decision.StateWriter == nil {
		t.Fatal("combined baseline decision not handled/armed, want the baseline policy to arm the checkpoint")
	}

	foreignItem := testWorkItem()
	foreignItem.ScopeID = "vuln-intel://nvd/modified"
	decision, err = combined.CommitPolicy(foreignItem, time.Now().UTC(), baselineEnvelopes)
	if err != nil {
		t.Fatalf("combined foreign CommitPolicy() error = %v, want unhandled", err)
	}
	if decision.Handled {
		t.Fatal("combined foreign decision Handled = true, want false")
	}
}

// TestCombineClaimCommitPoliciesSkipsNils proves a nil member never runs:
// combining with a missing family keeps the other family's behavior.
func TestCombineClaimCommitPoliciesSkipsNils(t *testing.T) {
	t.Parallel()

	baseline, err := NewNVDBaselineCommitPolicy("scorecard-instance", nil, nil)
	if err != nil {
		t.Fatalf("NewNVDBaselineCommitPolicy() error = %v", err)
	}
	combined := CombineClaimCommitPolicies(nil, baseline)
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	decision, err := combined.CommitPolicy(item, time.Now().UTC(), []facts.Envelope{
		testNVDWindowSnapshotEnvelope(testNVDBaselineVerdictPayload()),
	})
	if err != nil {
		t.Fatalf("combined CommitPolicy() error = %v, want handled past the nil", err)
	}
	if !decision.Handled {
		t.Fatal("combined decision Handled = false, want the non-nil policy to handle it")
	}
	if got := strings.TrimSpace(item.ScopeID); got == "" {
		t.Fatal("test scope id blank")
	}
}
