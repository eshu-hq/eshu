// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"context"
	"fmt"
)

// Reach is how the labeled Neo4j entity-context anchor reaches a node.
type Reach int

const (
	// NoID marks a node without an id property: the anchor and the retired
	// unlabeled fallback both ignore it, so the census does not count it.
	NoID Reach = iota
	// ReachViaID is an id-bearing node on an id-constrained label.
	ReachViaID
	// ReachViaUID is an id-bearing node, on no id-constrained label, whose uid
	// equals its id on a uid-constrained label.
	ReachViaUID
	// Unreachable is an id-bearing node the labeled anchor cannot return.
	Unreachable
)

// Node is the part of a graph node the census reads.
type Node struct {
	// Labels are the node's labels.
	Labels []string
	// ID is the id property, nil when absent.
	ID *string
	// UID is the uid property, nil when absent.
	UID *string
}

// Classify is the reference definition of anchor reachability. A node counts
// once: the id branch wins when a node is reachable through both branches.
// The Cypher in [CensusCypher] must agree with it; a live test compares them.
func Classify(node Node, uidLabels, idLabels map[string]bool) Reach {
	if node.ID == nil {
		return NoID
	}
	viaUID := false
	for _, label := range node.Labels {
		if idLabels[label] {
			return ReachViaID
		}
		if uidLabels[label] && node.UID != nil && *node.UID == *node.ID {
			viaUID = true
		}
	}
	if viaUID {
		return ReachViaUID
	}
	return Unreachable
}

// Census is one anchor-reachability count over the whole graph. It is a
// snapshot of one read transaction, not a point in time: a node written during
// the scan may or may not be counted.
type Census struct {
	// IDBearing is the number of nodes with a non-null id.
	IDBearing int64
	// ViaID is how many of them sit on an id-constrained label.
	ViaID int64
	// ViaUIDOnly is how many of the rest are reachable by uid equal to id.
	ViaUIDOnly int64
	// Residual is the number of id-bearing nodes the anchor cannot reach.
	Residual int64
}

// CensusSource runs the census against a graph.
type CensusSource interface {
	AnchorCensus(ctx context.Context) (Census, error)
}

// CensusCypher counts id-bearing nodes by anchor reachability in one pass.
// `coalesce(n.uid = n.id, false)` is load-bearing: a node with a null uid makes
// the comparison null, and `NOT (null AND ...)` is null, which a WHERE would
// drop — the node would vanish from the residual instead of being counted.
//
// The statement is one AllNodesScan, measured at 1.95 s over 1,130,424 nodes
// on 2026-10-08 (image sha-57167b0), so callers run it on an interval with a
// timeout and never on a request or scrape path.
const CensusCypher = `MATCH (n)
WHERE n.id IS NOT NULL
WITH
  any(l IN labels(n) WHERE l IN $id_labels) AS via_id,
  (coalesce(n.uid = n.id, false) AND any(l IN labels(n) WHERE l IN $uid_labels)) AS via_uid
RETURN
  count(*) AS id_bearing,
  sum(CASE WHEN via_id THEN 1 ELSE 0 END) AS via_id,
  sum(CASE WHEN NOT via_id AND via_uid THEN 1 ELSE 0 END) AS via_uid_only,
  sum(CASE WHEN NOT via_id AND NOT via_uid THEN 1 ELSE 0 END) AS residual`

// ResidualByLabelsCypher groups the unreachable nodes by label set, so a
// failing census names the shape without printing any id. It is bounded by
// LIMIT and runs only after [CensusCypher] reports a residual.
//
// The two reachability booleans are computed in a WITH, never in the WHERE: a
// quantifier over labels() inside a WHERE is a label predicate NornicDB does
// not evaluate (#6786 X11), and the WHERE here holds only boolean variables.
const ResidualByLabelsCypher = `MATCH (n)
WHERE n.id IS NOT NULL
WITH n,
  any(l IN labels(n) WHERE l IN $id_labels) AS via_id,
  (coalesce(n.uid = n.id, false) AND any(l IN labels(n) WHERE l IN $uid_labels)) AS via_uid
WHERE NOT via_id AND NOT via_uid
RETURN labels(n) AS labels, count(*) AS nodes
ORDER BY nodes DESC
LIMIT 20`

// CensusParameters returns the bound parameters of [CensusCypher] and
// [ResidualByLabelsCypher]: the schema-derived label sets.
func CensusParameters() map[string]any {
	return map[string]any{"uid_labels": UIDLabels(), "id_labels": IDLabels()}
}

// ParseCensusRow reads the single row [CensusCypher] returns.
func ParseCensusRow(row map[string]any) (Census, error) {
	var out Census
	for _, field := range []struct {
		key string
		dst *int64
	}{
		{"id_bearing", &out.IDBearing},
		{"via_id", &out.ViaID},
		{"via_uid_only", &out.ViaUIDOnly},
		{"residual", &out.Residual},
	} {
		value, ok := wholeNumber(row[field.key])
		if !ok {
			return Census{}, fmt.Errorf("anchor census row: %q is %T, want a whole number", field.key, row[field.key])
		}
		*field.dst = value
	}
	return out, nil
}

// Verdict is the outcome of the corpus-scale census gate.
type Verdict struct {
	// OK is true when every id-bearing node is anchor-reachable.
	OK bool
	// Detail is the one-line explanation the gate prints.
	Detail string
}

// EvaluateCensus runs the census and judges it. The residual must be zero. A
// graph with no id-bearing node fails too: an empty projection would otherwise
// pass vacuously. A census that cannot run fails closed.
func EvaluateCensus(ctx context.Context, source CensusSource) Verdict {
	census, err := source.AnchorCensus(ctx)
	if err != nil {
		return Verdict{Detail: fmt.Sprintf("anchor census did not run: %v", err)}
	}
	summary := fmt.Sprintf("id-bearing %d, via uid %d, via id %d, residual %d",
		census.IDBearing, census.ViaUIDOnly, census.ViaID, census.Residual)
	switch {
	case census.IDBearing == 0:
		return Verdict{Detail: "no id-bearing node in the graph: an empty projection proves nothing (" + summary + ")"}
	case census.Residual != 0:
		return Verdict{Detail: fmt.Sprintf("%d id-bearing node(s) are not reachable by the labeled anchor (%s)", census.Residual, summary)}
	}
	return Verdict{OK: true, Detail: summary}
}

// wholeNumber reads a count a graph driver or reader may hand back as int64,
// int, or a whole float64.
func wholeNumber(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case float64:
		if typed == float64(int64(typed)) {
			return int64(typed), true
		}
	}
	return 0, false
}
