// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestContainerImageIdentityHandlerPassesWarningGatedRetirementPlan(t *testing.T) {
	t.Parallel()

	loader := &stubContainerImageIdentityFactLoader{
		scopeFacts: []facts.Envelope{
			gitImageRefFact("git-tag", "registry.example.com/team/api:prod"),
		},
		warnings: []facts.Envelope{
			retirementWarningEnvelope("tag_list_truncated", ""),
		},
	}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader: loader,
		Writer:     writer,
		Now: func() time.Time {
			return time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
		},
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-5854",
		Domain:       DomainContainerImageIdentity,
		ScopeID:      "repository:synthetic",
		GenerationID: "generation-5854",
		SourceSystem: "git",
		Cause:        "test",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got, want := loader.warningCalls, 1; got != want {
		t.Fatalf("warning loader calls = %d, want %d", got, want)
	}
	if got := len(writer.write.LegacyFactIDs); got != 0 {
		t.Fatalf("writer legacy fact IDs = %v, want none while tag list is truncated", writer.write.LegacyFactIDs)
	}
	if got := len(writer.write.TombstoneDecisions); got != 0 {
		t.Fatalf("writer tombstones = %v, want none while tag list is truncated", writer.write.TombstoneDecisions)
	}
	if got := len(writer.write.HeldDecisions); got != 1 {
		t.Fatalf("writer held decisions = %v, want the truncated tag ref", writer.write.HeldDecisions)
	}
	if got, want := result.SubSignals["retire_held_tag_list_truncated"], float64(1); got != want {
		t.Fatalf("Handle().SubSignals[retire_held_tag_list_truncated] = %v, want %v", got, want)
	}
}
