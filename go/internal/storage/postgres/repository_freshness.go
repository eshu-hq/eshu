// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RepositoryFreshnessStore reads the per-repository commit-receipt and
// build-completeness evidence backing GET /api/v0/repositories/{id}/freshness
// (#5143). It is a narrow, dedicated store -- like LiveActivityStore -- rather
// than a StatusStore section, because it is keyed by a single repository
// selector rather than composing the fixed RawSnapshot shape.
type RepositoryFreshnessStore struct {
	queryer Queryer
	// Instruments is left nil by NewRepositoryFreshnessStore (matching
	// LiveActivityStore's convention) so existing construction call sites
	// stay source-compatible; NewInstrumentedRepositoryFreshnessStore is the
	// wiring entry point that wants the duration/error signals.
	Instruments *telemetry.Instruments
}

// NewRepositoryFreshnessStore constructs a read-only repository freshness
// store with no telemetry wired.
func NewRepositoryFreshnessStore(queryer Queryer) RepositoryFreshnessStore {
	return RepositoryFreshnessStore{queryer: queryer}
}

// NewInstrumentedRepositoryFreshnessStore constructs a repository freshness
// store that records
// eshu_dp_repository_freshness_query_duration_seconds and
// eshu_dp_repository_freshness_query_errors_total on every read.
func NewInstrumentedRepositoryFreshnessStore(queryer Queryer, instruments *telemetry.Instruments) RepositoryFreshnessStore {
	return RepositoryFreshnessStore{queryer: queryer, Instruments: instruments}
}

// ReadRepositoryFreshness reads the freshness snapshot for one canonical
// repository id: the composite single-scope read (resolve -> generation ->
// stage counts -> shared-enrichment pending), then the separate bounded
// webhook-trigger lookup. It is one instrumented Go-level composite read
// backed by four tightly-scoped, index-bound SQL statements -- see this
// package's README, "Repo freshness single-scope composite read (#5143)",
// for the measured shape.
//
// A repoID that resolves to no scope returns a snapshot with Resolved=false
// and a nil error: an unresolved repository is not a query failure, it is
// evidence the verdict function represents as "unknown".
func (s RepositoryFreshnessStore) ReadRepositoryFreshness(ctx context.Context, repoID string) (statuspkg.RepositoryFreshnessSnapshot, error) {
	start := time.Now()
	snapshot, err := s.readRepositoryFreshness(ctx, repoID)
	s.recordOutcome(ctx, time.Since(start), err)
	return snapshot, err
}

func (s RepositoryFreshnessStore) readRepositoryFreshness(ctx context.Context, repoID string) (statuspkg.RepositoryFreshnessSnapshot, error) {
	snapshot := statuspkg.RepositoryFreshnessSnapshot{RepositoryID: repoID}
	if s.queryer == nil {
		return snapshot, fmt.Errorf("read repository freshness: queryer is required")
	}
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return snapshot, nil
	}

	scopeID, generationID, resolved, err := s.resolveScope(ctx, repoID)
	if err != nil {
		return snapshot, err
	}
	if !resolved {
		return snapshot, nil
	}
	snapshot.ScopeID = scopeID
	snapshot.Resolved = true

	generation, repoDisplay, hasGeneration, err := s.readGeneration(ctx, scopeID, generationID)
	if err != nil {
		return snapshot, err
	}
	if !hasGeneration {
		return snapshot, nil
	}
	snapshot.HasGeneration = true
	snapshot.ScopeKind = generation.scopeKind
	snapshot.Generation = statuspkg.RepositoryFreshnessGeneration{
		ID:          generation.id,
		Status:      generation.status,
		TriggerKind: generation.triggerKind,
		IsDelta:     generation.isDelta,
		ActivatedAt: generation.activatedAt,
	}
	snapshot.ObservedCommit = generation.sourceCommitSHA
	snapshot.ObservedAt = generation.observedAt

	outstanding, err := s.readStageCounts(ctx, scopeID, generationID)
	if err != nil {
		return snapshot, err
	}
	snapshot.Outstanding = outstanding
	snapshot.Stages = deriveRepositoryFreshnessStages(outstanding)

	pendingDomains, err := s.readSharedPending(ctx, repoID, generationID)
	if err != nil {
		return snapshot, err
	}
	snapshot.SharedEnrichment = statuspkg.RepositoryFreshnessSharedEnrichment{
		Pending:        len(pendingDomains) > 0,
		PendingDomains: pendingDomains,
	}
	snapshot.Stages.Materialized = len(pendingDomains) == 0

	if repoDisplay != "" {
		unobserved, err := s.readUnobservedPush(ctx, repoDisplay, snapshot.ObservedCommit)
		if err != nil {
			return snapshot, err
		}
		snapshot.UnobservedPush = unobserved
	}

	return snapshot, nil
}

