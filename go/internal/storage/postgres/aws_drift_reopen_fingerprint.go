// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// LoadAWSDriftARNSnapshot reads one AWS scope generation's ARN snapshot:
// one payload identity per ARN, in ARN order, plus the input fingerprint
// over the snapshot (contract.ComputeARNInputFingerprint). It runs the same
// payload-hash expression the reopen listing fingerprints, so the stored
// value matches what the listing recomputes in SQL for identical input
// (#10878, first domain). The record path calls it for the generation the
// run just classified; the next run diffs the recorded snapshot against
// its fresh load to narrow to changed ARNs.
func (l PostgresAWSCloudRuntimeDriftEvidenceLoader) LoadAWSDriftARNSnapshot(
	ctx context.Context,
	scopeID string,
	generationID string,
) (reducercontract.AWSDriftReopenSnapshot, error) {
	snapshot := reducercontract.AWSDriftReopenSnapshot{ScopeID: scopeID, GenerationID: generationID}
	if l.DB == nil {
		return snapshot, fmt.Errorf("aws cloud runtime drift evidence database is required")
	}
	rows, err := l.DB.QueryContext(ctx, backfill.AWSDriftSnapshotRowsSQL, scopeID, generationID)
	if err != nil {
		return snapshot, fmt.Errorf("list aws drift arn snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()
	hashes := map[string]string{}
	for rows.Next() {
		var arn, payloadHash string
		if err := rows.Scan(&arn, &payloadHash); err != nil {
			return snapshot, fmt.Errorf("scan aws drift arn snapshot: %w", err)
		}
		if strings.TrimSpace(arn) == "" {
			continue
		}
		hashes[arn] = payloadHash
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate aws drift arn snapshot: %w", err)
	}
	arns := make([]string, 0, len(hashes))
	for arn := range hashes {
		arns = append(arns, arn)
	}
	sort.Strings(arns)
	aligned := make([]string, 0, len(arns))
	for _, arn := range arns {
		aligned = append(aligned, hashes[arn])
	}
	snapshot.ARNIDs = arns
	snapshot.ARNPayloadHashes = aligned
	snapshot.Fingerprint = reducercontract.ComputeARNInputFingerprint(hashes)
	return snapshot, nil
}

// RecordAWSDriftReopenFingerprint stores the ARN snapshot of an
// aws_cloud_runtime_drift run that is about to succeed (#10878), fenced on
// the exact claim (workItemID plus claimedAt, the claim's last_attempt_at,
// and a live claim_until). It never recomputes the snapshot: the value is
// the one LoadAWSDriftARNSnapshot returned for the generation the run
// classified. It returns an error wrapping
// reducercontract.ErrExecutionClaimRejected when the claim no longer holds
// the item or a later claim already recorded a row.
func (s IngestionStore) RecordAWSDriftReopenFingerprint(
	ctx context.Context,
	workItemID string,
	claimedAt time.Time,
	snapshot reducercontract.AWSDriftReopenSnapshot,
	fullRun bool,
) error {
	arnIDs := snapshot.ARNIDs
	if arnIDs == nil {
		arnIDs = []string{}
	}
	hashes := snapshot.ARNPayloadHashes
	if hashes == nil {
		hashes = []string{}
	}
	result, err := s.database.ExecContext(ctx, backfill.RecordAWSDriftReopenFingerprintQuery,
		workItemID, claimedAt.UTC(), snapshot.ScopeID, snapshot.GenerationID,
		array.StringArray(arnIDs), array.StringArray(hashes), snapshot.Fingerprint,
		reducercontract.AWSRuntimeDriftLogicVersion, fullRun,
	)
	if err != nil {
		return fmt.Errorf("record aws drift reopen fingerprint: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("record aws drift reopen fingerprint: rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("record aws drift reopen fingerprint for %s: %w",
			workItemID, reducercontract.ErrExecutionClaimRejected)
	}
	return nil
}

// LoadAWSDriftRecordedSnapshot reads this run's previously recorded reopen
// snapshot for the per-run incremental plan: the ARN-to-payload-identity
// map zipped from the recorded parallel arrays, plus the last full run's
// completion time (last_audit_at, the plan's audit clock). A NULL clock
// (never audited) reads as the zero time, which the planner treats as
// audit-due. ok=false means the work item never recorded: absence re-opens,
// never skips (#10878 decision 4). A length-mismatched row is an error, not
// a truncation: the planner fails open to a full run and the record path
// heals the row.
func (s IngestionStore) LoadAWSDriftRecordedSnapshot(
	ctx context.Context,
	workItemID string,
) (map[string]string, time.Time, bool, error) {
	if s.database == nil {
		return nil, time.Time{}, false, fmt.Errorf("ingestion database is required")
	}
	rows, err := s.database.QueryContext(ctx, backfill.LoadAWSDriftRecordedSnapshotQuery, workItemID)
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("list aws drift recorded snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, time.Time{}, false, fmt.Errorf("list aws drift recorded snapshot: %w", err)
		}
		return nil, time.Time{}, false, nil
	}
	var arnIDs, arnPayloadHashes array.StringArray
	var lastAuditAt sql.NullTime
	if err := rows.Scan(&arnIDs, &arnPayloadHashes, &lastAuditAt); err != nil {
		return nil, time.Time{}, false, fmt.Errorf("scan aws drift recorded snapshot: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, false, fmt.Errorf("iterate aws drift recorded snapshot: %w", err)
	}
	if len(arnIDs) != len(arnPayloadHashes) {
		return nil, time.Time{}, false, fmt.Errorf(
			"aws drift recorded snapshot length mismatch: %d ids vs %d hashes",
			len(arnIDs), len(arnPayloadHashes),
		)
	}
	prev := make(map[string]string, len(arnIDs))
	for i, arn := range arnIDs {
		prev[arn] = arnPayloadHashes[i]
	}
	return prev, lastAuditAt.Time, true, nil
}

// listAWSDriftReopenCandidates runs the changed-evidence listing and
// returns the items to re-open plus the count left succeeded (#10878).
func listAWSDriftReopenCandidates(
	ctx context.Context,
	queryer db.Queryer,
	domain string,
) ([]string, int, error) {
	rows, err := queryer.QueryContext(ctx, backfill.ListAWSDriftReopenCandidatesQuery, domain)
	if err != nil {
		return nil, 0, fmt.Errorf("list %s reopen candidates: %w", domain, err)
	}
	return backfill.ScanAWSDriftReopenCandidates(rows)
}
