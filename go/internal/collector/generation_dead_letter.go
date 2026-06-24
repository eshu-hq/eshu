// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

const generationDeadLetterReplayCompletionTimeout = 10 * time.Second

// GenerationDeadLetterSink persists collector generations that could not cross
// the durable commit boundary.
type GenerationDeadLetterSink interface {
	RecordGenerationDeadLetter(context.Context, GenerationDeadLetter) error
}

// GenerationDeadLetterReplayCompleter clears unresolved generation
// dead-letters after the same source scope commits successfully.
type GenerationDeadLetterReplayCompleter interface {
	CompleteGenerationDeadLetterReplay(context.Context, GenerationDeadLetterReplayCompletion) error
}

// GenerationDeadLetter captures safe replay metadata for one collector
// generation whose facts could not be durably committed.
type GenerationDeadLetter struct {
	Scope            scope.IngestionScope
	Generation       scope.ScopeGeneration
	FailureClass     string
	FailureMessage   string
	PayloadReference map[string]string
	DeadLetteredAt   time.Time
}

// GenerationDeadLetterReplayCompletion captures the successful source commit
// that resolves prior dead-lettered generation rows for the same scope.
type GenerationDeadLetterReplayCompletion struct {
	Scope       scope.IngestionScope
	Generation  scope.ScopeGeneration
	CompletedAt time.Time
}

// GenerationDeadLetterReplayFilter constrains operator or coordinator replay
// requests for dead-lettered collector generations.
type GenerationDeadLetterReplayFilter struct {
	ScopeIDs      []string
	FailureClass  string
	CollectorKind scope.CollectorKind
	Limit         int
}

// GenerationDeadLetterReplayResult reports which dead-lettered generations
// were marked for source-level replay.
type GenerationDeadLetterReplayResult struct {
	Replayed      int
	GenerationIDs []string
}

func generationDeadLetterPayloadReference(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
) map[string]string {
	ref := map[string]string{
		"collector_kind": string(scopeValue.CollectorKind),
		"generation_id":  generation.GenerationID,
		"partition_key":  scopeValue.PartitionKey,
		"scope_id":       scopeValue.ScopeID,
		"scope_kind":     string(scopeValue.ScopeKind),
		"source_system":  scopeValue.SourceSystem,
		"trigger_kind":   string(generation.TriggerKind),
	}
	if generation.FreshnessHint != "" {
		ref["freshness_hint"] = generation.FreshnessHint
	}
	return ref
}
