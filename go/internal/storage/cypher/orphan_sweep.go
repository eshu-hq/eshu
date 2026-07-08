// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"math"
	"time"
)

const (
	defaultOrphanSweepBatchLimit = 100
	defaultOrphanSweepCountLimit = 10000
	defaultOrphanSweepTTL        = 7 * 24 * time.Hour
)

// OrphanSweepLabel names one closed graph node label eligible for bounded
// orphan detection and cleanup.
type OrphanSweepLabel string

const (
	// OrphanSweepLabelRepository covers relationship-created repository targets.
	OrphanSweepLabelRepository OrphanSweepLabel = "Repository"
	// OrphanSweepLabelPlatform covers inferred runtime and infrastructure targets.
	OrphanSweepLabelPlatform OrphanSweepLabel = "Platform"
	// OrphanSweepLabelEvidenceArtifact covers repository relationship evidence nodes.
	OrphanSweepLabelEvidenceArtifact OrphanSweepLabel = "EvidenceArtifact"
	// OrphanSweepLabelFile covers disconnected canonical source file nodes.
	OrphanSweepLabelFile OrphanSweepLabel = "File"
	// OrphanSweepLabelDirectory covers disconnected canonical source directory nodes.
	OrphanSweepLabelDirectory OrphanSweepLabel = "Directory"
	// OrphanSweepLabelModule covers disconnected canonical imported module nodes.
	OrphanSweepLabelModule OrphanSweepLabel = "Module"
)

// DefaultOrphanSweepLabels returns the closed orphan sweep label set.
func DefaultOrphanSweepLabels() []OrphanSweepLabel {
	return []OrphanSweepLabel{
		OrphanSweepLabelRepository,
		OrphanSweepLabelPlatform,
		OrphanSweepLabelEvidenceArtifact,
		OrphanSweepLabelFile,
		OrphanSweepLabelDirectory,
		OrphanSweepLabelModule,
	}
}

// OrphanSweepReader runs bounded graph read queries used by the orphan observer.
type OrphanSweepReader interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// OrphanSweepPolicy bounds one orphan sweep cycle.
type OrphanSweepPolicy struct {
	OrphanTTL  time.Duration
	BatchLimit int
	CountLimit int
	Labels     []string
	Now        time.Time
}

// OrphanSweepResult summarizes one graph orphan sweep cycle.
type OrphanSweepResult struct {
	Counts  map[string]int64
	Marked  map[string]int64
	Deleted map[string]int64
	// Skipped counts write statements that were not executed because a
	// preceding cheap count query returned zero. Values are 0..3 per label
	// (clear, mark, sweep).
	Skipped  map[string]int64
	Duration time.Duration
}

// OrphanSweepStore counts, marks, and deletes zero-relationship graph nodes
// through backend-neutral Cypher seams.
type OrphanSweepStore struct {
	Executor   Executor
	Reader     OrphanSweepReader
	CountLimit int
	Labels     []OrphanSweepLabel
	Now        func() time.Time
}

// NewOrphanSweepStore returns a graph orphan sweep store.
func NewOrphanSweepStore(executor Executor, reader OrphanSweepReader) *OrphanSweepStore {
	return &OrphanSweepStore{
		Executor: executor,
		Reader:   reader,
		Labels:   DefaultOrphanSweepLabels(),
	}
}

