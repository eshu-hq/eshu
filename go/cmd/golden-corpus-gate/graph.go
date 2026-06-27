// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// identRE constrains graph labels and relationship types to a safe identifier
// shape. Labels and relationship types cannot be parameterized in Cypher, so the
// gate interpolates them into the query string; this allowlist prevents a
// malformed or hostile snapshot from injecting Cypher. The snapshot is a trusted
// committed contract, but defense in depth is cheap.
var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// graphCounter executes scalar-count Cypher against the graph backend. It is the
// only graph-facing seam the gate needs, so it is defined here where it is
// consumed and faked in tests.
type graphCounter interface {
	// CountNodes returns the number of nodes carrying label.
	CountNodes(ctx context.Context, label string) (int64, error)
	// CountEdges returns the number of relationships of the given type.
	CountEdges(ctx context.Context, relationship string) (int64, error)
	// CountCorrelation returns the number of (from)-[rel]->(to) paths.
	CountCorrelation(ctx context.Context, from, rel, to string) (int64, error)
	// CountCorrelationWithEvidence returns the number of (from)-[rel]->(to) paths
	// whose rel.evidence_kinds property contains every kind in evidenceKinds. It
	// isolates a single verb on a shared, tool-agnostic edge type (see
	// RequiredCorrelation.EvidenceKinds).
	CountCorrelationWithEvidence(ctx context.Context, from, rel, to string, evidenceKinds []string) (int64, error)
}

// boltGraphCounter runs counts over the shared Bolt driver used by every Eshu
// runtime, so it speaks to NornicDB and Neo4j identically.
type boltGraphCounter struct {
	driver neo4j.DriverWithContext
	db     string
}

// openGraphCounter opens and verifies a Bolt driver from the environment and
// returns a counter plus a close func. Reuses runtime.OpenNeo4jDriver so the
// gate honours the same env vars (ESHU_GRAPH_BACKEND, NEO4J_URI, ...) as the
// services under test.
func openGraphCounter(ctx context.Context, getenv func(string) string) (*boltGraphCounter, func(), error) {
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, getenv)
	if err != nil {
		return nil, nil, err
	}
	closeFn := func() { _ = driver.Close(context.Background()) }
	return &boltGraphCounter{driver: driver, db: cfg.DatabaseName}, closeFn, nil
}

func (b *boltGraphCounter) scalarCount(ctx context.Context, cypher string) (int64, error) {
	result, err := neo4j.ExecuteQuery(ctx, b.driver, cypher, nil,
		neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return 0, fmt.Errorf("execute count query: %w", err)
	}
	if len(result.Records) == 0 {
		return 0, nil
	}
	val, _, err := neo4j.GetRecordValue[int64](result.Records[0], "c")
	if err != nil {
		return 0, fmt.Errorf("read count column: %w", err)
	}
	return val, nil
}

func (b *boltGraphCounter) CountNodes(ctx context.Context, label string) (int64, error) {
	if !identRE.MatchString(label) {
		return 0, fmt.Errorf("unsafe node label %q", label)
	}
	// No backtick quoting: NornicDB's Cypher dialect does not match a
	// backtick-quoted label (MATCH (n:`Repository`) returns 0 where
	// (n:Repository) returns the real count), and identRE already guarantees the
	// identifier is safe to interpolate, so plain interpolation is both injection
	// -safe and portable across NornicDB and Neo4j.
	return b.scalarCount(ctx, fmt.Sprintf("MATCH (n:%s) RETURN count(n) AS c", label))
}

func (b *boltGraphCounter) CountEdges(ctx context.Context, relationship string) (int64, error) {
	if !identRE.MatchString(relationship) {
		return 0, fmt.Errorf("unsafe relationship type %q", relationship)
	}
	return b.scalarCount(ctx, fmt.Sprintf("MATCH ()-[r:%s]->() RETURN count(r) AS c", relationship))
}

func (b *boltGraphCounter) CountCorrelation(ctx context.Context, from, rel, to string) (int64, error) {
	for _, id := range []string{from, rel, to} {
		if !identRE.MatchString(id) {
			return 0, fmt.Errorf("unsafe correlation identifier %q", id)
		}
	}
	return b.scalarCount(ctx, fmt.Sprintf(
		"MATCH (:%s)-[r:%s]->(:%s) RETURN count(r) AS c", from, rel, to))
}

