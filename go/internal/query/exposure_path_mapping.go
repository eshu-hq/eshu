// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/exposure"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"
)

// graphBackedSinkSpecsByKind indexes the catalog's graph-backed sink specs by
// kind so the assembler can read each path's sink severity without re-deriving
// it.
func graphBackedSinkSpecsByKind() map[exposure.SinkKind]exposure.SinkSpec {
	specs := exposure.GraphBackedSinkSpecs()
	byKind := make(map[exposure.SinkKind]exposure.SinkSpec, len(specs))
	for _, spec := range specs {
		// The first graph-backed spec per kind carries the kind's severity; all
		// specs of a kind share it (IAM's three edges are all high/critical).
		if _, ok := byKind[spec.Kind]; !ok {
			byKind[spec.Kind] = spec
		}
	}
	return byKind
}

// graphBackedSinkRelationships returns the sorted, de-duplicated set of graph
// relationship types that terminate on a cataloged cloud sink, for the traversal
// WHERE clause.
func graphBackedSinkRelationships() []string {
	seen := map[string]struct{}{}
	for _, spec := range exposure.GraphBackedSinkSpecs() {
		if rel := strings.TrimSpace(spec.Relationship); rel != "" {
			seen[rel] = struct{}{}
		}
	}
	rels := make([]string, 0, len(seen))
	for rel := range seen {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	return rels
}

// exposurePathCandidateFromRow maps one traversal row into a PathCandidate,
// recognizing the sink via the catalog. The sink node's labels and scalar
// properties are matched against every graph-backed spec; a row whose sink edge
// does not match the catalog is dropped (no fabricated sink).
func exposurePathCandidateFromRow(row map[string]any) (exposure.PathCandidate, bool) {
	sinkRel := StringVal(row, "sink_rel")
	sinkNode, sinkProps := exposurePathNodeFromAny(row["sink_node"])
	if labels := StringSliceVal(row, "sink_labels"); len(sinkNode.Labels) == 0 {
		sinkNode.Labels = labels
	}

	spec, ok := matchSinkForLabels(sinkRel, sinkNode.Labels, sinkProps)
	if !ok {
		return exposure.PathCandidate{}, false
	}
	nodes := exposurePathNodesFromAny(row["chain"])
	if len(nodes) == 0 {
		return exposure.PathCandidate{}, false
	}
	return exposure.PathCandidate{
		Nodes: nodes,
		Sink: exposure.SinkHit{
			Kind:        spec.Kind,
			DisplayName: spec.DisplayName,
			Node:        sinkNode,
		},
		Depth: IntVal(row, "depth"),
	}, true
}

// matchSinkForLabels tries the catalog recognizer against each of the sink node's
// labels, returning the first matching spec. A node may carry several labels;
// only one needs to match a cataloged (relationship, target label, predicate)
// tuple.
func matchSinkForLabels(rel string, labels []string, props map[string]string) (exposure.SinkSpec, bool) {
	for _, label := range labels {
		if spec, ok := exposure.MatchSink(rel, label, props); ok {
			return spec, true
		}
	}
	return exposure.SinkSpec{}, false
}

// exposurePathNodesFromAny maps a raw nodes(path) value (typed Bolt nodes from
// either backend, or canned maps in tests) into ordered PathNodes.
func exposurePathNodesFromAny(raw any) []exposure.PathNode {
	switch nodes := raw.(type) {
	case []any:
		return exposurePathNodesFromSlice(nodes)
	case []map[string]any:
		converted := make([]any, len(nodes))
		for i := range nodes {
			converted[i] = nodes[i]
		}
		return exposurePathNodesFromSlice(converted)
	case []dbtype.Node:
		converted := make([]any, len(nodes))
		for i := range nodes {
			converted[i] = nodes[i]
		}
		return exposurePathNodesFromSlice(converted)
	default:
		return nil
	}
}

func exposurePathNodesFromSlice(nodes []any) []exposure.PathNode {
	out := make([]exposure.PathNode, 0, len(nodes))
	for _, node := range nodes {
		pathNode, _ := exposurePathNodeFromAny(node)
		if pathNode.EntityID == "" {
			// An unrecognized node type (or a nil element) yields a zero-value
			// node; skip it so a path never carries a phantom node with no
			// identity.
			continue
		}
		out = append(out, pathNode)
	}
	return out
}

// exposurePathNodeFromAny extracts a PathNode and its scalar string properties
// from a typed Bolt node or a canned map. Properties are returned so sink
// predicates (e.g. CidrBlock is_internet) can be evaluated.
func exposurePathNodeFromAny(raw any) (exposure.PathNode, map[string]string) {
	switch node := raw.(type) {
	case dbtype.Node:
		props := scalarStringProps(node.Props)
		return exposure.PathNode{
			EntityID: nodeIdentity(node.Props),
			Name:     props["name"],
			Labels:   append([]string(nil), node.Labels...),
		}, props
	case map[string]any:
		props := scalarStringProps(node)
		labels := stringSliceFromAny(node["labels"])
		return exposure.PathNode{
			EntityID: nodeIdentity(node),
			Name:     props["name"],
			Labels:   labels,
		}, props
	default:
		return exposure.PathNode{}, map[string]string{}
	}
}

// nodeIdentity returns a node's stable identity, preferring id then uid.
func nodeIdentity(props map[string]any) string {
	for _, key := range []string{"id", "uid"} {
		if v, ok := props[key]; ok {
			if s := strings.TrimSpace(fmt.Sprintf("%v", v)); s != "" {
				return s
			}
		}
	}
	return ""
}

// scalarStringProps renders a node's scalar properties as strings for predicate
// matching (booleans become "true"/"false"). Non-scalar values are skipped.
func scalarStringProps(props map[string]any) map[string]string {
	out := make(map[string]string, len(props))
	for key, value := range props {
		switch v := value.(type) {
		case string:
			out[key] = v
		case bool:
			out[key] = fmt.Sprintf("%t", v)
		case int, int8, int16, int32, int64, float32, float64:
			out[key] = fmt.Sprintf("%v", v)
		}
	}
	return out
}

// writeExposureFinding serializes an exposure finding into the API response with
// a derived truth envelope.
func (h *ImpactHandler) writeExposureFinding(w http.ResponseWriter, r *http.Request, finding exposure.ExposureFinding) {
	WriteSuccess(w, r, http.StatusOK, exposureFindingPayload(finding),
		BuildTruthEnvelope(h.profile(), exposurePathCapability, TruthBasisHybrid,
			"derived from bounded symbol-level reachability over the call graph and the cloud-sink catalog; not value-flow"))
}

// exposureFindingPayload renders the finding into the stable API/MCP JSON shape.
func exposureFindingPayload(finding exposure.ExposureFinding) map[string]any {
	paths := make([]map[string]any, 0, len(finding.Paths))
	for _, p := range finding.Paths {
		paths = append(paths, map[string]any{
			"nodes":    exposurePathNodesPayload(p.Nodes),
			"sink":     exposureSinkPayload(p.Sink),
			"depth":    p.Depth,
			"state":    string(p.State),
			"severity": string(p.Severity),
			"reason":   p.Reason,
		})
	}
	return map[string]any{
		"source": map[string]any{
			"entity_id": finding.Source.EntityID,
			"name":      finding.Source.Name,
			"labels":    finding.Source.Labels,
		},
		"source_kind":   string(finding.SourceKind),
		"exposure_rank": string(finding.ExposureRank),
		"truth_label":   finding.TruthLabel,
		"state":         string(finding.State),
		"paths":         paths,
		"coverage": map[string]any{
			"max_depth":         finding.Coverage.MaxDepth,
			"paths_found":       finding.Coverage.PathsFound,
			"truncated":         finding.Coverage.Truncated,
			"unresolved_reason": finding.Coverage.UnresolvedReason,
		},
	}
}

func exposurePathNodesPayload(nodes []exposure.PathNode) []map[string]any {
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, map[string]any{
			"entity_id": n.EntityID,
			"name":      n.Name,
			"labels":    n.Labels,
		})
	}
	return out
}

func exposureSinkPayload(sink exposure.SinkHit) map[string]any {
	return map[string]any{
		"kind":         string(sink.Kind),
		"display_name": sink.DisplayName,
		"node": map[string]any{
			"entity_id": sink.Node.EntityID,
			"name":      sink.Node.Name,
			"labels":    sink.Node.Labels,
		},
	}
}
