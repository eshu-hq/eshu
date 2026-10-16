// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRefuseDestructiveSuccessorCells pins the #7926 successor-emptiness
// backstop: the store must refuse to let an empty or sharply shrunken
// successor replace a content-bearing prior generation unless the collection
// carries an explicit trusted-deletion signal. Refusal keeps the prior
// generation active; the trusted signal is the escape hatch for legitimate
// mass-resolves.
func TestRefuseDestructiveSuccessorCells(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		priorFound      bool
		priorFacts      int
		successorFacts  int
		trustedDeletion bool
		wantRefuse      bool
	}{
		{name: "empty successor over content-bearing prior", priorFound: true, priorFacts: 300, successorFacts: 0, wantRefuse: true},
		{name: "empty successor over single-fact prior", priorFound: true, priorFacts: 1, successorFacts: 0, wantRefuse: true},
		{name: "sharply shrunken successor", priorFound: true, priorFacts: 300, successorFacts: 49, wantRefuse: true},
		{name: "small scope shrunken to one", priorFound: true, priorFacts: 3, successorFacts: 1, wantRefuse: true},
		{name: "exactly halved successor stores", priorFound: true, priorFacts: 100, successorFacts: 50, wantRefuse: false},
		{name: "mild shrink stores", priorFound: true, priorFacts: 10, successorFacts: 9, wantRefuse: false},
		{name: "grown successor stores", priorFound: true, priorFacts: 10, successorFacts: 12, wantRefuse: false},
		{name: "first write with no prior stores", priorFound: false, priorFacts: 0, successorFacts: 0, wantRefuse: false},
		{name: "empty successor over empty prior stores", priorFound: true, priorFacts: 0, successorFacts: 0, wantRefuse: false},
		{name: "trusted empty successor stores", priorFound: true, priorFacts: 300, successorFacts: 0, trustedDeletion: true, wantRefuse: false},
		{name: "trusted shrunken successor stores", priorFound: true, priorFacts: 300, successorFacts: 49, trustedDeletion: true, wantRefuse: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refuse, reason := RefuseDestructiveSuccessor(tc.priorFound, tc.priorFacts, tc.successorFacts, tc.trustedDeletion)
			if refuse != tc.wantRefuse {
				t.Fatalf("RefuseDestructiveSuccessor(priorFound=%v, prior=%d, successor=%d, trusted=%v) = %v, want %v",
					tc.priorFound, tc.priorFacts, tc.successorFacts, tc.trustedDeletion, refuse, tc.wantRefuse)
			}
			if refuse && reason != telemetry.RefreshSkipReasonDestructiveSuccessor {
				t.Fatalf("refusal reason = %q, want %q", reason, telemetry.RefreshSkipReasonDestructiveSuccessor)
			}
			if !refuse && reason != "" {
				t.Fatalf("allow reason = %q, want empty", reason)
			}
		})
	}
}
