// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import "time"

// LiveActivityRow is one in-flight work item (status claimed, running, or
// retrying) joined to its originating ingestion scope, giving an operator
// repo x stage x worker attribution for the live operations board (#5137).
// Unlike every other Report section, it is sourced from a single bounded,
// operator-limited Postgres query rather than the fixed RawSnapshot
// aggregate path, so it travels through OperationsReport instead of Report.
type LiveActivityRow struct {
	// WorkItemID is the durable fact_work_items primary key.
	WorkItemID string
	// Stage is the pipeline stage the item is queued for (e.g. "reducer").
	Stage string
	// Status is the current queue status: claimed, running, or retrying.
	Status string
	// Domain is the reducer/projection domain the item belongs to.
	Domain string
	// LeaseOwner is the worker holding the claim, empty when unclaimed.
	LeaseOwner string
	// ClaimUntil is the active lease deadline; zero when the item holds no
	// live claim (for example a retrying item waiting to become visible).
	ClaimUntil time.Time
	// AttemptCount is the number of attempts made so far.
	AttemptCount int
	// UpdatedAt is the last time this row changed; the primary staleness and
	// ordering signal for the live board.
	UpdatedAt time.Time
	// CreatedAt is when the work item was first enqueued.
	CreatedAt time.Time
	// ScopeKind is the originating ingestion_scopes.scope_kind.
	ScopeKind string
	// CollectorKind is the originating ingestion_scopes.collector_kind.
	CollectorKind string
	// SourceSystem is the originating ingestion_scopes.source_system.
	SourceSystem string
	// SourceKey is the originating ingestion_scopes.source_key (repository
	// identity); redacted for scoped callers by the query-layer projection.
	SourceKey string
	// SourceDisplay is the operator-facing repo name (#5137 follow-up):
	// ingestion_scopes.payload's repo_slug or repo_name for git scopes,
	// falling back to SourceKey when the payload carries neither. SourceKey
	// alone is an opaque hash for git scopes (e.g. "repository:r_ea78e8bb"),
	// not a name an operator can recognize. Redacted for scoped callers by
	// the query-layer projection, exactly like SourceKey.
	SourceDisplay string
	// GenerationState reports whether this row's generation is the scope's
	// current active generation ("active") or an older, superseded/stale one
	// ("stale"), #5138. Only a retrying row can ever be "stale": claimed and
	// running rows are always "active" regardless of generation, mirroring
	// activeFactWorkItemsCTE's own carve-out (a live claimed/running worker
	// stays diagnosable even against a stale generation). A stale row is
	// never excluded from LiveActivity -- hiding it would erase evidence that
	// a generation-superseded retry is still consuming queue capacity, which
	// is itself an operator-relevant signal -- it is annotated instead so the
	// console can render it dimmed rather than as ordinary in-flight work.
	GenerationState string
}

// Age returns how long ago the row last changed, relative to asOf. It
// returns zero when UpdatedAt is unset or in the future relative to asOf, so
// a caller never renders a negative age from clock skew between the
// snapshot's asOf and a row's own timestamp.
func (r LiveActivityRow) Age(asOf time.Time) time.Duration {
	if r.UpdatedAt.IsZero() || asOf.Before(r.UpdatedAt) {
		return 0
	}
	return asOf.Sub(r.UpdatedAt)
}

// OperationsReport is the bounded operator read model for the live
// operations board (issue #5137): health, collector runtimes (with
// heartbeat), stage summaries, domain backlogs, and queue pressure --
// sections already available on an already-loaded Report, added at no extra
// I/O cost -- composed with LiveActivity, the one section backed by its own
// bounded Postgres query.
type OperationsReport struct {
	// AsOf is the snapshot time inherited from the projected Report.
	AsOf time.Time
	// Health is the operator-facing health verdict and reasons.
	Health HealthSummary
	// Collectors is the unified collector runtime view (coordinator
	// registration, direct status evidence, and persisted fact evidence),
	// each carrying LastObservedAt as its heartbeat signal.
	Collectors []CollectorRuntimeStatus
	// StageSummaries collapses queue counts into one row per pipeline stage.
	StageSummaries []StageSummary
	// DomainBacklogs lists reducer/projection domain backlogs.
	DomainBacklogs []DomainBacklog
	// Queue is the aggregate queue depth and claim-latency snapshot.
	Queue QueueSnapshot
	// LiveActivity lists up to Limit in-flight work items ordered by
	// most-recently-updated first, each joined to its originating repo.
	LiveActivity []LiveActivityRow
	// Truncated reports whether more in-flight rows existed than Limit
	// allowed through.
	Truncated bool
	// Limit is the bound requested for LiveActivity.
	Limit int
}

// Operations composes an already-loaded Report with a separately-fetched,
// bounded live-activity slice into the operations-board read model. Like
// ControlPlane, it performs no I/O of its own: the caller loads the Report
// through the normal status.Reader path and fetches LiveActivity through its
// own bounded query, then calls Operations once to project both into one
// response shape.
func Operations(report Report, activity []LiveActivityRow, truncated bool, limit int) OperationsReport {
	return OperationsReport{
		AsOf:           report.AsOf,
		Health:         report.Health,
		Collectors:     CollectorRuntimeStatuses(report),
		StageSummaries: cloneStageSummaries(report.StageSummaries),
		DomainBacklogs: cloneDomainBacklogs(report.DomainBacklogs),
		Queue:          report.Queue,
		LiveActivity:   cloneLiveActivity(activity),
		Truncated:      truncated,
		Limit:          limit,
	}
}

func cloneStageSummaries(rows []StageSummary) []StageSummary {
	if len(rows) == 0 {
		return nil
	}
	out := make([]StageSummary, len(rows))
	copy(out, rows)
	return out
}

func cloneDomainBacklogs(rows []DomainBacklog) []DomainBacklog {
	if len(rows) == 0 {
		return nil
	}
	out := make([]DomainBacklog, len(rows))
	copy(out, rows)
	return out
}

func cloneLiveActivity(rows []LiveActivityRow) []LiveActivityRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]LiveActivityRow, len(rows))
	copy(out, rows)
	return out
}
