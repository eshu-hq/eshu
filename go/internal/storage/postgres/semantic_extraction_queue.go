// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

const semanticExtractionJobSchemaSQL = `
CREATE TABLE IF NOT EXISTS semantic_extraction_jobs (
    job_id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    source_class TEXT NOT NULL,
    source_id_hash TEXT NOT NULL,
    chunk_id_hash TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    chunk_hash TEXT NOT NULL,
    source_version TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    redaction_version TEXT NOT NULL,
    extractor_version TEXT NOT NULL,
    extraction_mode TEXT NOT NULL,
    provider_kind TEXT NULL,
    provider_profile_id TEXT NULL,
    provider_profile_class TEXT NULL,
    policy_id TEXT NULL,
    rule_id TEXT NULL,
    policy_state TEXT NULL,
    policy_reason TEXT NULL,
    guard_state TEXT NULL,
    guard_reason TEXT NULL,
    actor_class TEXT NULL,
    acl_state TEXT NULL,
    classifier_version TEXT NULL,
    status TEXT NOT NULL,
    provider_job BOOLEAN NOT NULL DEFAULT false,
    retryable BOOLEAN NOT NULL DEFAULT false,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NULL,
    claim_until TIMESTAMPTZ NULL,
    last_attempt_at TIMESTAMPTZ NULL,
    next_attempt_at TIMESTAMPTZ NULL,
    failure_class TEXT NULL,
    failure_message TEXT NULL,
    failure_details TEXT NULL,
    stale_reason TEXT NULL,
    stale_at TIMESTAMPTZ NULL,
    response_hash TEXT NULL,
    budget_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS semantic_extraction_jobs_work_item_idx
    ON semantic_extraction_jobs (work_item_id);

CREATE INDEX IF NOT EXISTS semantic_extraction_jobs_scope_generation_status_idx
    ON semantic_extraction_jobs (scope_id, generation_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS semantic_extraction_jobs_fingerprint_idx
    ON semantic_extraction_jobs (scope_id, source_id_hash, chunk_id_hash, fingerprint);

CREATE INDEX IF NOT EXISTS semantic_extraction_jobs_claim_idx
    ON semantic_extraction_jobs (status, claim_until, updated_at ASC)
    WHERE status IN ('pending', 'retrying');

CREATE INDEX IF NOT EXISTS semantic_extraction_jobs_provider_claim_idx
    ON semantic_extraction_jobs (scope_id, status, next_attempt_at, claim_until, updated_at ASC, job_id)
    WHERE status IN ('pending', 'retrying') AND provider_job = true;
`

// SemanticExtractionJobSchemaSQL returns the semantic queue DDL.
func SemanticExtractionJobSchemaSQL() string {
	return semanticExtractionJobSchemaSQL
}

func semanticExtractionJobBootstrapDefinition() Definition {
	return Definition{
		Name: "semantic_extraction_jobs",
		Path: "schema/data-plane/postgres/006a_semantic_extraction_jobs.sql",
		SQL:  semanticExtractionJobSchemaSQL,
	}
}

// SemanticExtractionQueueStore persists semantic extraction queue records.
type SemanticExtractionQueueStore struct {
	db ExecQueryer
}

// NewSemanticExtractionQueueStore creates a semantic extraction queue store.
func NewSemanticExtractionQueueStore(db ExecQueryer) SemanticExtractionQueueStore {
	return SemanticExtractionQueueStore{db: db}
}

// ApplyPlan upserts the queue records produced by a planning pass.
func (s SemanticExtractionQueueStore) ApplyPlan(ctx context.Context, plan semanticqueue.Plan) error {
	if s.db == nil {
		return errors.New("semantic extraction queue store db is required")
	}
	records := planRecords(plan)
	if len(records) == 0 {
		return nil
	}
	query, args, err := buildSemanticQueueUpsert(records)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert semantic extraction jobs: %w", err)
	}
	return nil
}

