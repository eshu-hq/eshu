// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// impactAnchorLabels is the ordered label set the by-id impact reads anchor on,
// derived from impactAnchorLabelDisjunction. A `MATCH (n:A|B|C) WHERE n.id = $id`
// disjunction anchor matches zero rows on the pinned NornicDB build, so callers
// resolve a by-id node with a per-label inline-property anchor instead (#5286).
var impactAnchorLabels = strings.Split(impactAnchorLabelDisjunction, "|")

// resolvedImpactAnchor is a by-id node resolved to its canonical label.
type resolvedImpactAnchor struct {
	id     string
	name   string
	label  string
	labels []string
}

// pattern returns the single-label inline-property start pattern for the
// resolved node, e.g. `(start:CloudResource {id: $id})`. The variable name and
// the id parameter name are caller-supplied so the pattern can fold into a
// single-clause traversal.
func (a resolvedImpactAnchor) pattern(variable, idParam string) string {
	return fmt.Sprintf("(%s:%s {id: $%s})", variable, a.label, idParam)
}

// impactAnchorResolveCypher builds the CALL{UNION} that resolves a by-id node to
// its label with one per-label inline-property anchor per UNION branch. The
// wrapping CALL{} with a plain outer RETURN is the NornicDB-safe shape for a
// per-label union (a bare top-level UNION mis-executes on the pinned build); a
// per-label inline-property anchor is the NornicDB-safe by-id lookup (a label
// disjunction matches zero rows). Only the branch whose label the node actually
// carries returns a row.
func impactAnchorResolveCypher(idParam string) string {
	branches := make([]string, 0, len(impactAnchorLabels))
	for _, label := range impactAnchorLabels {
		branches = append(branches, fmt.Sprintf(
			"MATCH (n:%s {id: $%s}) RETURN '%s' AS label, n.id AS id, n.name AS name, labels(n) AS labels",
			label, idParam, label))
	}
	return "CALL {\n" + strings.Join(branches, "\nUNION\n") + "\n}\nRETURN label, id, name, labels\nLIMIT 1"
}

// impactRepoTraversalCypher builds the trace-resource-to-code traversal as a
// CALL{UNION} of per-label inline-property anchors, each traversing to a
// Repository. Folding the label resolution into the traversal keeps it a single
// round-trip (only the branch whose label the start node carries returns rows),
// and the CALL{} wrapper with a plain outer RETURN is the NornicDB-safe shape for
// a per-label union. %d is the max traversal depth. The start label/name are
// projected so the handler can hydrate the start node from the same query.
func impactRepoTraversalCypher(depth int) string {
	branches := make([]string, 0, len(impactAnchorLabels))
	for _, label := range impactAnchorLabels {
		branches = append(branches, fmt.Sprintf(
			"MATCH path = (start:%s {id: $start_id})-[*1..%d]->(repo:Repository) "+
				"RETURN repo.id AS repo_id, repo.name AS repo_name, length(path) AS depth, "+
				"relationships(path) AS rels, labels(start) AS start_labels, start.name AS start_name",
			label, depth))
	}
	return "CALL {\n" + strings.Join(branches, "\nUNION\n") + "\n}\n" +
		"RETURN repo_id, repo_name, depth, rels, start_labels, start_name\n" +
		"ORDER BY depth, repo_name, repo_id\nLIMIT $limit"
}

// impactDualAnchorResolveCypher resolves both the source and the target node to
// their labels in a single round-trip, as a CALL{UNION} of per-label
// inline-property anchors for each role. Each matching branch returns one row
// tagged with its role; the pinned NornicDB build matches zero rows for a label
// disjunction, so per-label anchors are required.
func impactDualAnchorResolveCypher() string {
	branches := make([]string, 0, len(impactAnchorLabels)*2)
	for _, role := range []struct{ name, param string }{{"source", "source_id"}, {"target", "target_id"}} {
		for _, label := range impactAnchorLabels {
			branches = append(branches, fmt.Sprintf(
				"MATCH (n:%s {id: $%s}) RETURN '%s' AS role, '%s' AS label, n.id AS id, n.name AS name, labels(n) AS labels",
				label, role.param, role.name, label))
		}
	}
	return "CALL {\n" + strings.Join(branches, "\nUNION\n") + "\n}\nRETURN role, label, id, name, labels"
}

