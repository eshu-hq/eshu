// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraform

import "fmt"

// Part is a statement-free description of one Terraform graph write. The
// parent cypher package converts each part to its executable Statement.
type Part struct {
	// Cypher is the static query template to execute.
	Cypher string
	// Parameters contains rows and statement metadata for the root adapter.
	Parameters map[string]any
	// IsRetract selects the root canonical retract operation.
	IsRetract bool
	// Drain requests standalone execution before grouped upserts.
	Drain bool
	// DrainVar identifies a variable drained from a mixed statement group.
	DrainVar string
}

const (
	phaseTerraformState = "terraform_state"
	phaseKey            = "_eshu_phase"
	entityLabelKey      = "_eshu_entity_label"
	scopeIDKey          = "_eshu_scope_id"
	generationIDKey     = "_eshu_generation_id"
	summaryKey          = "_eshu_statement_summary"
)

func batchedParts(cypher string, rows []map[string]any, batchSize int, label string, scopeID, generationID string) []Part {
	var parts []Part
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[start:end]
		parts = append(parts, Part{
			Cypher: cypher,
			Parameters: map[string]any{
				"rows":   batch,
				phaseKey: phaseTerraformState, entityLabelKey: label,
				scopeIDKey: scopeID, generationIDKey: generationID,
				summaryKey: fmt.Sprintf("label=%s rows=%d", label, len(batch)),
			},
		})
	}
	return parts
}