// StatusSummary returns redacted aggregate semantic queue status counts.
func (s SemanticExtractionQueueStore) StatusSummary(
	ctx context.Context,
	scopeID string,
	generationID string,
) (semanticqueue.Summary, error) {
	if s.db == nil {
		return semanticqueue.Summary{}, errors.New("semantic extraction queue store db is required")
	}
	rows, err := s.db.QueryContext(ctx, semanticQueueStatusSummaryQuery, scopeID, generationID)
	if err != nil {
		return semanticqueue.Summary{}, fmt.Errorf("query semantic queue status summary: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summary semanticqueue.Summary
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return semanticqueue.Summary{}, fmt.Errorf("scan semantic queue status summary: %w", err)
		}
		addSemanticStatusCount(&summary, semanticqueue.Status(status), int(count))
	}
	if err := rows.Err(); err != nil {
		return semanticqueue.Summary{}, fmt.Errorf("iterate semantic queue status summary: %w", err)
	}
	return summary, nil
}

// ClaimNext leases the next pending semantic provider job for one scope.
func (s SemanticExtractionQueueStore) ClaimNext(
	ctx context.Context,
	scopeID string,
	leaseOwner string,
	now time.Time,
	leaseFor time.Duration,
) (semanticqueue.Record, bool, error) {
	if s.db == nil {
		return semanticqueue.Record{}, false, errors.New("semantic extraction queue store db is required")
	}
	if strings.TrimSpace(leaseOwner) == "" {
		return semanticqueue.Record{}, false, errors.New("lease owner is required")
	}
	if leaseFor <= 0 {
		return semanticqueue.Record{}, false, errors.New("lease duration must be positive")
	}
	rows, err := s.db.QueryContext(
		ctx,
		claimSemanticQueueJobQuery,
		scopeID,
		now.UTC(),
		leaseOwner,
		now.Add(leaseFor).UTC(),
	)
	if err != nil {
		return semanticqueue.Record{}, false, fmt.Errorf("claim semantic extraction job: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return semanticqueue.Record{}, false, fmt.Errorf("claim semantic extraction job: %w", err)
		}
		return semanticqueue.Record{}, false, nil
	}
	var status string
	var attemptCount int64
	var sourceClass, providerKind, providerProfileID, providerProfileClass sql.NullString
	record := semanticqueue.Record{}
	if err := rows.Scan(
		&record.JobID,
		&record.WorkItemID,
		&record.Fingerprint,
		&record.ScopeID,
		&record.GenerationID,
		&status,
		&attemptCount,
		&sourceClass,
		&providerKind,
		&providerProfileID,
		&providerProfileClass,
	); err != nil {
		return semanticqueue.Record{}, false, fmt.Errorf("scan semantic extraction job claim: %w", err)
	}
	record.Status = semanticqueue.Status(status)
	record.AttemptCount = int(attemptCount)
	record.SourceClass = sourceClass.String
	record.ProviderKind = providerKind.String
	record.ProviderProfileID = providerProfileID.String
	record.ProviderProfileClass = providerProfileClass.String
	return record, true, nil
}

// RetryClaim persists a retry state while preserving the worker lease fence.
func (s SemanticExtractionQueueStore) RetryClaim(
	ctx context.Context,
	record semanticqueue.Record,
	leaseOwner string,
	now time.Time,
	nextAttempt time.Time,
	failure semanticqueue.Failure,
) error {
	if s.db == nil {
		return errors.New("semantic extraction queue store db is required")
	}
	result, err := s.db.ExecContext(
		ctx,
		retrySemanticQueueJobQuery,
		now.UTC(),
		nextAttempt.UTC(),
		failure.Class,
		failure.Message,
		failure.Detail,
		record.JobID,
		leaseOwner,
		record.Fingerprint,
	)
	if err != nil {
		return fmt.Errorf("retry semantic extraction job: %w", err)
	}
	return semanticExtractionRowsAffected(result)
}

