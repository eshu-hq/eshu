// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphdump

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/replay"
)

// canonicalOptions is the replay.CanonicalOptions graphdump passes to the
// shared canonicalizer for every node, edge, and whole-graph record. It is
// the zero value: no VolatileKeys, DerivedKeys, SecretKeys, or SortArrays, so
// replay does only what its zero value promises — sort object keys and
// indent — leaving all graph-specific normalization to normalizeProps.
// replay's cassette-shaped defaults (DefaultCanonicalOptions) collapse
// observed_at to a sentinel and derive generation_id from scope_id, which is
// exactly wrong for a graph node: those are deterministic node content here,
// not run-specific cassette metadata, so using the zero value instead of
// DefaultCanonicalOptions is a deliberate choice, not an oversight.
var canonicalOptions = replay.CanonicalOptions{}

// nodeRecord returns the canonical JSON value for a single node: its sorted
// label set and its normalized property map. Two Node values with the same
// labels and (post-normalization) properties always produce byte-identical
// records regardless of slice/map iteration order, which is what lets an
// edge's endpoint be identified by content digest instead of by backend
// element ID.
func nodeRecord(n Node) map[string]any {
	return map[string]any{
		"labels": sortedLabels(n.Labels),
		"props":  normalizeProps(n.Props),
	}
}

// nodeDigest returns the sha256 hex digest of n's canonical record. It is
// the content address an edge's endpoint is compared against: it depends
// only on Labels and (normalized) Props, never on a backend element ID,
// slice position, or map iteration order.
func nodeDigest(n Node) (string, error) {
	bs, err := replay.CanonicalizeValue(nodeRecord(n), canonicalOptions)
	if err != nil {
		return "", fmt.Errorf("canonicalize node record: %w", err)
	}
	sum := sha256.Sum256(bs)
	return hex.EncodeToString(sum[:]), nil
}

// edgeRecord returns the canonical JSON value for a single edge: its type,
// its endpoints' content digests (never backend IDs), and its normalized
// property map.
func edgeRecord(e Edge) (map[string]any, error) {
	fromDigest, err := nodeDigest(Node{Labels: e.FromLabels, Props: e.FromProps})
	if err != nil {
		return nil, fmt.Errorf("digest %q edge source endpoint: %w", e.Type, err)
	}
	toDigest, err := nodeDigest(Node{Labels: e.ToLabels, Props: e.ToProps})
	if err != nil {
		return nil, fmt.Errorf("digest %q edge target endpoint: %w", e.Type, err)
	}
	return map[string]any{
		"type":  e.Type,
		"from":  fromDigest,
		"to":    toDigest,
		"props": normalizeProps(e.Props),
	}, nil
}

// canonicalBytesOf marshals value through replay's shared canonicalizer,
// wrapping any error with ctx for a caller-meaningful message.
func canonicalBytesOf(value any, ctx string) ([]byte, error) {
	bs, err := replay.CanonicalizeValue(value, canonicalOptions)
	if err != nil {
		return nil, fmt.Errorf("canonicalize %s: %w", ctx, err)
	}
	return bs, nil
}

// decodeCanonical decodes each record's canonical JSON bytes back into a
// generic value, using json.Number for numeric literals (matching replay's
// own Canonicalize decode path) so a large integer property is not silently
// rounded through a float64 round-trip before the final re-encoding pass.
func decodeCanonical(records [][]byte) ([]any, error) {
	out := make([]any, len(records))
	for i, bs := range records {
		dec := json.NewDecoder(bytes.NewReader(bs))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("decode canonical record %d: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

// byCanonicalBytes sorts canonical record byte slices lexicographically, the
// same tiebreak rule replay's own sortArray uses for its array elements. This
// is graphdump's whole-array sort key — unlike replay's SortArrays (which
// orders by a named sibling field, falling back to canonical-byte tiebreak),
// graphdump has no natural field to sort a node or edge record by, since the
// record's full canonical bytes ARE its identity. Reimplementing that as a
// plain bytes.Compare sort here is simpler and clearer than contorting
// replay's SortArrays into ordering by an always-absent field name.
func byCanonicalBytes(records [][]byte) {
	sort.Slice(records, func(i, j int) bool {
		return bytes.Compare(records[i], records[j]) < 0
	})
}

// Canonicalize reads every node and edge from r and returns the graph's
// stable canonical byte form: content-addressed, order-independent, and
// idempotent. Two reads of an unchanged graph — regardless of the order the
// backend returns nodes/edges in — produce byte-identical output; any
// genuine difference in graph content (a changed property, a changed edge
// type, an added/removed node or edge) changes the output.
func Canonicalize(ctx context.Context, r Reader) ([]byte, error) {
	nodes, err := r.Nodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("read nodes: %w", err)
	}
	edges, err := r.Edges(ctx)
	if err != nil {
		return nil, fmt.Errorf("read edges: %w", err)
	}

	nodeBytes := make([][]byte, len(nodes))
	for i, n := range nodes {
		bs, err := canonicalBytesOf(nodeRecord(n), fmt.Sprintf("node %d", i))
		if err != nil {
			return nil, err
		}
		nodeBytes[i] = bs
	}
	edgeBytes := make([][]byte, len(edges))
	for i, e := range edges {
		rec, err := edgeRecord(e)
		if err != nil {
			return nil, fmt.Errorf("build edge %d record: %w", i, err)
		}
		bs, err := canonicalBytesOf(rec, fmt.Sprintf("edge %d", i))
		if err != nil {
			return nil, err
		}
		edgeBytes[i] = bs
	}

	byCanonicalBytes(nodeBytes)
	byCanonicalBytes(edgeBytes)

	sortedNodes, err := decodeCanonical(nodeBytes)
	if err != nil {
		return nil, fmt.Errorf("decode sorted node records: %w", err)
	}
	sortedEdges, err := decodeCanonical(edgeBytes)
	if err != nil {
		return nil, fmt.Errorf("decode sorted edge records: %w", err)
	}

	graph := map[string]any{
		"nodes": sortedNodes,
		"edges": sortedEdges,
	}
	return canonicalBytesOf(graph, "graph document")
}

// Digest returns the sha256 hex digest of Canonicalize's output for r.
func Digest(ctx context.Context, r Reader) (string, error) {
	bs, err := Canonicalize(ctx, r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bs)
	return hex.EncodeToString(sum[:]), nil
}

// Equal reports whether a and b canonicalize to byte-identical graphs. It is
// a convenience wrapper over Digest for the common determinism-matrix
// comparison (graph after N=1 vs. graph after N=4, etc.).
func Equal(ctx context.Context, a, b Reader) (bool, error) {
	da, err := Digest(ctx, a)
	if err != nil {
		return false, fmt.Errorf("digest first graph: %w", err)
	}
	db, err := Digest(ctx, b)
	if err != nil {
		return false, fmt.Errorf("digest second graph: %w", err)
	}
	return da == db, nil
}
