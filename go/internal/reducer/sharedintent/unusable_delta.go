// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// UnusableDeltaScopeReason is the bounded unroutable reason for a refresh
// intent on a delta generation whose delta file scope is unusable (#10069).
// The generation carries content facts for its changed files only, and none
// of the changed paths could be qualified, so no correct retract exists: the
// file-scoped one has nothing to bind and the repo-wide one would delete
// every unchanged file's edge with nothing left to restore it. The row is
// permanently unroutable and the loss (deleted-file edges that stay stale) is
// real, so it shares the missing-required-field reading, not the version-skew
// one: no writer version can route it.
const UnusableDeltaScopeReason = "unusable_delta_scope"

// UnusableDeltaScopeError is the fail-closed rejection a retract issues for a
// delta-flagged row that carries no usable delta file paths. It is a typed
// error rather than a bare fmt.Errorf so the worker that owns intent
// completion can tell this deterministic payload defect from a transient
// backend failure: the same row fails identically on every poll, so retrying
// it is head-of-line blocking, while a transient failure must keep retrying.
// See IsUnusableDeltaScopeError.
type UnusableDeltaScopeError struct {
	// SampleRepositoryID names one repository an operator should look at.
	// The usual cause is a repository fact with no local_path, or a symlinked
	// repos root whose changed paths normalizeSnapshotRelativePaths drops.
	SampleRepositoryID string
	// DeltaRowCount counts the delta-flagged rows the rejected batch held.
	DeltaRowCount int
	// RowCarriesNoPaths selects the per-row rejection message: one row
	// carried no delta_file_paths at all. When false the batch carried paths
	// that all normalized away, and the message reports the row count.
	RowCarriesNoPaths bool
}

// NewUnusableDeltaScopeRowError builds the rejection for one delta-flagged
// row that carries no delta_file_paths. The message names the repository so
// the dead letter is actionable.
func NewUnusableDeltaScopeRowError(repositoryID string) *UnusableDeltaScopeError {
	return &UnusableDeltaScopeError{SampleRepositoryID: repositoryID, DeltaRowCount: 1, RowCarriesNoPaths: true}
}

// NewUnusableDeltaScopeBatchError builds the rejection for a batch whose
// delta-flagged rows carried paths that all normalized away.
func NewUnusableDeltaScopeBatchError(deltaRowCount int) *UnusableDeltaScopeError {
	return &UnusableDeltaScopeError{DeltaRowCount: deltaRowCount}
}

// Error reports the fail-closed rejection. Both messages keep the
// "delta retract requires delta_file_paths" prefix the writer's regression
// proof matches on.
func (e *UnusableDeltaScopeError) Error() string {
	if e.RowCarriesNoPaths {
		return fmt.Sprintf(
			"delta retract requires delta_file_paths: repository %q carries none",
			e.SampleRepositoryID,
		)
	}
	return fmt.Sprintf(
		"delta retract requires delta_file_paths: no path survived across %d delta-flagged row(s)",
		e.DeltaRowCount,
	)
}

// IsUnusableDeltaScopeError reports whether err is (or wraps) the fail-closed
// unusable-delta rejection. It matches through %w chains, so a retract that
// returns the typed error bare and a worker that wraps it as "retract edges"
// classify identically. A plain error whose text merely mentions delta paths
// does NOT match: only the type proves the rejection came from the
// fail-closed collector rather than a backend fault, so transient failures
// keep retrying.
func IsUnusableDeltaScopeError(err error) bool {
	var target *UnusableDeltaScopeError
	return errors.As(err, &target)
}

// RowHasUsableDeltaScope reports whether a row carries a delta file scope the
// retract can bind. A row that is not delta-flagged is trivially usable: the
// predicate only judges delta scopes. A delta-flagged row is usable only when
// at least one of its delta_file_paths survives trimming.
//
// The normalization mirrors the writer's delta collector exactly, so the
// worker's quarantine split and the writer's rejection agree row for row: the
// flag reads only a real bool, a []string is taken as-is, a []any is
// stringified per item (skipping "" and "<nil>"), anything else carries
// nothing, and blank entries never count. Keep the two in lockstep; a row the
// predicate calls usable but the writer rejects (or vice versa) either
// retries forever or quarantines a healthy row.
func RowHasUsableDeltaScope(row Row) bool {
	if !payloadcore.BoolPayload(row.Payload, "delta_projection") {
		return true
	}
	return len(usableDeltaFilePaths(row.Payload)) > 0
}

// RowHasUnusableDeltaScope reports whether a row is delta-flagged yet carries
// no usable delta file path: the exact shape the retract fails closed on.
// The same payload fails identically on every poll, so such a row must be
// dead-lettered rather than retried.
func RowHasUnusableDeltaScope(row Row) bool {
	if !payloadcore.BoolPayload(row.Payload, "delta_projection") {
		return false
	}
	return len(usableDeltaFilePaths(row.Payload)) == 0
}

// SplitUnusableDeltaScopeRows partitions retract rows into the rows whose
// retract can run and the rows whose delta scope is unusable. Non-delta rows
// are always usable. The split is order-preserving on both sides so the
// retried batch keeps the caller's ordering.
func SplitUnusableDeltaScopeRows(rows []Row) (usable, unusable []Row) {
	for _, row := range rows {
		if RowHasUnusableDeltaScope(row) {
			unusable = append(unusable, row)
			continue
		}
		usable = append(usable, row)
	}
	return usable, unusable
}

// usableDeltaFilePaths returns the trimmed, non-empty delta_file_paths of a
// payload under the writer's normalization.
func usableDeltaFilePaths(payload map[string]any) []string {
	if payload == nil {
		return nil
	}
	value, ok := payload["delta_file_paths"]
	if !ok || value == nil {
		return nil
	}
	var raw []string
	switch typed := value.(type) {
	case []string:
		raw = typed
	case []any:
		raw = make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text == "" || text == "<nil>" {
				continue
			}
			raw = append(raw, text)
		}
	default:
		return nil
	}
	paths := make([]string, 0, len(raw))
	for _, path := range raw {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

// UnroutableRowForUnusableDeltaScope builds the durable dead-letter row for
// one quarantined refresh intent. The intent id joins back to
// shared_projection_intents and the scope/generation/domain triple is how an
// operator scopes the query; the reason stays the bounded
// UnusableDeltaScopeReason so the counter label stays low-cardinality.
func UnroutableRowForUnusableDeltaScope(row Row, evidenceSource string, decidedAt time.Time) UnroutableRow {
	return UnroutableRow{
		IntentID:         row.IntentID,
		ProjectionDomain: row.ProjectionDomain,
		PartitionKey:     row.PartitionKey,
		RepositoryID:     RowRepoID(row),
		ScopeID:          row.ScopeID,
		GenerationID:     row.GenerationID,
		EvidenceSource:   evidenceSource,
		Reason:           UnusableDeltaScopeReason,
		DecidedAt:        decidedAt.UTC(),
	}
}
