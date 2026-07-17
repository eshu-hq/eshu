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
