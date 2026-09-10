// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package query implements the Go read-path query layer for the platform
// context graph API. It provides Neo4j graph reads, Postgres content store
// reads, and HTTP handlers that replace the former Python query package.
package query

import (
	"context"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Neo4jReader executes read-only Cypher queries against a Neo4j database.
type Neo4jReader struct {
	driver         neo4jdriver.DriverWithContext
	database       string
	tracer         trace.Tracer
	policy         neo4jReadPolicy
	sessionFactory neo4jReadSessionFactory
}

// NewNeo4jReader constructs a read-only Neo4j query executor.
func NewNeo4jReader(driver neo4jdriver.DriverWithContext, database string, options ...Neo4jReaderOption) *Neo4jReader {
	reader := &Neo4jReader{
		driver:   driver,
		database: database,
		tracer:   otel.Tracer("eshu/go/internal/query"),
		policy:   defaultNeo4jReadPolicy(),
	}
	for _, option := range options {
		option(reader)
	}
	return reader
}

// GraphConfigured reports whether r has a live driver or session factory
// wired, as opposed to merely being a non-nil *Neo4jReader. NewNeo4jReader is
// called unconditionally by cmd/api/wiring.go and cmd/mcp-server/wiring.go,
// even when openQueryGraph returns a nil driver for local_lightweight or
// ESHU_DISABLE_NEO4J=true (cmd/api/wiring_graph.go), so a driverless
// *Neo4jReader is a normal production shape, not a test-only edge case. A
// caller comparing a GraphQuery field to nil cannot detect this; it must ask
// GraphConfigured instead (#5761 F1). A nil receiver reports false rather
// than panicking, so callers can call it through a possibly-nil GraphQuery
// field without an extra nil check first.
func (r *Neo4jReader) GraphConfigured() bool {
	return r != nil && (r.driver != nil || r.sessionFactory != nil)
}

// Run executes a read-only Cypher query and returns results as maps.
func (r *Neo4jReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return r.runRead(ctx, cypher, params)
}

// RunSingle executes a Cypher query expecting at most one result row.
func (r *Neo4jReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	ctx, span := r.tracer.Start(ctx, "neo4j.query.single")
	defer span.End()

	rows, err := r.Run(ctx, cypher, params)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// RelationshipTypes returns the set of relationship type names in the graph.
func (r *Neo4jReader) RelationshipTypes(ctx context.Context) (map[string]struct{}, error) {
	rows, err := r.Run(ctx, "CALL db.relationshipTypes()", nil)
	if err != nil {
		return nil, err
	}
	types := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		for _, v := range row {
			if s, ok := v.(string); ok && s != "" {
				types[s] = struct{}{}
			}
		}
	}
	return types, nil
}

// StringVal, BoolVal, IntVal and StringSliceVal forward to querycontract.
// The implementations moved there for #6060 so a handler-family subpackage can
// decode graph rows without importing this package, which it cannot do without
// creating an import cycle through root's compatibility aliases. These wrappers
// keep the original names for this package's callers and for the files outside
// it that name them.

// StringVal safely extracts a string from a map value.
func StringVal(row map[string]any, key string) string {
	return querycontract.StringVal(row, key)
}

// BoolVal safely extracts a bool from a map value.
func BoolVal(row map[string]any, key string) bool {
	return querycontract.BoolVal(row, key)
}

// IntVal safely extracts an int from a map value.
func IntVal(row map[string]any, key string) int {
	return querycontract.IntVal(row, key)
}

// StringSliceVal safely extracts a []string from a map value.
func StringSliceVal(row map[string]any, key string) []string {
	return querycontract.StringSliceVal(row, key)
}

// RepoRef is the canonical repository reference returned by query endpoints.
// It is an alias onto querycontract so the moved repository handler family
// can name it from outside this package (#6060, lane B B3).
type RepoRef = querycontract.RepoRef

// RepoRefFromRow converts a graph result row to a RepoRef. The implementation
// moved to querycontract for #6060; this wrapper keeps root callers unchanged.
func RepoRefFromRow(row map[string]any) querycontract.RepoRef {
	return querycontract.RepoRefFromRow(row)
}

// impactRelProvenanceList decodes a relationships(path) value into per-edge
// provenance. relationships(path) is serialized as neo4j.Relationship by the
// Neo4j Go driver but as a map[string]any (with a nested properties map) by
// NornicDB; both shapes are decoded. A `[rel IN relationships(path) | {…}]`
// map-valued comprehension corrupts on the pinned NornicDB build, so the raw
// list is unwound here instead. This decoder lives in neo4j.go because it is the
// only driver-aware seam in the query package (per the package AGENTS.md); it
// returns the impacttrace plain struct so the impact handler family (#6060
// lane B2) can shape hops without importing the driver, which the depguard
// query-no-graph-driver rule forbids outside the driver-owning files.
func impactRelProvenanceList(raw any) []impacttrace.ImpactRelProvenance {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]impacttrace.ImpactRelProvenance, 0, len(items))
	for _, item := range items {
		switch rel := item.(type) {
		case neo4jdriver.Relationship:
			out = append(out, impactRelProvenanceFromProps(rel.Type, rel.Props))
		case map[string]any:
			props, _ := rel["properties"].(map[string]any)
			out = append(out, impactRelProvenanceFromProps(StringVal(rel, "type"), props))
		}
	}
	return out
}

