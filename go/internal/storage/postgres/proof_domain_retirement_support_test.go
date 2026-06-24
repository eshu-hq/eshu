// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// proofActiveGenerationFactRows emulates the active-generation read contract
// shared by every facts_active_*.go reader: a fact is visible only when its
// generation is the scope's active_generation_id AND that generation's status
// is "active". This is the supersession half of retirement — facts from a
// superseded or failed generation never join, so a removed file, deleted
// entity, dropped dependency, or replaced workload disappears from active reads
// without any per-row delete.
//
// filterTombstones mirrors the `fact.is_tombstone = FALSE` predicate that the
// stricter active readers (for example listActiveContainerImageIdentityFacts)
// add on top of supersession. When true, a tombstone fact that survives in the
// active generation is still excluded, proving the second retirement mechanism:
// collector-emitted negative evidence inside an otherwise-current generation.
//
// factKindAllowed scopes the read to the fact kinds the concrete query selects
// so the harness models the real WHERE clause instead of returning every fact.
func proofActiveGenerationFactRows(
	state proofState,
	filterTombstones bool,
	factKindAllowed func(facts.Envelope) bool,
) [][]any {
	visible := make([]facts.Envelope, 0, len(state.facts))
	for _, envelope := range state.facts {
		activeGenerationID := state.activeGenerations[envelope.ScopeID]
		if activeGenerationID == "" || activeGenerationID != envelope.GenerationID {
			continue
		}
		generation, ok := state.generations[envelope.GenerationID]
		if !ok || generation.Status != scope.GenerationStatusActive {
			continue
		}
		if filterTombstones && envelope.IsTombstone {
			continue
		}
		if factKindAllowed != nil && !factKindAllowed(envelope) {
			continue
		}
		visible = append(visible, envelope)
	}

	sort.Slice(visible, func(i, j int) bool {
		if visible[i].ObservedAt.Equal(visible[j].ObservedAt) {
			return visible[i].FactID < visible[j].FactID
		}
		return visible[i].ObservedAt.Before(visible[j].ObservedAt)
	})

	rows := make([][]any, 0, len(visible))
	for _, envelope := range visible {
		payload, _ := json.Marshal(envelope.Payload)
		rows = append(rows, proofFactEnvelopeRow(envelope, payload))
	}
	return rows
}

// proofActiveRepositoryFactKind matches the listActiveRepositoryFactsQuery
// WHERE clause: git repository facts only. This reader relies on supersession
// alone (no is_tombstone predicate), so it is the case that proves an active
// tombstone still flows through unless a reader opts into tombstone filtering.
func proofActiveRepositoryFactKind(envelope facts.Envelope) bool {
	return envelope.FactKind == "repository" && envelope.SourceRef.SourceSystem == "git"
}
