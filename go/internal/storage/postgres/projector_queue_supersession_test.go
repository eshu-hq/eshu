// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestProjectorQueueAckSupersedesObsoleteTerminalGenerations(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.Now = func() time.Time {
		return time.Date(2026, time.May, 24, 15, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-current",
		},
	}

	if err := queue.Ack(context.Background(), work, projector.Result{}); err != nil {
		t.Fatalf("Ack() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 5; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.execs[1].query
	for _, want := range []string{
		"UPDATE fact_work_items AS stale",
		"stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"stale_generation.status IN ('pending', 'failed')",
		"projector work superseded by newer same-scope generation",
		"current_generation.generation_id = $3",
		"'current_generation_id', $3",
		"stale_generation.ingested_at < current_generation.ingested_at",
		"stale_generation.generation_id < current_generation.generation_id",
		"UPDATE scope_generations AS generation",
		"generation.status IN ('pending', 'failed')",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("Ack() obsolete generation query missing %q:\n%s", want, query)
		}
	}
}

func TestProjectorClaimSupersedesStaleTerminalGenerations(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"stale_generation.status IN ('pending', 'failed')",
		"generation.status IN ('pending', 'failed')",
	} {
		if !strings.Contains(claimProjectorWorkQuery, want) {
			t.Fatalf("claimProjectorWorkQuery missing %q:\n%s", want, claimProjectorWorkQuery)
		}
	}
}
