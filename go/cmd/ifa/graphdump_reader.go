// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// boltNodesCypher and boltEdgesCypher are the two read-only queries a
// prove-the-theory shim already confirmed run against NornicDB via the Bolt
// driver (see go/internal/ifa/graphdump/reader.go's Reader doc): a bare
// `MATCH (n) RETURN labels(n), properties(n)` for nodes, and the equivalent
// one-hop pattern for edges. Every node/edge in the graph is returned in one
// unbounded scan; graphdump.Canonicalize's own doc explains why this is safe
// for the corpus sizes this verb targets (a demo-org/gate-scale graph, not an
// unbounded production one) and why iteration order does not matter.
const (
	boltNodesCypher = "MATCH (n) RETURN labels(n) AS labels, properties(n) AS props"
	boltEdgesCypher = "MATCH (a)-[r]->(b) RETURN labels(a) AS fl, properties(a) AS fp, " +
		"type(r) AS rel, properties(r) AS rp, labels(b) AS tl, properties(b) AS tp"
)

// boltGraphReader implements graphdump.Reader over a live Bolt session,
// reusing the same shared driver plumbing (runtime.OpenNeo4jDriver) and
// neo4j.ExecuteQuery call shape as cmd/golden-corpus-gate's boltGraphCounter,
// for consistency across Eshu's Bolt-facing CLI tools. It belongs here in
// cmd/ifa rather than in internal/ifa/graphdump itself: the graphdump package
// is deliberately driver-free so its canonicalization logic stays
// hermetically testable against an in-memory fakeReader with no
// NornicDB/Neo4j/Docker dependency (see graphdump/reader.go's Reader doc,
// which names this verb as the intended live-backend implementation site).
type boltGraphReader struct {
	driver neo4j.DriverWithContext
	db     string
}

// openBoltGraphReader opens and verifies a Bolt driver from the environment
// (ESHU_GRAPH_BACKEND, NEO4J_URI/ESHU_NEO4J_URI, NEO4J_USERNAME/
// ESHU_NEO4J_USERNAME, NEO4J_PASSWORD/ESHU_NEO4J_PASSWORD, NEO4J_DATABASE/
// ESHU_NEO4J_DATABASE) via runtime.OpenNeo4jDriver, the same env contract
// every other Bolt-backed Eshu binary honours, and returns a graphdump.Reader
// plus a close func. A missing or invalid backend config fails here, before
// any dial is attempted, so a caller without a live graph backend gets a
// clean error rather than a hang or a late connection-refused.
func openBoltGraphReader(ctx context.Context, getenv func(string) string) (*boltGraphReader, func(), error) {
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, getenv)
	if err != nil {
		return nil, nil, err
	}
	closeFn := func() { _ = driver.Close(context.Background()) }
	return &boltGraphReader{driver: driver, db: cfg.DatabaseName}, closeFn, nil
}

// StreamNodes implements graphdump.Reader: it runs boltNodesCypher on a
// read-only session and yields every node straight off the Bolt result cursor
// (result.Next), never collecting the whole result set into a slice. Combined
// with graphdump.Canonicalize's own streaming, this keeps peak memory at the
// canonical record set rather than the full node struct set (issue #5009).
func (b *boltGraphReader) StreamNodes(ctx context.Context, yield func(graphdump.Node) error) error {
	// AccessModeWrite, not Read: on a Neo4j-compatible cluster AccessMode is a
	// ROUTING control, and a read-only graph dump for a determinism digest must
	// read the authoritative writer, never a possibly-replication-lagged reader
	// member. This matches the routing of the neo4j.ExecuteQuery (default
	// RoutingControl=Write) call this replaced, and cmd/golden-corpus-gate's
	// boltGraphCounter. Against single-instance NornicDB it is a no-op.
	session := b.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: b.db,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, boltNodesCypher, nil)
	if err != nil {
		return fmt.Errorf("execute node dump query: %w", err)
	}
	for result.Next(ctx) {
		rec := result.Record()
		labelsRaw, _ := rec.Get("labels")
		propsRaw, _ := rec.Get("props")
		if err := yield(graphdump.Node{
			Labels: boltStringSlice(labelsRaw),
			Props:  boltPropsMap(propsRaw),
		}); err != nil {
			return err
		}
	}
	if err := result.Err(); err != nil {
		return fmt.Errorf("stream node dump rows: %w", err)
	}
	return nil
}

// StreamEdges implements graphdump.Reader: it runs boltEdgesCypher on a
// read-only session and yields every edge straight off the Bolt result cursor
// (result.Next), never collecting the whole result set. Each yielded edge
// carries both endpoints' labels/props snapshot (see Edge's doc for why the
// endpoint is repeated rather than referenced by index or backend ID). Same
// streaming, no-materialization contract as StreamNodes (issue #5009).
func (b *boltGraphReader) StreamEdges(ctx context.Context, yield func(graphdump.Edge) error) error {
	// AccessModeWrite, not Read: on a Neo4j-compatible cluster AccessMode is a
	// ROUTING control, and a read-only graph dump for a determinism digest must
	// read the authoritative writer, never a possibly-replication-lagged reader
	// member. This matches the routing of the neo4j.ExecuteQuery (default
	// RoutingControl=Write) call this replaced, and cmd/golden-corpus-gate's
	// boltGraphCounter. Against single-instance NornicDB it is a no-op.
	session := b.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: b.db,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, boltEdgesCypher, nil)
	if err != nil {
		return fmt.Errorf("execute edge dump query: %w", err)
	}
	for result.Next(ctx) {
		rec := result.Record()
		fromLabelsRaw, _ := rec.Get("fl")
		fromPropsRaw, _ := rec.Get("fp")
		relTypeRaw, _ := rec.Get("rel")
		relPropsRaw, _ := rec.Get("rp")
		toLabelsRaw, _ := rec.Get("tl")
		toPropsRaw, _ := rec.Get("tp")
		relType, _ := relTypeRaw.(string)
		if err := yield(graphdump.Edge{
			Type:       relType,
			FromLabels: boltStringSlice(fromLabelsRaw),
			FromProps:  boltPropsMap(fromPropsRaw),
			ToLabels:   boltStringSlice(toLabelsRaw),
			ToProps:    boltPropsMap(toPropsRaw),
			Props:      boltPropsMap(relPropsRaw),
		}); err != nil {
			return err
		}
	}
	if err := result.Err(); err != nil {
		return fmt.Errorf("stream edge dump rows: %w", err)
	}
	return nil
}

// boltStringSlice coerces a Bolt-decoded Cypher list value into []string. The
// Go driver decodes a Cypher list (what labels() always returns) as []any,
// with each label element itself a string; a non-string element is dropped
// rather than causing the whole row to fail, matching
// internal/query.StringSliceVal's same defensive coercion for the same
// driver value shape. A nil or unexpected-shape value returns nil (an empty
// label set), never an error: graphdump's own sortedLabels already treats
// nil and empty identically.
func boltStringSlice(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// boltPropsMap coerces a Bolt-decoded properties() value into
// map[string]any. The Go driver already decodes a Cypher map (what
// properties() always returns) as map[string]any, so this is a plain type
// assertion; a nil or unexpected-shape value returns nil (no properties),
// which graphdump.normalizeProps treats identically to an empty map.
func boltPropsMap(raw any) map[string]any {
	m, _ := raw.(map[string]any)
	return m
}

// Ensure boltGraphReader satisfies graphdump.Reader at compile time.
var _ graphdump.Reader = (*boltGraphReader)(nil)