// DeadLetterClaim persists a terminal dead-letter state behind the lease fence.
func (s SemanticExtractionQueueStore) DeadLetterClaim(
	ctx context.Context,
	record semanticqueue.Record,
	leaseOwner string,
	now time.Time,
	failure semanticqueue.Failure,
) error {
	if s.db == nil {
		return errors.New("semantic extraction queue store db is required")
	}
	result, err := s.db.ExecContext(
		ctx,
		deadLetterSemanticQueueJobQuery,
		now.UTC(),
		failure.Class,
		failure.Message,
		failure.Detail,
		record.JobID,
		leaseOwner,
		record.Fingerprint,
	)
	if err != nil {
		return fmt.Errorf("dead-letter semantic extraction job: %w", err)
	}
	return semanticExtractionRowsAffected(result)
}

const semanticQueueStatusSummaryQuery = `
SELECT status, COUNT(*)::BIGINT
FROM semantic_extraction_jobs
WHERE scope_id = $1
  AND generation_id = $2
GROUP BY status
`

const claimSemanticQueueJobQuery = `
WITH next_job AS (
    SELECT job_id
    FROM semantic_extraction_jobs
    WHERE scope_id = $1
      AND status IN ('pending', 'retrying')
      AND provider_job = true
      AND (claim_until IS NULL OR claim_until <= $2)
      AND (next_attempt_at IS NULL OR next_attempt_at <= $2)
    ORDER BY updated_at ASC, job_id ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE semantic_extraction_jobs AS jobs
SET status = 'claimed',
    lease_owner = $3,
    claim_until = $4,
    attempt_count = attempt_count + 1,
    last_attempt_at = $2,
    updated_at = $2
FROM next_job
WHERE jobs.job_id = next_job.job_id
RETURNING jobs.job_id, jobs.work_item_id, jobs.fingerprint, jobs.scope_id,
    jobs.generation_id, jobs.status, jobs.attempt_count, jobs.source_class,
    jobs.provider_kind, jobs.provider_profile_id, jobs.provider_profile_class
`

const retrySemanticQueueJobQuery = `
UPDATE semantic_extraction_jobs
SET status = 'retrying',
    retryable = true,
    provider_job = true,
    claim_until = NULL,
    lease_owner = NULL,
    next_attempt_at = $2,
    last_attempt_at = $1,
    failure_class = $3,
    failure_message = $4,
    failure_details = $5,
    updated_at = $1
WHERE job_id = $6
  AND lease_owner = $7
  AND fingerprint = $8
`

const deadLetterSemanticQueueJobQuery = `
UPDATE semantic_extraction_jobs
SET status = 'dead_letter',
    retryable = false,
    provider_job = false,
    claim_until = NULL,
    lease_owner = NULL,
    next_attempt_at = NULL,
    last_attempt_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4,
    updated_at = $1
WHERE job_id = $5
  AND lease_owner = $6
  AND fingerprint = $7
`

func buildSemanticQueueUpsert(records []semanticqueue.Record) (string, []any, error) {
	columns := semanticQueueColumns()
	var builder strings.Builder
	builder.WriteString("INSERT INTO semantic_extraction_jobs (")
	builder.WriteString(strings.Join(columns, ", "))
	builder.WriteString(") VALUES ")

	args := make([]any, 0, len(records)*len(columns))
	for i, record := range records {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString("(")
		for j := range columns {
			if j > 0 {
				builder.WriteString(", ")
			}
			_, _ = fmt.Fprintf(&builder, "$%d", len(args)+j+1)
		}
		builder.WriteString(")")
		values, err := semanticQueueRecordValues(record)
		if err != nil {
			return "", nil, err
		}
		args = append(args, values...)
	}
	builder.WriteString(" ON CONFLICT (job_id) DO UPDATE SET ")
	builder.WriteString(strings.Join(semanticQueueUpsertAssignments(), ", "))
	builder.WriteString(" WHERE semantic_extraction_jobs.status <> 'claimed'")
	return builder.String(), args, nil
}

