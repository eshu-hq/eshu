// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// impactAnchorLabels is the ordered label set the by-id impact reads anchor on,
// derived from impactAnchorLabelDisjunction. A `MATCH (n:A|B|C) WHERE n.id = $id`
// disjunction anchor matches zero rows on the pinned NornicDB build, so callers
// resolve a by-id node with a per-label inline-property anchor instead (#5286).
var impactAnchorLabels = strings.Split(impactAnchorLabelDisjunction, "|")

// ResolvedImpactAnchor is a by-id node resolved to its canonical label.
//
// Exported because the impact package resolves anchors through the path-probe
// backend (#6060 lane B2). Field writes happen only in this package; readers
// use the exported fields and Pattern.
type ResolvedImpactAnchor struct {
	ID     string
	Name   string
	Label  string
	Labels []string
}

// Pattern returns the single-label inline-property start pattern for the
// resolved node, e.g. `(start:CloudResource {id: $id})`. The variable name and
// the id parameter name are caller-supplied so the pattern can fold into a
// single-clause traversal.
func (a ResolvedImpactAnchor) Pattern(variable, idParam string) string {
	return fmt.Sprintf("(%s:%s {id: $%s})", variable, a.Label, idParam)
}

// ImpactRelProvenance is one relationship's provenance decoded from a
// relationships(path) element (used by the by-id impact reads, #5286). The
// driver-typed decoding lives in package query (neo4j.go), which is the only
// non-test code allowed to import the graph driver; this plain struct crosses
// the package boundary so the impact family can shape hops without naming
// driver types. See #6060.
type ImpactRelProvenance struct {
	RelType    string
	Confidence float64
	HasConf    bool
	Reason     string
}

// ImpactNodeIdentity is the id/name of a nodes(path) element. Same boundary
// rationale as ImpactRelProvenance. See #6060.
type ImpactNodeIdentity struct {
	ID   string
	Name string
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

// ImpactRepoPathCypher is the trace-resource-to-code traversal from a resolved
// start node to Repository nodes. %s is the resolved single-label inline-property
// start pattern (anchored on the resolved canonical id, which is indexed) and %d
// is the max traversal depth. It projects the raw relationships(path) list,
// unwound into per-hop provenance in Go.
//
// Exported because the impact package formats this traversal for its by-id
// reads (#6060 lane B2).
const ImpactRepoPathCypher = `MATCH path = %s-[*1..%d]->(repo:Repository)
RETURN repo.id AS repo_id, repo.name AS repo_name, length(path) AS depth, relationships(path) AS rels
ORDER BY depth, repo_name, repo_id
LIMIT $limit`

// ResolveImpactAnchorNode resolves a by-id node to its canonical label so a
// caller can anchor a single-label inline-property traversal. It returns nil when
// no anchor-label node carries the id.
//
// Exported because the impact package resolves anchors through the path-probe
// backend (#6060 lane B2). It names only the GraphQuery port and row decoders,
// never driver types.
func ResolveImpactAnchorNode(ctx context.Context, reader querycontract.GraphQuery, idParam, id string) (*ResolvedImpactAnchor, error) {
	row, err := reader.RunSingle(ctx, impactAnchorResolveCypher(idParam), map[string]any{idParam: id})
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	label := querycontract.StringVal(row, "label")
	if label == "" {
		return nil, nil
	}
	return &ResolvedImpactAnchor{
		ID:     querycontract.StringVal(row, "id"),
		Name:   querycontract.StringVal(row, "name"),
		Label:  label,
		Labels: querycontract.StringSliceVal(row, "labels"),
	}, nil
}

// ImpactTraceHops builds the trace-resource-to-code hop provenance ({type,
// confidence, reason}) from decoded per-edge provenance. The driver-typed
// decoding runs in package query; this shaper takes the plain decoded slice
// so no driver import crosses the package boundary. See #6060.
func ImpactTraceHops(rels []ImpactRelProvenance) []map[string]any {
	hops := make([]map[string]any, 0, len(rels))
	for _, rel := range rels {
		hop := map[string]any{"type": rel.RelType}
		if rel.HasConf {
			hop["confidence"] = rel.Confidence
		}
		if rel.Reason != "" {
			hop["reason"] = rel.Reason
		}
		hops = append(hops, hop)
	}
	return hops
}

// ImpactDependencyHops builds the explain-dependency-path hop provenance
// ({from_id, from_name, to_id, to_name, type, confidence, reason}) by zipping
// decoded nodes(path) identities with decoded relationships(path) provenance.
// The from/to endpoints follow the path traversal order (source toward
// target); rels[i] connects nodes[i] and nodes[i+1]. Same boundary rationale
// as ImpactTraceHops. See #6060.
func ImpactDependencyHops(nodes []ImpactNodeIdentity, rels []ImpactRelProvenance) []map[string]any {
	hops := make([]map[string]any, 0, len(rels))
	for i, rel := range rels {
		hop := map[string]any{"type": rel.RelType}
		if i < len(nodes) {
			hop["from_id"] = nodes[i].ID
			hop["from_name"] = nodes[i].Name
		}
		if i+1 < len(nodes) {
			hop["to_id"] = nodes[i+1].ID
			hop["to_name"] = nodes[i+1].Name
		}
		if rel.HasConf {
			hop["confidence"] = rel.Confidence
		}
		if rel.Reason != "" {
			hop["reason"] = rel.Reason
		}
		hops = append(hops, hop)
	}
	return hops
}
