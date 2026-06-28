// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

const (
	// defaultProvenanceGroupLimit caps the number of distinct groups (source
	// tools / languages) a provenance count query returns. It bounds label
	// cardinality on the wire, NOT the rows counted — counts are exact below the
	// cap. Both vocabularies are closed and small (source_tool ~25, languages
	// ~40), so the cap is a safety valve, never reached in practice.
	defaultProvenanceGroupLimit = 10000
)

// sourceToolEdgeVerbs is the closed set of Tier-2 relationship types that carry
// a normalized source_tool property (#3999). The edge count is computed per type
// so each query is answered by the relationship-type index (exact and bounded) —
// an unanchored `MATCH ()-[r]->()` scan is forbidden by the cypher-performance
// bounded-read contract. The list is a fixed allowlist, never request input, so
// interpolating it into the query is injection-safe.
var sourceToolEdgeVerbs = []string{
	"DEPENDS_ON",
	"DEPLOYS_FROM",
	"USES_MODULE",
	"READS_CONFIG_FROM",
	"PROVISIONS_DEPENDENCY_FOR",
	"DISCOVERS_CONFIG_IN",
	"RUNS_ON",
}

// ProvenanceCountReader runs bounded graph read queries used by the provenance
// count store. It reuses the same interface shape as OrphanSweepReader so a
// single graph-read adapter satisfies both.
type ProvenanceCountReader interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// ProvenanceCountStore returns exact, bounded edge and file counts for the
// eshu_dp_edges_by_source_tool and eshu_dp_files_by_language observable gauges.
//
// Both queries are index-answered and exact (no row-sampling): the edge count is
// a per-relationship-type aggregate over the closed sourceToolEdgeVerbs set
// (relationship-type index), and the file count is a File-label-anchored group.
// GroupLimit caps only the number of returned groups (label cardinality), so a
// tool or language can never be silently undercounted — a series dropping to
// zero is a real signal, not a sampling artifact. Tune via ESHU_GRAPH_COUNT_LIMIT.
type ProvenanceCountStore struct {
	Reader     ProvenanceCountReader
	GroupLimit int
}

// NewProvenanceCountStore returns a provenance count store backed by the
// provided graph reader.
func NewProvenanceCountStore(reader ProvenanceCountReader) *ProvenanceCountStore {
	return &ProvenanceCountStore{Reader: reader}
}

// EdgesBySourceTool satisfies telemetry.EdgesBySourceToolObserver. It returns
// exact edge counts keyed by source_tool, summed across the Tier-2 verbs that
// carry the property. Only edges where source_tool IS NOT NULL are counted; each
// per-verb aggregate is answered by the relationship-type index.
func (s *ProvenanceCountStore) EdgesBySourceTool(ctx context.Context) (map[string]int64, error) {
	if s == nil || s.Reader == nil {
		return nil, fmt.Errorf("provenance count reader is required")
	}
	result := make(map[string]int64)
	for _, verb := range sourceToolEdgeVerbs {
		// verb is from the fixed sourceToolEdgeVerbs allowlist (never request
		// input), so interpolation is injection-safe; relationship types cannot be
		// parameterized in Cypher.
		cypher := fmt.Sprintf(`MATCH ()-[r:%s]->()
WHERE r.source_tool IS NOT NULL
RETURN r.source_tool AS source_tool, count(r) AS cnt`, verb)
		rows, err := s.Reader.Run(ctx, cypher, nil)
		if err != nil {
			return nil, fmt.Errorf("count %s edges by source_tool: %w", verb, err)
		}
		m, err := provenanceCountMap(rows, "source_tool")
		if err != nil {
			return nil, err
		}
		for tool, cnt := range m {
			result[tool] += cnt
		}
	}
	return result, nil
}

// FilesByLanguage satisfies telemetry.FilesByLanguageObserver. It returns exact
// File node counts keyed by language. The query is File-label-anchored and
// groups by language; the LIMIT bounds the number of returned groups (language
// cardinality), not the File nodes counted, so per-language counts are exact.
func (s *ProvenanceCountStore) FilesByLanguage(ctx context.Context) (map[string]int64, error) {
	if s == nil || s.Reader == nil {
		return nil, fmt.Errorf("provenance count reader is required")
	}
	cypher := `MATCH (f:File)
WHERE f.language IS NOT NULL
RETURN f.language AS language, count(f) AS cnt
ORDER BY cnt DESC
LIMIT $limit`
	rows, err := s.Reader.Run(ctx, cypher, map[string]any{"limit": s.groupLimit()})
	if err != nil {
		return nil, fmt.Errorf("count files by language: %w", err)
	}
	return provenanceCountMap(rows, "language")
}

func (s *ProvenanceCountStore) groupLimit() int {
	if s.GroupLimit > 0 {
		return s.GroupLimit
	}
	return defaultProvenanceGroupLimit
}

// provenanceCountMap converts raw Cypher result rows into a string→int64 map.
// Each row must contain a string column named by keyCol and an integer column
// named "cnt". Empty keys are skipped.
func provenanceCountMap(rows []map[string]any, keyCol string) (map[string]int64, error) {
	result := make(map[string]int64, len(rows))
	for _, row := range rows {
		key, ok := row[keyCol].(string)
		if !ok || key == "" {
			continue
		}
		cnt, ok := int64Count(row["cnt"])
		if !ok {
			return nil, fmt.Errorf("unexpected count type for %s=%q: %T", keyCol, key, row["cnt"])
		}
		result[key] += cnt
	}
	return result, nil
}