// resolveImpactAnchorNode resolves a by-id node to its canonical label so a
// caller can anchor a single-label inline-property traversal. It returns nil when
// no anchor-label node carries the id.
func resolveImpactAnchorNode(ctx context.Context, reader GraphQuery, idParam, id string) (*resolvedImpactAnchor, error) {
	row, err := reader.RunSingle(ctx, impactAnchorResolveCypher(idParam), map[string]any{idParam: id})
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	label := StringVal(row, "label")
	if label == "" {
		return nil, nil
	}
	return &resolvedImpactAnchor{
		id:     StringVal(row, "id"),
		name:   StringVal(row, "name"),
		label:  label,
		labels: StringSliceVal(row, "labels"),
	}, nil
}

// resolveImpactDualAnchors resolves the source and target by-id nodes to their
// labels in a single round-trip. Either return value is nil when no anchor-label
// node carries the corresponding id.
func resolveImpactDualAnchors(ctx context.Context, reader GraphQuery, sourceID, targetID string) (source, target *resolvedImpactAnchor, err error) {
	rows, err := reader.Run(ctx, impactDualAnchorResolveCypher(), map[string]any{"source_id": sourceID, "target_id": targetID})
	if err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		anchor := &resolvedImpactAnchor{
			id:     StringVal(row, "id"),
			name:   StringVal(row, "name"),
			label:  StringVal(row, "label"),
			labels: StringSliceVal(row, "labels"),
		}
		if anchor.label == "" {
			continue
		}
		switch StringVal(row, "role") {
		case "source":
			source = anchor
		case "target":
			target = anchor
		}
	}
	return source, target, nil
}

// impactRelProvenance is one relationship's provenance decoded from a
// relationships(path) element.
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
// list is unwound here instead (#5286).
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

// impactTraceHops builds the trace-resource-to-code hop provenance ({type,
// confidence, reason}) from a relationships(path) value.
func impactTraceHops(relsRaw any) []map[string]any {
	rels := impactRelProvenanceList(relsRaw)
	hops := make([]map[string]any, 0, len(rels))
	for _, rel := range rels {
		hop := map[string]any{"type": rel.relType}
		if rel.hasConf {
			hop["confidence"] = rel.confidence
		}
		if rel.reason != "" {
			hop["reason"] = rel.reason
		}
		hops = append(hops, hop)
	}
	return hops
}

// impactDependencyHops builds the explain-dependency-path hop provenance
// ({from_id, from_name, to_id, to_name, type, confidence, reason}) by zipping
// nodes(path) with relationships(path). The from/to endpoints follow the path
// traversal order (source toward target); relationships(path)[i] connects
// nodes(path)[i] and nodes(path)[i+1].
func impactDependencyHops(nodesRaw, relsRaw any) []map[string]any {
	nodes := impactNodeIdentityList(nodesRaw)
	rels := impactRelProvenanceList(relsRaw)
	hops := make([]map[string]any, 0, len(rels))
	for i, rel := range rels {
		hop := map[string]any{"type": rel.relType}
		if i < len(nodes) {
			hop["from_id"] = nodes[i].id
			hop["from_name"] = nodes[i].name
		}
		if i+1 < len(nodes) {
			hop["to_id"] = nodes[i+1].id
			hop["to_name"] = nodes[i+1].name
		}
		if rel.hasConf {
			hop["confidence"] = rel.confidence
		}
		if rel.reason != "" {
			hop["reason"] = rel.reason
		}
		hops = append(hops, hop)
	}
	return hops
}
