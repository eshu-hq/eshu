// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplane

// Part is one leaf-local edge statement part: the static Cypher template,
// its parameters, and the routing flag the parent cypher package maps onto
// Statement{Operation, Drain}. Part carries no Statement so this leaf never
// imports its parent (the root edge-writer adapter is the caller).
type Part struct {
	// Cypher is the static template to execute.
	Cypher string
	// Parameters are the statement parameters.
	Parameters map[string]any
	// IsRetract marks a stale-edge retraction (parent maps to
	// OperationCanonicalRetract); false marks an upsert.
	IsRetract bool
	// Drain marks a retract the NornicDB phase-group executor must run as
	// a standalone autocommit statement (parent maps to Statement.Drain).
	Drain bool
}

const (
	// phaseKey tags a part with the writer phase that produced it. The value
	// must stay identical to the parent cypher package's
	// StatementMetadataPhaseKey ("_eshu_phase"): the root adapter copies
	// Parameters through untouched, so a drift here would silently fork the
	// phase dimension in statement metadata.
	phaseKey = "_eshu_phase"
	// entityLabelKey tags a part with the concrete edge label it writes.
	// Same aliasing contract as phaseKey, against
	// StatementMetadataEntityLabelKey.
	entityLabelKey = "_eshu_entity_label"
	// summaryKey carries the human-readable bounded summary. Same aliasing
	// contract as phaseKey, against StatementMetadataSummaryKey.
	summaryKey = "_eshu_statement_summary"
)