// SweepOrphanNodes marks newly observed orphan nodes and deletes nodes whose
// orphan marker is older than the configured TTL.
func (s *OrphanSweepStore) SweepOrphanNodes(ctx context.Context, policy OrphanSweepPolicy) (OrphanSweepResult, error) {
	if s == nil || s.Executor == nil {
		return OrphanSweepResult{}, fmt.Errorf("orphan sweep executor is required")
	}
	if s.Reader == nil {
		return OrphanSweepResult{}, fmt.Errorf("orphan sweep reader is required")
	}

	if policy.Now.IsZero() && s.Now != nil {
		policy.Now = s.Now().UTC()
	}
	policy = normalizeOrphanSweepPolicy(policy)
	labels, err := orphanSweepLabels(policy.Labels)
	if err != nil {
		return OrphanSweepResult{}, err
	}
	start := time.Now()
	nowUnix := policy.Now.Unix()
	cutoffUnix := policy.Now.Add(-policy.OrphanTTL).Unix()
	result := OrphanSweepResult{
		Counts:  make(map[string]int64, len(labels)),
		Marked:  make(map[string]int64, len(labels)),
		Deleted: make(map[string]int64, len(labels)),
		Skipped: make(map[string]int64, len(labels)),
	}

	for _, label := range labels {
		labelKey := string(label)
		var skipped int64

		// Each write is gated on a count query whose predicate mirrors that
		// write's own MATCH...WHERE exactly, so a statement is issued only when
		// it will mutate at least one row. This avoids the ~14s fixed-cost
		// NornicDB write transaction (a label MATCH inside a write transaction
		// runs a full-store MVCC visible-at iteration) for the common cases
		// where clear/mark/sweep would otherwise match zero rows. markedCount
		// (a cheap marker-presence read) short-circuits clear and sweep, which
		// can only match when marked nodes exist, so the steady no-orphan state
		// runs two cheap reads and issues zero write transactions.
		markedCount, err := s.countMarkedOrphans(ctx, label, policy.CountLimit)
		if err != nil {
			return OrphanSweepResult{}, err
		}

		count, err := s.countOrphans(ctx, label, policy.CountLimit)
		if err != nil {
			return OrphanSweepResult{}, err
		}
		result.Counts[labelKey] = count

		// clear matches marked nodes that regained a relationship; it can only
		// match when markers exist. Preserve the clear-before-mark ordering.
		if markedCount > 0 {
			relinkedCount, err := s.countMarkedRelinked(ctx, label, policy.CountLimit)
			if err != nil {
				return OrphanSweepResult{}, err
			}
			if relinkedCount > 0 {
				clearStmt, _ := BuildClearOrphanMarkerStatement(label, policy.BatchLimit)
				if err := s.Executor.Execute(ctx, clearStmt); err != nil {
					return OrphanSweepResult{}, fmt.Errorf("clear orphan marker for %s: %w", label, err)
				}
			} else {
				skipped++
			}
		} else {
			// No markers: clear cannot match, skipped without a count read.
			skipped++
		}

		// mark matches unmarked orphans only. When no markers exist every
		// orphan is unmarked, so the total orphan count is exact and no extra
		// read is needed; otherwise count the unmarked orphans precisely.
		markCount := count
		if markedCount > 0 {
			markCount, err = s.countUnmarkedOrphans(ctx, label, policy.CountLimit)
			if err != nil {
				return OrphanSweepResult{}, err
			}
		}
		if markCount > 0 {
			markStmt, _ := BuildMarkOrphanNodesStatement(label, nowUnix, policy.BatchLimit)
			if err := s.Executor.Execute(ctx, markStmt); err != nil {
				return OrphanSweepResult{}, fmt.Errorf("mark orphan nodes for %s: %w", label, err)
			}
			result.Marked[labelKey] = boundedMutationEstimate(markCount, policy.BatchLimit)
		} else {
			result.Marked[labelKey] = 0
			skipped++
		}

		// sweep matches aged marked orphans; it can only match when markers exist.
		if markedCount > 0 {
			agedCount, err := s.countAgedOrphans(ctx, label, cutoffUnix, policy.CountLimit)
			if err != nil {
				return OrphanSweepResult{}, err
			}
			if agedCount > 0 {
				sweepStmt, _ := BuildSweepOrphanNodesStatement(label, cutoffUnix, policy.BatchLimit)
				if err := s.Executor.Execute(ctx, sweepStmt); err != nil {
					return OrphanSweepResult{}, fmt.Errorf("sweep orphan nodes for %s: %w", label, err)
				}
				result.Deleted[labelKey] = boundedMutationEstimate(agedCount, policy.BatchLimit)
			} else {
				result.Deleted[labelKey] = 0
				skipped++
			}
		} else {
			// No markers: sweep cannot match, skipped without a count read.
			result.Deleted[labelKey] = 0
			skipped++
		}

		result.Skipped[labelKey] = skipped
	}
	result.Duration = time.Since(start)
	return result, nil
}

// GraphOrphanNodeCounts returns bounded zero-relationship node counts by label.
func (s *OrphanSweepStore) GraphOrphanNodeCounts(ctx context.Context) (map[string]int64, error) {
	if s == nil || s.Reader == nil {
		return nil, fmt.Errorf("orphan sweep reader is required")
	}
	labels := s.Labels
	if len(labels) == 0 {
		labels = DefaultOrphanSweepLabels()
	}
	limit := s.CountLimit
	if limit <= 0 {
		limit = defaultOrphanSweepCountLimit
	}
	counts := make(map[string]int64, len(labels))
	for _, label := range labels {
		count, err := s.countOrphans(ctx, label, limit)
		if err != nil {
			return nil, err
		}
		counts[string(label)] = count
	}
	return counts, nil
}

