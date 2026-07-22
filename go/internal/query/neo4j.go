// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package query implements the Go read-path query layer for the platform
// context graph API. It provides Neo4j graph reads, Postgres content store
// reads, and HTTP handlers that replace the former Python query package.
package query

import (
	"context"
	"fmt"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
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

// StringVal safely extracts a string from a map value.
func StringVal(row map[string]any, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

// BoolVal safely extracts a bool from a map value.
func BoolVal(row map[string]any, key string) bool {
	v, ok := row[key]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}

// IntVal safely extracts an int from a map value.
func IntVal(row map[string]any, key string) int {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

// StringSliceVal safely extracts a []string from a map value.
func StringSliceVal(row map[string]any, key string) []string {
	v, ok := row[key]
	if !ok || v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		result := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

// RepoRef is the canonical repository reference returned by query endpoints.
type RepoRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	LocalPath string `json:"local_path"`
	RemoteURL string `json:"remote_url,omitempty"`
	RepoSlug  string `json:"repo_slug,omitempty"`
	HasRemote bool   `json:"has_remote"`
}

// RepoRefFromRow converts a Neo4j result row to a RepoRef.
func RepoRefFromRow(row map[string]any) RepoRef {
	localPath := StringVal(row, "local_path")
	if localPath == "" {
		localPath = StringVal(row, "path")
	}
	name := StringVal(row, "name")
	if name == "" && localPath != "" {
		parts := strings.Split(localPath, "/")
		name = parts[len(parts)-1]
	}
	return RepoRef{
		ID:        StringVal(row, "id"),
		Name:      name,
		LocalPath: localPath,
		RemoteURL: StringVal(row, "remote_url"),
		RepoSlug:  StringVal(row, "repo_slug"),
		HasRemote: BoolVal(row, "has_remote"),
	}
}

// impactRelProvenance is one relationship's provenance decoded from a
// relationships(path) element (used by the by-id impact reads, #5286).
type impactRelProvenance struct {
	relType    string
	confidence float64
	hasConf    bool
	reason     string
}

// impactRelProvenanceList decodes a relationships(path) value into per-edge
// provenance. relationships(path) is serialized as neo4j.Relationship by the
// Neo4j Go driver but as a map[string]any (with a nested properties map) by
// NornicDB; both shapes are decoded. A `[rel IN relationships(path) | {…}]`
// map-valued comprehension corrupts on the pinned NornicDB build, so the raw
// list is unwound here instead. This decoder lives in neo4j.go because it is the
// only driver-aware seam in the query package (per the package AGENTS.md).
func impactRelProvenanceList(raw any) []impactRelProvenance {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]impactRelProvenance, 0, len(items))
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
func impactRelProvenanceFromProps(relType string, props map[string]any) impactRelProvenance {
	p := impactRelProvenance{relType: relType}
	if conf, ok := props["confidence"].(float64); ok {
		p.confidence = conf
		p.hasConf = true
	}
	if reason, ok := props["reason"].(string); ok {
		p.reason = reason
	}
	return p
}

// impactNodeIdentity is the id/name of a nodes(path) element.
type impactNodeIdentity struct {
	id   string
	name string
}

// impactNodeIdentityList decodes a nodes(path) value into per-node identities.
// nodes(path) is serialized as neo4j.Node by both backends (unlike
// relationships(path)); a map[string]any fallback is kept for safety.
func impactNodeIdentityList(raw any) []impactNodeIdentity {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]impactNodeIdentity, 0, len(items))
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
func impactNodeIdentityFromProps(props map[string]any) impactNodeIdentity {
	return impactNodeIdentity{id: StringVal(props, "id"), name: StringVal(props, "name")}
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

// routeToCallerEntityFromChain extracts the far-endpoint (caller/callee) entity
// fields from a nodes(path) value, reading the LAST node in the chain. The
// route-to-caller relationship reads anchor the known handler as the path start
// and project raw nodes(path) because NornicDB corrupts a CALL-subquery computed
// projection over path nodes (#5287); the far endpoint is the discovered
// caller/callee. nodes(path) is a neo4j.Node on both backends, with a
// map[string]any fallback. Returns nil when the chain is empty or its last
// element is not a node. This decoder lives in neo4j.go because it is the only
// driver-aware seam in the query package (per the package AGENTS.md).
func routeToCallerEntityFromChain(chain any) map[string]any {
	props := lastChainNodeProps(chain)
	if props == nil {
		return nil
	}
	entityID := StringVal(props, "id")
	if entityID == "" {
		entityID = StringVal(props, "uid")
	}
	filePath := StringVal(props, "file_path")
	if filePath == "" {
		filePath = StringVal(props, "relative_path")
	}
	return map[string]any{
		"entity_id":  entityID,
		"name":       StringVal(props, "name"),
		"file_path":  filePath,
		"repo_id":    StringVal(props, "repo_id"),
		"language":   StringVal(props, "language"),
		"start_line": IntVal(props, "start_line"),
		"end_line":   IntVal(props, "end_line"),
	}
}

// lastChainNodeProps returns the property map of the last node in a nodes(path)
// value, decoding both the neo4j.Node driver shape and a map[string]any
// fallback.
func lastChainNodeProps(chain any) map[string]any {
	items, ok := chain.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	switch node := items[len(items)-1].(type) {
	case neo4jdriver.Node:
		return node.Props
	case map[string]any:
		if props, ok := node["properties"].(map[string]any); ok {
			return props
		}
		return node
	default:
		return nil
	}
}

// RepoProjection returns the standard Cypher RETURN clause for repository nodes.
func RepoProjection(alias string) string {
	return fmt.Sprintf(
		"%s.id as id, %s.name as name, %s.path as path, "+
			"coalesce(%s.local_path, %s.path) as local_path, "+
			"%s.remote_url as remote_url, "+
			"%s.repo_slug as repo_slug, "+
			"coalesce(%s.has_remote, false) as has_remote",
		alias, alias, alias, alias, alias, alias, alias, alias,
	)
}

// repositoryDependencyMarkerProjection returns a Cypher projection that marks a
// repository as a dependency repo when at least one other repository that the
// caller is authorized to see depends on it, i.e. it is the target of an
// admitted (:Repository)-[:DEPENDS_ON]->(:Repository) edge where the depending
// repository is also within the caller's access grant.
//
// For scoped callers the inner Repository node is filtered by the same tenant
// predicate as the outer MATCH, using the $allowed_repository_ids and
// $allowed_scope_ids params already bound by access.graphParams. This prevents
// a scoped caller from learning dependency-marker truth about repositories
// outside their grant via an in-scope depending node.
//
// For shared/admin/local callers (allScopes) no predicate is added and all
// depending-repository nodes are eligible, which is the unscoped-safe path.
//
// This replaces the earlier coalesce(r.is_dependency, false) read, which probed
// a Repository node property that no writer populates (is_dependency is a
// file/entity parser flag, never set on Repository nodes), so the marker was
// always false.
func repositoryDependencyMarkerProjection(alias string, access repositoryAccessFilter) string {
	const depAlias = "dep"
	predicate := access.graphPredicate(depAlias)
	return fmt.Sprintf(
		"EXISTS { MATCH (%s)<-[:DEPENDS_ON]-(%s:Repository)%s } as is_dependency",
		alias, depAlias, predicate,
	)
}
