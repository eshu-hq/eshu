// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

const (
	// StatementMetadataReconciliationDriftKey marks a canonical retract statement
	// whose successful graph delete counters should contribute to reconciliation
	// drift telemetry.
	StatementMetadataReconciliationDriftKey = "_eshu_reconciliation_drift"

	reconciliationDriftDomainCanonicalGraph = "canonical_graph"
	reconciliationDriftKindEdge             = "edge"
	reconciliationDriftKindNode             = "node"
)

// StatementRetractionCounts carries graph driver delete counters for one
// executed statement. Driver adapters accumulate these inside the transaction
// and publish them only after the transaction commits.
type StatementRetractionCounts struct {
	Statement            Statement
	NodesDeleted         int64
	RelationshipsDeleted int64
}

func annotateReconciliationDriftWritePhases(phases []canonicalWritePhase) []canonicalWritePhase {
	for phaseIndex := range phases {
		phase := &phases[phaseIndex]
		if phase.name != "retract" && phase.name != "entity_retract" {
			continue
		}
		for statementIndex := range phase.statements {
			statement := &phase.statements[statementIndex]
			if statement.Operation != OperationCanonicalRetract {
				continue
			}
			if statement.Parameters == nil {
				statement.Parameters = make(map[string]any)
			}
			statement.Parameters[StatementMetadataReconciliationDriftKey] = true
		}
	}
	return phases
}

// RecordReconciliationDriftRetractionCounts records every marked statement in a
// committed write transaction.
func RecordReconciliationDriftRetractionCounts(
	ctx context.Context,
	instruments *telemetry.Instruments,
	counts []StatementRetractionCounts,
) {
	for _, count := range counts {
		RecordReconciliationDriftRetractions(
			ctx,
			instruments,
			count.Statement,
			count.NodesDeleted,
			count.RelationshipsDeleted,
		)
	}
}

// RecordReconciliationDriftRetractions records actual graph delete counters for
// a marked reconciliation retract statement.
func RecordReconciliationDriftRetractions(
	ctx context.Context,
	instruments *telemetry.Instruments,
	statement Statement,
	nodesDeleted int64,
	relationshipsDeleted int64,
) {
	if instruments == nil || instruments.ReconciliationDriftRetractions == nil {
		return
	}
	if statement.Operation != OperationCanonicalRetract || statement.Parameters == nil {
		return
	}
	marked, _ := statement.Parameters[StatementMetadataReconciliationDriftKey].(bool)
	if !marked {
		return
	}
	phase, _ := statement.Parameters[StatementMetadataPhaseKey].(string)
	if phase == "" {
		phase = "unknown"
	}
	recordReconciliationDriftRetraction(ctx, instruments, phase, reconciliationDriftKindNode, nodesDeleted)
	recordReconciliationDriftRetraction(ctx, instruments, phase, reconciliationDriftKindEdge, relationshipsDeleted)
}

func recordReconciliationDriftRetraction(
	ctx context.Context,
	instruments *telemetry.Instruments,
	phase string,
	kind string,
	count int64,
) {
	if count <= 0 {
		return
	}
	instruments.ReconciliationDriftRetractions.Add(ctx, count, metric.WithAttributes(
		telemetry.AttrDomain(reconciliationDriftDomainCanonicalGraph),
		telemetry.AttrWritePhase(phase),
		telemetry.AttrKind(kind),
	))
}