func semanticQueueColumns() []string {
	return []string{
		"job_id", "work_item_id", "fingerprint", "scope_id", "generation_id",
		"source_class", "source_id_hash", "chunk_id_hash", "source_hash", "chunk_hash",
		"source_version", "prompt_version", "redaction_version", "extractor_version",
		"extraction_mode", "provider_kind", "provider_profile_id", "provider_profile_class",
		"policy_id", "rule_id", "policy_state", "policy_reason", "guard_state",
		"guard_reason", "actor_class", "acl_state", "classifier_version", "status",
		"provider_job", "retryable", "attempt_count", "failure_class", "failure_message",
		"failure_details", "stale_reason", "response_hash", "budget_metadata",
		"created_at", "updated_at", "last_attempt_at", "next_attempt_at", "stale_at",
	}
}

func semanticQueueUpsertAssignments() []string {
	assignments := []string{}
	for _, column := range semanticQueueColumns()[1:] {
		assignments = append(assignments, column+" = EXCLUDED."+column)
	}
	return assignments
}

func semanticQueueRecordValues(record semanticqueue.Record) ([]any, error) {
	budget, err := json.Marshal(record.Budget)
	if err != nil {
		return nil, fmt.Errorf("marshal semantic budget metadata: %w", err)
	}
	return []any{
		record.JobID, record.WorkItemID, record.Fingerprint, record.ScopeID, record.GenerationID,
		record.SourceClass, record.SourceIDHash, record.ChunkIDHash, record.SourceHash, record.ChunkHash,
		record.SourceVersion, record.PromptVersion, record.RedactionVersion, record.ExtractorVersion,
		record.ExtractionMode, nullString(record.ProviderKind), nullString(record.ProviderProfileID),
		nullString(record.ProviderProfileClass), nullString(record.PolicyID), nullString(record.RuleID),
		nullString(record.PolicyState), nullString(record.PolicyReason), nullString(record.GuardState),
		nullString(record.GuardReason), nullString(record.ActorClass), nullString(record.ACLState),
		nullString(record.ClassifierVersion), string(record.Status), record.ProviderJob, record.Retryable,
		record.AttemptCount, nullString(record.Failure.Class), nullString(record.Failure.Message),
		nullString(record.Failure.Detail), nullString(record.StaleReason), nullString(record.ResponseHash),
		budget, record.CreatedAt, record.UpdatedAt, nullableTimePtr(record.LastAttemptAt),
		nullableTimePtr(record.NextAttemptAt), nullableTimePtr(record.StaleAt),
	}, nil
}

func planRecords(plan semanticqueue.Plan) []semanticqueue.Record {
	records := make([]semanticqueue.Record, 0, len(plan.Jobs)+len(plan.Skipped)+len(plan.Stale))
	records = append(records, plan.Jobs...)
	records = append(records, plan.Skipped...)
	records = append(records, plan.Stale...)
	return records
}

func addSemanticStatusCount(summary *semanticqueue.Summary, status semanticqueue.Status, count int) {
	switch status {
	case semanticqueue.StatusPending, semanticqueue.StatusClaimed, semanticqueue.StatusRetrying:
		summary.Planned += count
	case semanticqueue.StatusSucceeded:
		summary.Succeeded += count
	case semanticqueue.StatusDeadLetter:
		summary.DeadLetter += count
	case semanticqueue.StatusStale:
		summary.Stale += count
	case semanticqueue.StatusSkippedNoProvider:
		summary.NoProvider += count
	case semanticqueue.StatusSkippedPolicy:
		summary.PolicyDenied += count
	case semanticqueue.StatusSkippedBudget:
		summary.BudgetDenied += count
	case semanticqueue.StatusUnsafePayload:
		summary.Unsafe += count
	case semanticqueue.StatusProviderUnavailable:
		summary.ProviderUnavailable += count
	case semanticqueue.StatusSkippedUnchanged:
		summary.Unchanged += count
	}
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableTimePtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}
