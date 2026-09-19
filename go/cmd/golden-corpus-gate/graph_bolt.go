// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

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
		"MATCH (:%s)-[r:%s]->(:%s) RETURN count(r) AS c", from, rel, to,
	))
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

// ListCorrelationEdgeProperty implements graphCounter: it returns prop for every
// (from)-[rel]->(to) edge, narrowed by evidenceKinds in Go (see the interface
// doc). prop is interpolated, so it is validated against identRE alongside the
// labels and relationship type.
func (b *boltGraphCounter) ListCorrelationEdgeProperty(ctx context.Context, from, rel, to string, evidenceKinds []string, prop string) ([]string, error) {
	for _, id := range []string{from, rel, to, prop} {
		if !identRE.MatchString(id) {
			return nil, fmt.Errorf("unsafe edge-property identifier %q", id)
		}
	}
	// Reject an empty narrowing kind for parity with CountCorrelationWithEvidence:
	// an empty kind would silently narrow to nothing and make the edge-property
	// finding pass vacuously, masking a real misconfiguration.
	for _, kind := range evidenceKinds {
		if kind == "" {
			return nil, fmt.Errorf("empty evidence kind for edge property (:%s)-[:%s]->(:%s)", from, rel, to)
		}
	}
	rows, err := neo4j.ExecuteQuery(ctx, b.driver,
		fmt.Sprintf("MATCH (:%s)-[r:%s]->(:%s) RETURN r.evidence_kinds AS ek, r.%s AS pv", from, rel, to, prop),
		nil, neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return nil, fmt.Errorf("list correlation edge property: %w", err)
	}
	out := make([]string, 0, len(rows.Records))
	for _, rec := range rows.Records {
		if len(evidenceKinds) > 0 {
			raw, _ := rec.Get("ek")
			if !edgeEvidenceContainsAll(raw, evidenceKinds) {
				continue
			}
		}
		pv, _ := rec.Get("pv")
		out = append(out, boltPropertyString(pv))
	}
	return out, nil
}

// ListNodeProperty implements graphCounter: it returns prop for every node
// carrying label. prop and label are interpolated, so both are validated.
func (b *boltGraphCounter) ListNodeProperty(ctx context.Context, label, prop string) ([]string, error) {
	for _, id := range []string{label, prop} {
		if !identRE.MatchString(id) {
			return nil, fmt.Errorf("unsafe node-property identifier %q", id)
		}
	}
	rows, err := neo4j.ExecuteQuery(ctx, b.driver,
		fmt.Sprintf("MATCH (n:%s) RETURN n.%s AS pv", label, prop),
		nil, neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return nil, fmt.Errorf("list node property: %w", err)
	}
	out := make([]string, 0, len(rows.Records))
	for _, rec := range rows.Records {
		pv, _ := rec.Get("pv")
		out = append(out, boltPropertyString(pv))
	}
	return out, nil
}

// CountSelfLoopEdges implements graphCounter: it counts (n:label {property:
// value})-[r:relationship]->(n) self-loop edges — same source and target node.
// property/value are node property scoping (e.g. language="dart") so a shared
// label like Function does not conflate one language's self-loop count with
// another's; value is passed as a query parameter (not interpolated), only
// label/relationship/property are validated against identRE and interpolated,
// matching the other Count* methods' safety posture.
//
// The self-loop identity test is performed in Go, not in Cypher, because
// NornicDB does not enforce cross-variable node identity in this pattern shape.
// The intuitive shape MATCH (n)-[r]->(n) (reusing one bound variable for both
// endpoints) does NOT bind the target back to the same node on NornicDB: it
// silently degenerates to MATCH (n)-[r]->() and returns every OUTGOING edge, not
// only the self-loops (verified live against the pinned NornicDB image — the
// golden corpus' 12 outgoing dart Function CALLS edges were all returned when
// only 2 are genuine self-loops). Every in-query identity predicate that could
// re-impose it — WHERE elementId(n)=elementId(m), WHERE n.uid=m.uid, WHERE
// startNode(r)=endNode(r), even a plain WHERE n.name=m.name — degenerates to
// zero matches on NornicDB instead. So the gate does the identity comparison
// itself: it returns both endpoints' elementId and counts the edges whose ends
// are the same node. This mirrors ListCorrelationEdgeProperty, which already
// filters in Go for the same class of NornicDB WHERE-clause limitation; the
// gate corpus has at most a handful of edges per relationship type, so returning
// both endpoint ids is cheap and bounded. The shape is also correct on Neo4j
// (where the elementIds simply always differ for non-self edges), keeping the
// gate backend-neutral.
func (b *boltGraphCounter) CountSelfLoopEdges(ctx context.Context, label, relationship, property, value string) (int64, error) {
	for _, id := range []string{label, relationship, property} {
		if !identRE.MatchString(id) {
			return 0, fmt.Errorf("unsafe self-loop identifier %q", id)
		}
	}
	result, err := neo4j.ExecuteQuery(ctx, b.driver,
		fmt.Sprintf("MATCH (n:%s {%s: $value})-[r:%s]->(m) RETURN elementId(n) AS a, elementId(m) AS b", label, property, relationship),
		map[string]any{"value": value},
		neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return 0, fmt.Errorf("execute self-loop count query: %w", err)
	}
	var count int64
	for _, rec := range result.Records {
		a, _ := rec.Get("a")
		bEnd, _ := rec.Get("b")
		as, aok := a.(string)
		bs, bok := bEnd.(string)
		if aok && bok && as != "" && as == bs {
			count++
		}
	}
	return count, nil
}

// boltPropertyString coerces a Bolt-decoded property value to a string for the
// gate's presence/value checks. A null, absent, or non-string-and-non-list
// value yields "" (treated as absent), which is conservative: a property the
// gate expects to be a canonical string token but finds stored as another
// type counts as a violation rather than silently passing.
//
// A Bolt LIST property (for example PackageArtifact.hashes, a sorted
// "algorithm:digest" string list -- Cypher node properties cannot hold a
// nested map, see packageRegistryHashPairs in
// go/internal/storage/cypher/package_registry_artifact_writer.go) decodes off
// the wire as []any of strings, mirroring edgeEvidenceContainsAll's handling
// of the evidence_kinds property below. Joining its elements with "|" lets
// ListNodeProperty/ListCorrelationEdgeProperty apply the same
// non-empty/allowed-value check a scalar property gets (#5820 P2 review
// finding: without this, a RequiredNode assertion naming a list property was
// unconditionally red on a live run regardless of writer correctness, because
// every element decoded to ""). A list containing a non-string element (or
// any other unrecognized Bolt shape) still yields "", the same conservative
// "treat as absent" behavior as a bare non-string scalar.
func boltPropertyString(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case []any:
		return boltStringListJoined(value)
	case []string:
		return strings.Join(value, "|")
	default:
		return ""
	}
}

// boltStringListJoined joins a Bolt list property's decoded []any elements
// with "|" once every element is confirmed to be a string, or "" if any
// element is not (or the list is empty) -- see boltPropertyString.
func boltStringListJoined(values []any) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			return ""
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "|")
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
