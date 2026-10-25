// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// TestPromotionProofReadsExtensionDefinedForAHostedFirstPartyInstance pins
// #8951 on the promotion proof surface: the proof is built from RuntimeStatuses,
// so an extension-hosted jira instance carries extension_defined in
// LastFailureClass and in its terminal-failure blocker, while an in-tree jira
// instance with the same persisted class keeps unknown. The persisted class
// text never reaches the blocker.
func TestPromotionProofReadsExtensionDefinedForAHostedFirstPartyInstance(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	instance := func(id string, hosted bool) collector.InstanceSummary {
		return collector.InstanceSummary{
			InstanceID: id, CollectorKind: "jira", Mode: "continuous", Enabled: true,
			ClaimsEnabled: true, ExtensionHosted: hosted,
			LastObservedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
		}
	}
	failure := func(id string) collector.TerminalFailure {
		return collector.TerminalFailure{
			CollectorKind: "jira", InstanceID: id, FailureClass: "vendor_quota_exceeded",
			FailureOperation: "collect_failure", ObservedAt: now.Add(-time.Minute),
		}
	}
	proofs := collector.PromotionProofs(collector.Evidence{
		AsOf:             now,
		Instances:        []collector.InstanceSummary{instance("hosted", true), instance("in-tree", false)},
		TerminalFailures: []collector.TerminalFailure{failure("hosted"), failure("in-tree")},
	}, collector.PromotionOptions{})
	want := map[string]string{"hosted": "extension_defined", "in-tree": "unknown"}
	seen := 0
	for _, proof := range proofs {
		class, ok := want[proof.InstanceID]
		if !ok || proof.CollectorKind != "jira" {
			continue
		}
		seen++
		if proof.LastFailureClass != class {
			t.Errorf("proof %s LastFailureClass = %q, want %q", proof.InstanceID, proof.LastFailureClass, class)
		}
		blocker := "terminal fetch failure: " + class + " (collect_failure)"
		if !strings.Contains(strings.Join(proof.Blockers, "\n"), blocker) {
			t.Errorf("proof %s blockers = %q, want %q", proof.InstanceID, proof.Blockers, blocker)
		}
		if strings.Contains(strings.Join(proof.Blockers, "\n"), "vendor_quota_exceeded") {
			t.Errorf("proof %s blockers carry the persisted class text: %q", proof.InstanceID, proof.Blockers)
		}
	}
	if seen != len(want) {
		t.Fatalf("matched %d of %d jira proofs in %d proofs", seen, len(want), len(proofs))
	}
}