// BuildMarkOrphanNodesStatement builds a static-label statement that marks
// newly observed zero-relationship nodes.
func BuildMarkOrphanNodesStatement(label OrphanSweepLabel, observedAtUnix int64, limit int) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`MATCH (n:%s)
WHERE %s
  AND n.eshu_orphan_observed_at_unix IS NULL
  AND NOT (n)--()
WITH n LIMIT $limit
SET n.eshu_orphan_observed_at_unix = $observed_at_unix`, match, orphanSweepEvidencePredicate(label)),
		Parameters: map[string]any{
			"observed_at_unix": observedAtUnix,
			"limit":            normalizePositiveInt(limit, defaultOrphanSweepBatchLimit),
		},
	}, true
}

// BuildSweepOrphanNodesStatement builds a static-label statement that deletes
// aged zero-relationship nodes without DETACH DELETE.
func BuildSweepOrphanNodesStatement(label OrphanSweepLabel, cutoffUnix int64, limit int) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`MATCH (n:%s)
WHERE %s
  AND n.eshu_orphan_observed_at_unix <= $cutoff_unix
  AND NOT (n)--()
WITH n LIMIT $limit
DELETE n`, match, orphanSweepEvidencePredicate(label)),
		Parameters: map[string]any{
			"cutoff_unix": cutoffUnix,
			"limit":       normalizePositiveInt(limit, defaultOrphanSweepBatchLimit),
		},
	}, true
}

// BuildClearOrphanMarkerStatement clears orphan markers from relinked nodes.
func BuildClearOrphanMarkerStatement(label OrphanSweepLabel, limit int) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`MATCH (n:%s)
WHERE n.eshu_orphan_observed_at_unix IS NOT NULL
  AND (n)--()
WITH n LIMIT $limit
REMOVE n.eshu_orphan_observed_at_unix`, match),
		Parameters: map[string]any{
			"limit": normalizePositiveInt(limit, defaultOrphanSweepBatchLimit),
		},
	}, true
}

func buildCountAgedOrphanNodesQuery(label OrphanSweepLabel, cutoffUnix int64, limit int) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`MATCH (n:%s)
WHERE %s
  AND n.eshu_orphan_observed_at_unix <= $cutoff_unix
  AND NOT (n)--()
WITH n LIMIT $limit
RETURN count(n) AS orphan_count`, match, orphanSweepEvidencePredicate(label)),
		Parameters: map[string]any{
			"cutoff_unix": cutoffUnix,
			"limit":       normalizePositiveInt(limit, defaultOrphanSweepCountLimit),
		},
	}, true
}

func orphanLabelMatch(label OrphanSweepLabel) (string, bool) {
	switch label {
	case OrphanSweepLabelRepository:
		return "Repository", true
	case OrphanSweepLabelPlatform:
		return "Platform", true
	case OrphanSweepLabelEvidenceArtifact:
		return "EvidenceArtifact", true
	case OrphanSweepLabelFile:
		return "File", true
	case OrphanSweepLabelDirectory:
		return "Directory", true
	case OrphanSweepLabelModule:
		return "Module", true
	default:
		return "", false
	}
}

func orphanSweepEvidencePredicate(label OrphanSweepLabel) string {
	if label == OrphanSweepLabelRepository {
		return "n.evidence_source IS NOT NULL\n  AND n.evidence_source <> 'projector/canonical'"
	}
	return "n.evidence_source IS NOT NULL"
}

func orphanSweepLabels(raw []string) ([]OrphanSweepLabel, error) {
	if len(raw) == 0 {
		return DefaultOrphanSweepLabels(), nil
	}
	labels := make([]OrphanSweepLabel, 0, len(raw))
	for _, value := range raw {
		label := OrphanSweepLabel(value)
		if _, ok := orphanLabelMatch(label); !ok {
			return nil, fmt.Errorf("unsupported orphan sweep label %q", value)
		}
		labels = append(labels, label)
	}
	return labels, nil
}

func normalizeOrphanSweepPolicy(policy OrphanSweepPolicy) OrphanSweepPolicy {
	if policy.OrphanTTL <= 0 {
		policy.OrphanTTL = defaultOrphanSweepTTL
	}
	policy.BatchLimit = normalizePositiveInt(policy.BatchLimit, defaultOrphanSweepBatchLimit)
	policy.CountLimit = normalizePositiveInt(policy.CountLimit, defaultOrphanSweepCountLimit)
	if policy.Now.IsZero() {
		policy.Now = time.Now().UTC()
	}
	return policy
}

func normalizePositiveInt(value int, defaultValue int) int {
	if value <= 0 {
		return defaultValue
	}
	return value
}

func boundedMutationEstimate(count int64, limit int) int64 {
	if count <= 0 {
		return 0
	}
	if limit <= 0 {
		limit = defaultOrphanSweepBatchLimit
	}
	return min(count, int64(limit))
}

func int64Count(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		if typed < 0 || typed > math.MaxInt64 || math.Trunc(typed) != typed {
			return 0, false
		}
		return int64(typed), true
	default:
		return 0, false
	}
}
