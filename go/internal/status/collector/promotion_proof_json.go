// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/shared"
)

// DefaultPromotionStaleAfter is the freshness window used when the
// status render derives promotion proofs. Evidence older than this window marks
// a collector family stale so operators notice silently aging lanes.
const DefaultPromotionStaleAfter = 24 * time.Hour

type PromotionProofJSON struct {
	CollectorKind    string   `json:"collector_kind"`
	InstanceID       string   `json:"instance_id,omitempty"`
	DisplayName      string   `json:"display_name,omitempty"`
	PromotionState   string   `json:"promotion_state"`
	RuntimeCategory  string   `json:"runtime_category,omitempty"`
	Health           string   `json:"health,omitempty"`
	ClaimDriven      bool     `json:"claim_driven"`
	ClaimState       string   `json:"claim_state,omitempty"`
	SourceScope      string   `json:"source_scope,omitempty"`
	FixtureOnly      bool     `json:"fixture_only,omitempty"`
	EvidenceSources  []string `json:"evidence_sources,omitempty"`
	SourceSystems    []string `json:"source_systems,omitempty"`
	ObservationCount int      `json:"observation_count,omitempty"`
	ReducerReadback  string   `json:"reducer_readback,omitempty"`
	TelemetryHandles []string `json:"telemetry_handles,omitempty"`
	Blockers         []string `json:"blockers,omitempty"`
	LastObservedAt   string   `json:"last_observed_at,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
}

func PromotionProofsJSON(rows []PromotionProof) []PromotionProofJSON {
	projected := make([]PromotionProofJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, PromotionProofJSON{
			CollectorKind:    row.CollectorKind,
			InstanceID:       row.InstanceID,
			DisplayName:      row.DisplayName,
			PromotionState:   row.PromotionState,
			RuntimeCategory:  row.RuntimeCategory,
			Health:           row.Health,
			ClaimDriven:      row.ClaimDriven,
			ClaimState:       row.ClaimState,
			SourceScope:      row.SourceScope,
			FixtureOnly:      row.FixtureOnly,
			EvidenceSources:  row.EvidenceSources,
			SourceSystems:    row.SourceSystems,
			ObservationCount: row.ObservationCount,
			ReducerReadback:  row.ReducerReadback,
			TelemetryHandles: row.TelemetryHandles,
			Blockers:         row.Blockers,
			LastObservedAt:   shared.NullableRFC3339Value(row.LastObservedAt),
			UpdatedAt:        shared.NullableRFC3339Value(row.UpdatedAt),
		})
	}
	return projected
}