func (b *boltGraphCounter) CountCorrelationWithEvidence(ctx context.Context, from, rel, to string, evidenceKinds []string) (int64, error) {
	for _, id := range []string{from, rel, to} {
		if !identRE.MatchString(id) {
			return 0, fmt.Errorf("unsafe correlation identifier %q", id)
		}
	}
	if len(evidenceKinds) == 0 {
		return b.CountCorrelation(ctx, from, rel, to)
	}
	for _, kind := range evidenceKinds {
		if kind == "" {
			return 0, fmt.Errorf("empty evidence kind for correlation (:%s)-[:%s]->(:%s)", from, rel, to)
		}
	}
	// The filter is applied in Go, NOT in Cypher, because NornicDB does not
	// evaluate a WHERE clause on this relationship-count shape: a probe against
	// the pinned binary showed `MATCH (:L)-[r:T]->(:L) WHERE false RETURN count(r)`
	// still returns the full count, and `ANY(x IN r.evidence_kinds WHERE ...)`
	// returns empty. Both `$k IN r.evidence_kinds` and `'lit' IN r.evidence_kinds`
	// therefore match every edge (a false green). Returning each edge's
	// evidence_kinds and counting matches in Go is backend-neutral and correct;
	// the gate corpus has at most a handful of edges per relationship type.
	rows, err := neo4j.ExecuteQuery(ctx, b.driver,
		fmt.Sprintf("MATCH (:%s)-[r:%s]->(:%s) RETURN r.evidence_kinds AS ek", from, rel, to),
		nil, neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return 0, fmt.Errorf("list correlation evidence: %w", err)
	}
	var count int64
	for _, rec := range rows.Records {
		raw, _ := rec.Get("ek")
		if edgeEvidenceContainsAll(raw, evidenceKinds) {
			count++
		}
	}
	return count, nil
}

// edgeEvidenceContainsAll reports whether the edge's evidence_kinds property
// (a Bolt list value, decoded as []any of strings) contains every required
// kind. A nil or non-list property contains nothing. An empty required set
// matches nothing (not everything): callers that want an unfiltered count use
// CountCorrelation, and this conservative contract keeps a future direct caller
// from silently matching every edge.
func edgeEvidenceContainsAll(raw any, required []string) bool {
	if len(required) == 0 {
		return false
	}
	present := make(map[string]struct{})
	switch kinds := raw.(type) {
	case []any:
		for _, k := range kinds {
			if s, ok := k.(string); ok {
				present[s] = struct{}{}
			}
		}
	case []string:
		for _, s := range kinds {
			present[s] = struct{}{}
		}
	default:
		return false
	}
	for _, want := range required {
		if _, ok := present[want]; !ok {
			return false
		}
	}
	return true
}

// checkRequiredNodes asserts each label in requiredLabels has at least one node.
// This is the minimal required graph smoke check: it proves the pipeline
// projected the corpus into the graph, and holds for any non-empty corpus.
func checkRequiredNodes(ctx context.Context, c graphCounter, requiredLabels []string, r *Report) error {
	for _, label := range requiredLabels {
		count, err := c.CountNodes(ctx, label)
		if err != nil {
			return fmt.Errorf("count nodes %s: %w", label, err)
		}
		r.Add(evaluateNodePresent(label, count))
	}
	return nil
}

// checkGraph runs every B-7(b) graph assertion: required correlations and
// node/edge count tolerances. blockingCorrelations names the correlation IDs
// that fail the gate (the rest are advisory). requiredOnly limits the run to the
// correlations, used by the minimal 5-repo gate where the 20-repo count ranges
// do not apply.
func checkGraph(ctx context.Context, c graphCounter, snap Snapshot, requiredOnly bool, blockingCorrelations map[string]bool, r *Report) error {
	for _, rc := range snap.Graph.RequiredCorrelations {
		var (
			count int64
			err   error
		)
		if len(rc.EvidenceKinds) > 0 {
			count, err = c.CountCorrelationWithEvidence(ctx, rc.FromLabel, rc.Relationship, rc.ToLabel, rc.EvidenceKinds)
		} else {
			count, err = c.CountCorrelation(ctx, rc.FromLabel, rc.Relationship, rc.ToLabel)
		}
		if err != nil {
			return fmt.Errorf("count correlation %s: %w", rc.ID, err)
		}
		r.Add(evaluateRequiredCorrelation(rc, count, blockingCorrelations[rc.ID]))
	}
	if requiredOnly {
		return nil
	}

	for _, label := range sortedKeys(snap.Graph.NodeCounts) {
		count, err := c.CountNodes(ctx, label)
		if err != nil {
			return fmt.Errorf("count nodes %s: %w", label, err)
		}
		r.Add(evaluateNodeCount(label, snap.Graph.NodeCounts[label], count))
	}
	for _, rel := range sortedKeys(snap.Graph.EdgeCounts) {
		count, err := c.CountEdges(ctx, rel)
		if err != nil {
			return fmt.Errorf("count edges %s: %w", rel, err)
		}
		r.Add(evaluateEdgeCount(rel, snap.Graph.EdgeCounts[rel], count))
	}
	return nil
}

func sortedKeys(m map[string]CountRange) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
