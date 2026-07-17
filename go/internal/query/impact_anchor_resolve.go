// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
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

// impactAnchorResolveCypher builds the CALL{UNION} that resolves a node to its
// canonical label and id from a caller-supplied identifier, matching on either
// the node `id` or the node `name` per label. The wrapping CALL{} with a plain
// outer RETURN is the NornicDB-safe shape for a per-label union (a bare top-level
// UNION mis-executes on the pinned build); a per-label inline-property anchor is
// the NornicDB-safe lookup (a label disjunction matches zero rows). Matching
// name as well as id is required because callers (and the MCP tools) pass human
// identifiers such as a repository name, not the hashed canonical id. Only the
// branch whose label and property the node actually carries returns a row.
func impactAnchorResolveCypher(idParam string) string {
	branches := make([]string, 0, len(impactAnchorLabels)*2)
	for _, label := range impactAnchorLabels {
		for _, prop := range []string{"id", "name"} {
			branches = append(branches, fmt.Sprintf(
				"MATCH (n:%s {%s: $%s}) RETURN '%s' AS label, n.id AS id, n.name AS name, labels(n) AS labels",
				label, prop, idParam, label))
		}
	}
	return "CALL {\n" + strings.Join(branches, "\nUNION\n") + "\n}\nRETURN label, id, name, labels\nLIMIT 1"
}

// impactRepoPathCypher is the trace-resource-to-code traversal from a resolved
// start node to Repository nodes. %s is the resolved single-label inline-property
// start pattern (anchored on the resolved canonical id, which is indexed) and %d
// is the max traversal depth. It projects the raw relationships(path) list,
// unwound into per-hop provenance in Go.
const impactRepoPathCypher = `MATCH path = %s-[*1..%d]->(repo:Repository)
RETURN repo.id AS repo_id, repo.name AS repo_name, length(path) AS depth, relationships(path) AS rels
ORDER BY depth, repo_name, repo_id
LIMIT $limit`

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
