// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"strings"
	"time"
)

// MaxGenerationLifecycleLimit is the hard upper bound on rows a single
// generation lifecycle drilldown page may return. Callers that ask for more
// are clamped to this cap so a broad scan cannot return an unbounded payload.
const MaxGenerationLifecycleLimit = 500

// DefaultGenerationLifecycleLimit is the page size used when a caller does not
// specify a limit.
const DefaultGenerationLifecycleLimit = 50

// GenerationLifecycleFilter bounds a generation lifecycle drilldown to a scope,
// repository, collector, source system, generation, or status. At least one
// selector should be supplied by callers that want a scoped answer; the
// storage reader still enforces Limit regardless of selector breadth.
//
// Repository selects repository-kind scopes by their canonical repo id (the
// scope source_key for git repository scopes). ScopeID and GenerationID select
// an exact row. Status filters by generation lifecycle status
// (pending|active|superseded|completed|failed).
type GenerationLifecycleFilter struct {
	ScopeID       string
	Repository    string
	CollectorKind string
	SourceSystem  string
	GenerationID  string
	Status        string
	Limit         int
}

// Normalize trims selector whitespace and clamps Limit into the supported
// range. A zero or negative Limit becomes DefaultGenerationLifecycleLimit and a
// value above MaxGenerationLifecycleLimit is clamped to the cap.
func (f GenerationLifecycleFilter) Normalize() GenerationLifecycleFilter {
	f.ScopeID = strings.TrimSpace(f.ScopeID)
	f.Repository = strings.TrimSpace(f.Repository)
	f.CollectorKind = strings.TrimSpace(f.CollectorKind)
	f.SourceSystem = strings.TrimSpace(f.SourceSystem)
	f.GenerationID = strings.TrimSpace(f.GenerationID)
	f.Status = strings.TrimSpace(f.Status)
	if f.Limit <= 0 {
		f.Limit = DefaultGenerationLifecycleLimit
	}
	if f.Limit > MaxGenerationLifecycleLimit {
		f.Limit = MaxGenerationLifecycleLimit
	}
	return f
}

// HasScopeSelector reports whether the filter names a specific scope,
// repository, or generation. It is used to decide whether an empty result is an
// explicit not-found (a named selector matched nothing) instead of a confident
// empty list for a broad scan.
func (f GenerationLifecycleFilter) HasScopeSelector() bool {
	return strings.TrimSpace(f.ScopeID) != "" ||
		strings.TrimSpace(f.Repository) != "" ||
		strings.TrimSpace(f.GenerationID) != ""
}

// GenerationLifecycleRecord is one scope-generation lifecycle row joined with
// its owning scope identity and the per-generation queue status and latest
// failure. It is the drilldown unit returned by the freshness generation
// surface; timestamps are RFC3339 UTC strings and empty when the underlying
// column is null.
type GenerationLifecycleRecord struct {
	ScopeID                   string `json:"scope_id"`
	GenerationID              string `json:"generation_id"`
	ScopeKind                 string `json:"scope_kind"`
	SourceSystem              string `json:"source_system"`
	CollectorKind             string `json:"collector_kind"`
	CurrentActiveGenerationID string `json:"current_active_generation_id,omitempty"`
	IsActive                  bool   `json:"is_active"`
	TriggerKind               string `json:"trigger_kind"`
	FreshnessHint             string `json:"freshness_hint,omitempty"`
	Status                    string `json:"status"`
	ObservedAt                string `json:"observed_at,omitempty"`
	IngestedAt                string `json:"ingested_at,omitempty"`
	ActivatedAt               string `json:"activated_at,omitempty"`
	SupersededAt              string `json:"superseded_at,omitempty"`
	// QueueStatus rolls up the fact_work_items status for this generation:
	// outstanding (pending/claimed/running/retrying), in-flight, failed,
	// dead-lettered, succeeded, and total work item counts.
	QueueStatus GenerationQueueStatus `json:"queue_status"`
	// LatestFailure carries the most recent fact_work_items failure for this
	// generation. It is nil when no work item for the generation recorded a
	// failure class.
	LatestFailure *GenerationLatestFailure `json:"latest_failure,omitempty"`
}

// GenerationQueueStatus summarizes the fact_work_items queue rows that belong to
// a single generation. All counts are bounded by the work items emitted for the
// generation and never include other generations.
type GenerationQueueStatus struct {
	Total       int `json:"total"`
	Outstanding int `json:"outstanding"`
	InFlight    int `json:"in_flight"`
	Retrying    int `json:"retrying"`
	Succeeded   int `json:"succeeded"`
	Failed      int `json:"failed"`
	DeadLetter  int `json:"dead_letter"`
}

// GenerationLatestFailure carries the most recent failure class and message
// recorded by a fact_work_items row for a generation. Message is bounded by the
// stored failure_message column; it is operator-facing and not redacted here.
type GenerationLatestFailure struct {
	FailureClass   string `json:"failure_class"`
	FailureMessage string `json:"failure_message,omitempty"`
	WorkItemStatus string `json:"work_item_status,omitempty"`
	ObservedAt     string `json:"observed_at,omitempty"`
}

// GenerationLifecyclePage is one bounded, ordered drilldown page. Truncated is
// true when more rows matched the filter than the requested Limit, signaling
// the caller to narrow the scope or page again.
type GenerationLifecyclePage struct {
	Records   []GenerationLifecycleRecord `json:"records"`
	Limit     int                         `json:"limit"`
	Truncated bool                        `json:"truncated"`
}

// GenerationLifecycleTimestamp formats a database timestamp as RFC3339 UTC, or
// the empty string when the value is zero. It centralizes the timestamp shape
// the drilldown contract promises so the storage reader and any projection stay
// in lockstep.
func GenerationLifecycleTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
