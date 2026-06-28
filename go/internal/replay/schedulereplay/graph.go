// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedulereplay

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Node is one canonical graph node in the in-memory order-replay model. Identity
// is (Label, ID): the same (Label, ID) delivered twice is one node, so node
// application is idempotent and therefore delivery-order independent.
type Node struct {
	Label string
	ID    string
	Props map[string]string
}

// Key returns the node's identity key. Edges reference endpoints by this key.
func (n Node) Key() string {
	return n.Label + "/" + n.ID
}

// Edge is one canonical directed relationship between two node keys. Identity is
// the (From, Rel, To) tuple, so edge application is idempotent and
// order-independent for a correct (set-semantics) applier.
type Edge struct {
	From string
	Rel  string
	To   string
}

// Graph is the in-memory canonical graph the schedule replay drains work items
// into. It models exactly what the offline replay tier asserts as graph truth —
// canonical nodes keyed by identity and directed edges keyed by tuple — with no
// graph backend and no Postgres, so the order-invariance gate runs on every PR.
//
// Graph is NOT safe for concurrent use; the scenario runner serializes all
// mutations behind a mutex so concurrent reducer workers can share one graph.
type Graph struct {
	nodes map[string]Node
	edges map[string]Edge
}

// NewGraph returns an empty canonical graph.
func NewGraph() *Graph {
	return &Graph{nodes: map[string]Node{}, edges: map[string]Edge{}}
}

// UpsertNode idempotently records a node by its (Label, ID) identity. Re-applying
// the same node is a no-op, which is what makes duplicate and reordered delivery
// converge on the same graph.
func (g *Graph) UpsertNode(n Node) {
	g.nodes[n.Key()] = n
}

// HasNode reports whether a node with the given key has been applied. It exists
// so an order-sensitive applier can be written (and proven to be caught) in
// tests; the canonical applier never consults it.
func (g *Graph) HasNode(key string) bool {
	_, ok := g.nodes[key]
	return ok
}

// UpsertEdge idempotently records a directed edge by its (From, Rel, To) tuple.
func (g *Graph) UpsertEdge(e Edge) {
	g.edges[e.From+"\x00"+e.Rel+"\x00"+e.To] = e
}

type nodeDoc struct {
	Label string            `json:"label"`
	ID    string            `json:"id"`
	Props map[string]string `json:"props,omitempty"`
}

type edgeDoc struct {
	From string `json:"from"`
	Rel  string `json:"rel"`
	To   string `json:"to"`
}

type snapshotDoc struct {
	Nodes []nodeDoc `json:"nodes"`
	Edges []edgeDoc `json:"edges"`
}

// Canonical serializes the graph to a deterministic, indented JSON snapshot:
// nodes sorted by (Label, ID), edges sorted by (From, Rel, To), and per-node
// props emitted with json's sorted-key map encoding. Two graphs with the same
// node and edge sets always produce byte-identical output, so the snapshot is
// the order-independent graph-truth fingerprint the gate compares.
func (g *Graph) Canonical() ([]byte, error) {
	doc := snapshotDoc{
		Nodes: make([]nodeDoc, 0, len(g.nodes)),
		Edges: make([]edgeDoc, 0, len(g.edges)),
	}
	for _, n := range g.nodes {
		doc.Nodes = append(doc.Nodes, nodeDoc(n))
	}
	for _, e := range g.edges {
		doc.Edges = append(doc.Edges, edgeDoc(e))
	}
	sort.Slice(doc.Nodes, func(a, b int) bool {
		if doc.Nodes[a].Label != doc.Nodes[b].Label {
			return doc.Nodes[a].Label < doc.Nodes[b].Label
		}
		return doc.Nodes[a].ID < doc.Nodes[b].ID
	})
	sort.Slice(doc.Edges, func(a, b int) bool {
		if doc.Edges[a].From != doc.Edges[b].From {
			return doc.Edges[a].From < doc.Edges[b].From
		}
		if doc.Edges[a].Rel != doc.Edges[b].Rel {
			return doc.Edges[a].Rel < doc.Edges[b].Rel
		}
		return doc.Edges[a].To < doc.Edges[b].To
	})
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal canonical snapshot: %w", err)
	}
	return out, nil
}
