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
