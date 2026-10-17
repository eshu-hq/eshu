// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// deployableUnitGapReader counts incident CORRELATES_DEPLOYABLE_UNIT edges
// through a read-only Bolt session, following the config-match resolver
// precedent (AccessModeRead, one statement, fail closed). The probe Cypher is
// the UNWIND-anchored single-pattern shape proven on both pinned backends.
type deployableUnitGapReader struct {
	driver       neo4jdriver.DriverWithContext
	databaseName string
}

// CountIncidentCorrelationEdges implements the gap-reconciliation graph
// probe: which of repos still own an outgoing CORRELATES_DEPLOYABLE_UNIT
// edge. One bounded UNWIND round trip, no new worker.
func (r *deployableUnitGapReader) CountIncidentCorrelationEdges(
	ctx context.Context,
	repos []string,
) (map[string]int64, error) {
	if r == nil || r.driver == nil {
		return nil, fmt.Errorf("deployable unit gap graph reader is required")
	}
	stmt := sourcecypher.BuildDeployableUnitCorrelationIncidentRepos(repos)
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return nil, fmt.Errorf("run deployable unit correlation incident probe: %w", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect deployable unit correlation incident probe: %w", err)
	}
	counts := make(map[string]int64, len(records))
	for _, record := range records {
		value, _ := record.Get("repo_id")
		repoID, _ := value.(string)
		if repoID == "" {
			continue
		}
		counts[repoID]++
	}
	return counts, nil
}

// deployableUnitGapReconciler runs the #8316 gap reconciliation in the
// maintenance pass, strictly before the correlation reopen. It must never
// run in the reducer sweep cycle: any sweep-cycle statement against
// admission_decisions parks a second waiter behind the fault-injection kill
// cell's exclusive lock, which its exact-equality census can never align
// (#8371).
type deployableUnitGapReconciler struct {
	listAnchors func(context.Context, int) ([]string, error)
	countEdges  *deployableUnitGapReader
	invalidate  func(context.Context, []string) (int64, error)
}

// newDeployableUnitGapReconciler binds the gap reconciliation to the
// maintenance pass's Postgres handle and graph reader.
func newDeployableUnitGapReconciler(database bootstrapDB, reader *deployableUnitGapReader) *deployableUnitGapReconciler {
	store := postgres.NewRelationshipStore(database)
	return &deployableUnitGapReconciler{
		listAnchors: store.ListDeployableUnitAdmittedAnchorRepos,
		countEdges:  reader,
		invalidate:  store.InvalidateDeployableUnitReopenFingerprintsForRepos,
	}
}

// Reconcile lists the bounded admitted set, probes the graph once, and
// invalidates the fingerprint rows of the gap set so the correlation reopen
// that follows re-projects the lost edges. A seam error fails closed.
func (r *deployableUnitGapReconciler) Reconcile(ctx context.Context) ([]string, int64, error) {
	if r == nil {
		return nil, 0, fmt.Errorf("deployable unit gap reconciler is required")
	}
	return backfill.ReconcileDeployableUnitCorrelationGaps(
		ctx,
		r.listAnchors,
		r.countEdges.CountIncidentCorrelationEdges,
		r.invalidate,
		backfill.DeployableUnitGapScanLimit,
	)
}