// impactRelProvenanceFromProps builds provenance from a relationship type and its
// property map, tolerating a nil property map.
func impactRelProvenanceFromProps(relType string, props map[string]any) impacttrace.ImpactRelProvenance {
	p := impacttrace.ImpactRelProvenance{RelType: relType}
	if conf, ok := props["confidence"].(float64); ok {
		p.Confidence = conf
		p.HasConf = true
	}
	if reason, ok := props["reason"].(string); ok {
		p.Reason = reason
	}
	return p
}

// impactNodeIdentityList decodes a nodes(path) value into per-node identities.
// nodes(path) is serialized as neo4j.Node by both backends (unlike
// relationships(path)); a map[string]any fallback is kept for safety. Same
// boundary rationale as impactRelProvenanceList: the driver switch stays here,
// the plain struct crosses into the impact family. See #6060.
func impactNodeIdentityList(raw any) []impacttrace.ImpactNodeIdentity {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]impacttrace.ImpactNodeIdentity, 0, len(items))
	for _, item := range items {
		switch node := item.(type) {
		case neo4jdriver.Node:
			out = append(out, impactNodeIdentityFromProps(node.Props))
		case map[string]any:
			if props, ok := node["properties"].(map[string]any); ok {
				out = append(out, impactNodeIdentityFromProps(props))
			} else {
				out = append(out, impactNodeIdentityFromProps(node))
			}
		}
	}
	return out
}

// impactNodeIdentityFromProps reads id/name from a node property map.
func impactNodeIdentityFromProps(props map[string]any) impacttrace.ImpactNodeIdentity {
	return impacttrace.ImpactNodeIdentity{ID: StringVal(props, "id"), Name: StringVal(props, "name")}
}

// resourceInvestigationHopList decodes a relationships(path) value into the
// resource-investigation per-hop maps {type, confidence, reason}. Each hop's
// reason falls back to the edge's evidence_type, matching the prior
// `coalesce(rel.reason, rel.evidence_type, ”)` projection. relationships(path)
// is serialized as neo4j.Relationship by the Neo4j driver and as a
// map[string]any (with a nested properties map) by NornicDB; both are decoded.
// The prior `[rel IN relationships(path) | {type, confidence, reason}]`
// map-valued comprehension corrupts on the pinned NornicDB build (#5287), so the
// raw list is unwound here instead. This decoder lives in neo4j.go because it is
// the only driver-aware seam in the query package (per the package AGENTS.md).
func resourceInvestigationHopList(raw any) []map[string]any {
	items, ok := raw.([]any)
	if !ok {
		return []map[string]any{}
	}
	hops := make([]map[string]any, 0, len(items))
	for _, item := range items {
		var relType string
		var props map[string]any
		switch rel := item.(type) {
		case neo4jdriver.Relationship:
			relType = rel.Type
			props = rel.Props
		case map[string]any:
			relType = StringVal(rel, "type")
			props, _ = rel["properties"].(map[string]any)
		default:
			// Unexpected relationships(path) element shape. Both pinned
			// backends serialize edges as neo4j.Relationship (Neo4j) or
			// map[string]any (NornicDB); a different type would indicate a
			// backend/driver upgrade that changed the serialization. Dropping
			// it keeps the read resilient rather than panicking; the
			// backend-required live test
			// (TestLiveResourceInvestigationReadsAreNornicDBSafe) asserts the
			// current shapes decode, so such a drift fails that gate before it
			// can surface as silently empty hops in production.
			continue
		}
		hops = append(hops, map[string]any{
			"type":       relType,
			"confidence": props["confidence"],
			"reason":     resourceInvestigationHopReason(props),
		})
	}
	return hops
}

// resourceInvestigationHopReason renders a hop reason as
// coalesce(reason, evidence_type, ”), tolerating a nil property map.
func resourceInvestigationHopReason(props map[string]any) string {
	if reason := StringVal(props, "reason"); reason != "" {
		return reason
	}
	return StringVal(props, "evidence_type")
}

// RepoProjection returns the standard Cypher RETURN clause for repository nodes.
// The implementation moved to querycontract for #6060; this wrapper keeps
// root callers unchanged.
func RepoProjection(alias string) string {
	return querycontract.RepoProjection(alias)
}
