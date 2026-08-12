// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import "time"

const (
	// DefaultCanonicalWriteTimeout bounds one NornicDB canonical transaction.
	DefaultCanonicalWriteTimeout = 30 * time.Second
	// DefaultPhaseGroupStatements is the broad per-transaction statement cap.
	DefaultPhaseGroupStatements = 500
	// DefaultDirectoryPhaseStatements is the directory-phase statement cap.
	DefaultDirectoryPhaseStatements = 5
	// DefaultFilePhaseStatements is the file-phase statement cap.
	DefaultFilePhaseStatements = 5
	// DefaultStructuralEdgePhaseStatements is the structural-edge statement cap
	// (issue #6070). Structural-edge statements are row-batched at the canonical
	// writer's batch size (500), so effective transaction pressure is that batch
	// size times this cap: 5 x 500 = 2,500 rows, the reference grouped row
	// pressure documented in docs/public/reference/nornicdb-tuning.md. Before
	// this cap the phase fell through to DefaultPhaseGroupStatements, so a
	// 147-statement scope committed roughly 73,500 rows in one transaction and
	// dead-lettered on the canonical write budget.
	DefaultStructuralEdgePhaseStatements = 5
	// DefaultEntityPhaseStatements is the entity-phase statement cap.
	DefaultEntityPhaseStatements = 25
	// DefaultFileBatchSize bounds rows in each canonical File statement.
	DefaultFileBatchSize = 100
	// DefaultEntityBatchSize bounds rows in each broad canonical entity statement.
	DefaultEntityBatchSize = 100
	// DefaultFunctionEntityBatchSize bounds Function rows per statement.
	DefaultFunctionEntityBatchSize = 15
	// DefaultStructEntityBatchSize bounds Struct rows per statement.
	DefaultStructEntityBatchSize = 50
	// DefaultVariableEntityBatchSize bounds Variable rows per statement.
	DefaultVariableEntityBatchSize = 100
	// DefaultK8sResourceEntityBatchSize bounds K8sResource rows per statement.
	DefaultK8sResourceEntityBatchSize = 1
	// DefaultFunctionEntityPhaseStatements bounds Function statements per transaction.
	DefaultFunctionEntityPhaseStatements = 5
	// DefaultStructEntityPhaseStatements bounds Struct statements per transaction.
	DefaultStructEntityPhaseStatements = 15
	// DefaultVariableEntityPhaseStatements bounds Variable statements per transaction.
	DefaultVariableEntityPhaseStatements = 5
	// DefaultK8sResourceEntityPhaseStatements bounds K8sResource statements per transaction.
	DefaultK8sResourceEntityPhaseStatements = 1
	// DefaultEntityLabelSummaryExecutions controls rolling label-summary cadence.
	DefaultEntityLabelSummaryExecutions = 10
	// DefaultCanonicalRetractBatchSize bounds each full-refresh delete step.
	DefaultCanonicalRetractBatchSize = 2000
	// MinCanonicalRetractBatchSize is the smallest accepted retract drain batch.
	MinCanonicalRetractBatchSize = 1
	// MaxCanonicalRetractBatchSize is the largest accepted retract drain batch.
	MaxCanonicalRetractBatchSize = 10000
	// EntityPhaseConcurrencyCap bounds concurrent entity transaction fan-out.
	EntityPhaseConcurrencyCap = 16
	// DefaultBatchedEntityContainment keeps containment in row-scoped entity upserts.
	DefaultBatchedEntityContainment = true
)

// DefaultEntityLabelBatchSizes returns the evidence-backed per-label row caps.
func DefaultEntityLabelBatchSizes(entityBatchSize int) map[string]int {
	return map[string]int{
		"Function":    capOptional(entityBatchSize, DefaultFunctionEntityBatchSize),
		"K8sResource": capOptional(entityBatchSize, DefaultK8sResourceEntityBatchSize),
		"Struct":      capOptional(entityBatchSize, DefaultStructEntityBatchSize),
		"Variable":    capOptional(entityBatchSize, DefaultVariableEntityBatchSize),
	}
}

// DefaultEntityLabelPhaseStatements returns the evidence-backed per-label transaction caps.
func DefaultEntityLabelPhaseStatements(entityPhaseStatements int) map[string]int {
	return map[string]int{
		"Function":    capOptional(entityPhaseStatements, DefaultFunctionEntityPhaseStatements),
		"K8sResource": capOptional(entityPhaseStatements, DefaultK8sResourceEntityPhaseStatements),
		"Struct":      capOptional(entityPhaseStatements, DefaultStructEntityPhaseStatements),
		"Variable":    capOptional(entityPhaseStatements, DefaultVariableEntityPhaseStatements),
	}
}

func capOptional(configured int, limit int) int {
	if configured <= 0 || configured > limit {
		return limit
	}
	return configured
}
