// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// Per-item read-set waits (#10877): reopen fingerprint records for the two
// domains joining deployable_unit_correlation behind the changed-evidence
// gate. Same claim-fenced write contract as
// RecordDeployableUnitReopenFingerprint (#8283): the value is the one the
// narrowed read returned, never recomputed, and a lost claim races the write
// into zero affected rows, which surfaces as ErrExecutionClaimRejected.

// recordCorpusReopenFingerprint stores one domain's read-time fingerprint,
// fenced on the exact claim. query is the domain's record statement.
func (s *RelationshipStore) recordCorpusReopenFingerprint(
	ctx context.Context,
	query string,
	workItemID string,
	claimedAt time.Time,
	fingerprint reducercontract.ResolvedReadFingerprint,
	domain string,
	logicVersion int,
) error {
	repoIDs := fingerprint.RepoIDs
	if repoIDs == nil {
		repoIDs = []string{}
	}
	result, err := s.database.ExecContext(ctx, query,
		workItemID, claimedAt.UTC(), array.StringArray(repoIDs), fingerprint.Value,
		logicVersion,
	)
	if err != nil {
		return fmt.Errorf("record %s reopen fingerprint: %w", domain, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("record %s reopen fingerprint: rows affected: %w", domain, err)
	}
	if affected == 0 {
		return fmt.Errorf("record %s reopen fingerprint for %s: %w",
			domain, workItemID, reducercontract.ErrExecutionClaimRejected)
	}
	return nil
}

// RecordWorkloadMaterializationReopenFingerprint stores the read-time
// fingerprint of a workload_materialization run that is about to succeed.
func (s *RelationshipStore) RecordWorkloadMaterializationReopenFingerprint(
	ctx context.Context,
	workItemID string,
	claimedAt time.Time,
	fingerprint reducercontract.ResolvedReadFingerprint,
) error {
	return s.recordCorpusReopenFingerprint(ctx,
		backfill.RecordCorpusReopenFingerprintQuery(
			"workload_materialization",
			"workload_materialization_reopen_fingerprints",
			reducercontract.WorkloadMaterializationLogicVersion),
		workItemID, claimedAt, fingerprint,
		"workload_materialization", reducercontract.WorkloadMaterializationLogicVersion)
}

// RecordServiceCatalogCorrelationReopenFingerprint stores the read-time
// fingerprint of a service_catalog_correlation run that is about to succeed.
func (s *RelationshipStore) RecordServiceCatalogCorrelationReopenFingerprint(
	ctx context.Context,
	workItemID string,
	claimedAt time.Time,
	fingerprint reducercontract.ResolvedReadFingerprint,
) error {
	return s.recordCorpusReopenFingerprint(ctx,
		backfill.RecordCorpusReopenFingerprintQuery(
			"service_catalog_correlation",
			"service_catalog_correlation_reopen_fingerprints",
			reducercontract.ServiceCatalogCorrelationLogicVersion),
		workItemID, claimedAt, fingerprint,
		"service_catalog_correlation", reducercontract.ServiceCatalogCorrelationLogicVersion)
}

// invalidateCorpusReopenFingerprintsForRepos deletes the reopen fingerprint
// rows of every item of one domain whose recorded repository set overlaps
// repoIDs. A missing row always re-opens on the next maintenance pass.
func (s *RelationshipStore) invalidateCorpusReopenFingerprintsForRepos(
	ctx context.Context,
	table string,
	repoIDs []string,
	domain string,
) (int64, error) {
	if len(repoIDs) == 0 {
		return 0, nil
	}
	result, err := s.database.ExecContext(ctx,
		backfill.InvalidateCorpusReopenFingerprintsForReposQuery(table),
		array.StringArray(repoIDs))
	if err != nil {
		return 0, fmt.Errorf("invalidate %s reopen fingerprints: %w", domain, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("invalidate %s reopen fingerprints: rows affected: %w", domain, err)
	}
	return affected, nil
}

// InvalidateWorkloadMaterializationReopenFingerprintsForRepos deletes the
// workload_materialization reopen rows overlapping the removed repositories.
func (s *RelationshipStore) InvalidateWorkloadMaterializationReopenFingerprintsForRepos(
	ctx context.Context,
	repoIDs []string,
) (int64, error) {
	return s.invalidateCorpusReopenFingerprintsForRepos(ctx,
		"workload_materialization_reopen_fingerprints", repoIDs, "workload_materialization")
}

// InvalidateServiceCatalogCorrelationReopenFingerprintsForRepos deletes the
// service_catalog_correlation reopen rows overlapping the removed
// repositories.
func (s *RelationshipStore) InvalidateServiceCatalogCorrelationReopenFingerprintsForRepos(
	ctx context.Context,
	repoIDs []string,
) (int64, error) {
	return s.invalidateCorpusReopenFingerprintsForRepos(ctx,
		"service_catalog_correlation_reopen_fingerprints", repoIDs, "service_catalog_correlation")
}
