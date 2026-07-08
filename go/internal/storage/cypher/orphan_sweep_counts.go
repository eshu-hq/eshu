// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// This file holds the bounded orphan-sweep count queries and the store
// helpers that run them. Each count mirrors a write statement's predicate so
// SweepOrphanNodes can gate the write on a cheap read (see orphan_sweep.go).

func (s *OrphanSweepStore) countOrphans(ctx context.Context, label OrphanSweepLabel, limit int) (int64, error) {
	stmt, ok := BuildCountOrphanNodesQuery(label, limit)
	if !ok {
		return 0, fmt.Errorf("unsupported orphan sweep label %q", label)
	}
	return s.countWithStatement(ctx, stmt, label)
}

func (s *OrphanSweepStore) countMarkedOrphans(ctx context.Context, label OrphanSweepLabel, limit int) (int64, error) {
	stmt, ok := BuildCountMarkedOrphanNodesQuery(label, limit)
	if !ok {
		return 0, fmt.Errorf("unsupported orphan sweep label %q", label)
	}
	return s.countWithStatement(ctx, stmt, label)
}

func (s *OrphanSweepStore) countUnmarkedOrphans(ctx context.Context, label OrphanSweepLabel, limit int) (int64, error) {
	stmt, ok := BuildCountUnmarkedOrphanNodesQuery(label, limit)
	if !ok {
		return 0, fmt.Errorf("unsupported orphan sweep label %q", label)
	}
	return s.countWithStatement(ctx, stmt, label)
}

func (s *OrphanSweepStore) countMarkedRelinked(ctx context.Context, label OrphanSweepLabel, limit int) (int64, error) {
	stmt, ok := BuildCountMarkedRelinkedNodesQuery(label, limit)
	if !ok {
		return 0, fmt.Errorf("unsupported orphan sweep label %q", label)
	}
	return s.countWithStatement(ctx, stmt, label)
}

func (s *OrphanSweepStore) countAgedOrphans(
	ctx context.Context,
	label OrphanSweepLabel,
	cutoffUnix int64,
	limit int,
) (int64, error) {
	stmt, ok := buildCountAgedOrphanNodesQuery(label, cutoffUnix, limit)
	if !ok {
		return 0, fmt.Errorf("unsupported orphan sweep label %q", label)
	}
	return s.countWithStatement(ctx, stmt, label)
}

func (s *OrphanSweepStore) countWithStatement(ctx context.Context, stmt Statement, label OrphanSweepLabel) (int64, error) {
	rows, err := s.Reader.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return 0, fmt.Errorf("count orphan nodes for %s: %w", label, err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	count, ok := int64Count(rows[0]["orphan_count"])
	if !ok {
		return 0, fmt.Errorf("count orphan nodes for %s: unexpected count type %T", label, rows[0]["orphan_count"])
	}
	return count, nil
}

// BuildCountOrphanNodesQuery builds a bounded static-label count query.
func BuildCountOrphanNodesQuery(label OrphanSweepLabel, limit int) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`MATCH (n:%s)
WHERE %s
  AND NOT (n)--()
WITH n LIMIT $limit
RETURN count(n) AS orphan_count`, match, orphanSweepEvidencePredicate(label)),
		Parameters: map[string]any{
			"limit": normalizePositiveInt(limit, defaultOrphanSweepCountLimit),
		},
	}, true
}

// BuildCountMarkedOrphanNodesQuery builds a bounded static-label query that
// counts nodes carrying the orphan marker regardless of relationship state.
func BuildCountMarkedOrphanNodesQuery(label OrphanSweepLabel, limit int) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`MATCH (n:%s)
WHERE n.eshu_orphan_observed_at_unix IS NOT NULL
WITH n LIMIT $limit
RETURN count(n) AS orphan_count`, match),
		Parameters: map[string]any{
			"limit": normalizePositiveInt(limit, defaultOrphanSweepCountLimit),
		},
	}, true
}

// BuildCountUnmarkedOrphanNodesQuery builds a bounded static-label query that
// counts unmarked zero-relationship nodes. Its predicate mirrors
// BuildMarkOrphanNodesStatement exactly so a positive count guarantees the
// mark write matches at least one row.
func BuildCountUnmarkedOrphanNodesQuery(label OrphanSweepLabel, limit int) (Statement, bool) {
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
RETURN count(n) AS orphan_count`, match, orphanSweepEvidencePredicate(label)),
		Parameters: map[string]any{
			"limit": normalizePositiveInt(limit, defaultOrphanSweepCountLimit),
		},
	}, true
}

// BuildCountMarkedRelinkedNodesQuery builds a bounded static-label query that
// counts marked nodes that have regained a relationship. Its predicate mirrors
// BuildClearOrphanMarkerStatement exactly so a positive count guarantees the
// clear write matches at least one row.
func BuildCountMarkedRelinkedNodesQuery(label OrphanSweepLabel, limit int) (Statement, bool) {
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
RETURN count(n) AS orphan_count`, match),
		Parameters: map[string]any{
			"limit": normalizePositiveInt(limit, defaultOrphanSweepCountLimit),
		},
	}, true
}