func (s RepositoryFreshnessStore) resolveScope(ctx context.Context, repoID string) (scopeID, generationID string, resolved bool, err error) {
	rows, err := s.queryer.QueryContext(ctx, repositoryFreshnessResolveQuery, repoID)
	if err != nil {
		return "", "", false, fmt.Errorf("read repository freshness: resolve scope: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return "", "", false, rows.Err()
	}
	if scanErr := rows.Scan(&scopeID, &generationID); scanErr != nil {
		return "", "", false, fmt.Errorf("read repository freshness: resolve scope: %w", scanErr)
	}
	return scopeID, generationID, true, rows.Err()
}

// repositoryFreshnessGenerationRow is the scanned lifecycle row from
// repositoryFreshnessGenerationQuery, kept unexported since it exists only to
// pass scanned columns to statuspkg.RepositoryFreshnessGeneration and the
// webhook lookup.
type repositoryFreshnessGenerationRow struct {
	id              string
	status          string
	triggerKind     string
	isDelta         bool
	activatedAt     time.Time
	sourceCommitSHA string
	observedAt      time.Time
	scopeKind       string
}

func (s RepositoryFreshnessStore) readGeneration(ctx context.Context, scopeID, generationID string) (repositoryFreshnessGenerationRow, string, bool, error) {
	var row repositoryFreshnessGenerationRow
	var repoDisplay string

	rows, err := s.queryer.QueryContext(ctx, repositoryFreshnessGenerationQuery, scopeID, generationID)
	if err != nil {
		return row, "", false, fmt.Errorf("read repository freshness: read generation: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return row, "", false, rows.Err()
	}
	var activatedAt sql.NullTime
	if scanErr := rows.Scan(
		&row.id, &row.status, &row.triggerKind, &row.isDelta, &activatedAt,
		&row.sourceCommitSHA, &row.observedAt, &row.scopeKind, &repoDisplay,
	); scanErr != nil {
		return row, "", false, fmt.Errorf("read repository freshness: read generation: %w", scanErr)
	}
	if activatedAt.Valid {
		row.activatedAt = activatedAt.Time
	}
	return row, repoDisplay, true, rows.Err()
}

func (s RepositoryFreshnessStore) readStageCounts(ctx context.Context, scopeID, generationID string) ([]statuspkg.RepositoryFreshnessOutstanding, error) {
	rows, err := s.queryer.QueryContext(ctx, repositoryFreshnessStageCountsQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("read repository freshness: read stage counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []statuspkg.RepositoryFreshnessOutstanding
	for rows.Next() {
		var row statuspkg.RepositoryFreshnessOutstanding
		var count int64
		if scanErr := rows.Scan(&row.Stage, &row.Status, &count); scanErr != nil {
			return nil, fmt.Errorf("read repository freshness: read stage counts: %w", scanErr)
		}
		row.Count = int(count)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s RepositoryFreshnessStore) readSharedPending(ctx context.Context, repoID, generationID string) ([]statuspkg.RepositoryFreshnessPendingDomain, error) {
	rows, err := s.queryer.QueryContext(ctx, repositoryFreshnessSharedPendingQuery, repoID, generationID)
	if err != nil {
		return nil, fmt.Errorf("read repository freshness: read shared pending: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []statuspkg.RepositoryFreshnessPendingDomain
	for rows.Next() {
		var row statuspkg.RepositoryFreshnessPendingDomain
		var count int64
		if scanErr := rows.Scan(&row.Domain, &count); scanErr != nil {
			return nil, fmt.Errorf("read repository freshness: read shared pending: %w", scanErr)
		}
		row.Count = int(count)
		out = append(out, row)
	}
	return out, rows.Err()
}

// readUnobservedPush returns the newest queued/claimed webhook trigger for
// repoDisplay whose target commit does not match observedCommit -- eshu has
// not started building it under the resolved generation. Scope limitation
// (documented rather than hidden): this compares only against the resolved
// generation's own observed commit, not the full scope_generations history,
// since this read is bounded to a single (scope, generation); a target_sha
// that happens to match an OLDER superseded generation would still surface
// here as unobserved relative to the current generation.
func (s RepositoryFreshnessStore) readUnobservedPush(ctx context.Context, repoDisplay, observedCommit string) (*statuspkg.RepositoryFreshnessUnobservedPush, error) {
	rows, err := s.queryer.QueryContext(ctx, repositoryFreshnessWebhookQuery, repoDisplay)
	if err != nil {
		return nil, fmt.Errorf("read repository freshness: read webhook triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var targetSHA, ref string
		var receivedAt time.Time
		if scanErr := rows.Scan(&targetSHA, &ref, &receivedAt); scanErr != nil {
			return nil, fmt.Errorf("read repository freshness: read webhook triggers: %w", scanErr)
		}
		if targetSHA != "" && targetSHA != observedCommit {
			return &statuspkg.RepositoryFreshnessUnobservedPush{
				TargetSHA:  targetSHA,
				Ref:        ref,
				ReceivedAt: receivedAt,
			}, rows.Err()
		}
	}
	return nil, rows.Err()
}

// deriveRepositoryFreshnessStages converts the per-(stage,status) outstanding
// rows into the collected/reduced/projected boolean checklist.
// Collected is always true here -- this helper only runs once a generation
// has already been resolved, and collection precedes the scope_generations
// row's own existence. Materialized is set by the caller from the
// shared-enrichment read, a separate axis from fact_work_items.
func deriveRepositoryFreshnessStages(outstanding []statuspkg.RepositoryFreshnessOutstanding) statuspkg.RepositoryFreshnessStages {
	stages := statuspkg.RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true}
	for _, row := range outstanding {
		if row.Status == "succeeded" {
			continue
		}
		switch row.Stage {
		case "reducer":
			stages.Reduced = false
		case "projector":
			stages.Projected = false
		}
	}
	return stages
}

// recordOutcome is nil-safe: when Instruments (or either instrument on it) is
// nil, recording is a no-op so RepositoryFreshnessStore stays usable without
// a meter provider wired, matching LiveActivityStore's Instruments
// convention.
func (s RepositoryFreshnessStore) recordOutcome(ctx context.Context, duration time.Duration, err error) {
	if s.Instruments == nil {
		return
	}
	if s.Instruments.RepositoryFreshnessQueryDuration != nil {
		s.Instruments.RepositoryFreshnessQueryDuration.Record(ctx, duration.Seconds())
	}
	if err != nil && s.Instruments.RepositoryFreshnessQueryErrors != nil {
		s.Instruments.RepositoryFreshnessQueryErrors.Add(ctx, 1)
	}
}
