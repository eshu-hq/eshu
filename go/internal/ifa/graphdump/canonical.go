// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphdump

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	// Stream nodes and edges: each record is canonicalized to bytes inside the
	// yield callback and the Node/Edge struct is then discarded, so peak memory
	// holds only the canonical record set ([][]byte), never the full struct
	// graph (issue #5009). An edge duplicates both endpoints' property maps, so
	// the struct set is far larger than the byte set at scale-lab-slot scale.
	var nodeBytes [][]byte
	nodeIdx := 0
	if err := r.StreamNodes(ctx, func(n Node) error {
		bs, err := canonicalBytesOf(nodeRecord(n), fmt.Sprintf("node %d", nodeIdx))
		nodeIdx++
		if err != nil {
			return err
		}
		nodeBytes = append(nodeBytes, bs)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read nodes: %w", err)
	}

	var edgeBytes [][]byte
	edgeIdx := 0
	if err := r.StreamEdges(ctx, func(e Edge) error {
		rec, err := edgeRecord(e)
		if err != nil {
			return fmt.Errorf("build edge %d record: %w", edgeIdx, err)
		}
		bs, err := canonicalBytesOf(rec, fmt.Sprintf("edge %d", edgeIdx))
		edgeIdx++
		if err != nil {
			return err
		}
		edgeBytes = append(edgeBytes, bs)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read edges: %w", err)
	}

	byCanonicalBytes(nodeBytes)
	byCanonicalBytes(edgeBytes)

	// Assemble the final {"edges":[...],"nodes":[...]} document directly from the
	// already-canonical, already-sorted record bytes instead of decoding them
	// back into map[string]any and re-canonicalizing (issue #5009): the decode
	// round-trip rebuilt one map per record, re-exploding memory at scale. Each
	// record is emitted verbatim, re-indented to its nested position, which
	// reproduces the shared canonicalizer's byte-identical output — proven
	// against pinned digests in graphdump_test.go.
	return assembleGraph(nodeBytes, edgeBytes), nil
}

// assembleGraph builds the canonical graph document from the sorted node and
// edge record bytes. Keys are emitted in sorted order ("edges" < "nodes") and
// each record is re-indented four spaces to its array-element depth, matching
// exactly what replay.CanonicalizeValue produces for the equivalent
// map[string]any (verified byte-for-byte by the digest tests).
func assembleGraph(nodeBytes, edgeBytes [][]byte) []byte {
	var b bytes.Buffer
	b.WriteString("{\n  \"edges\": ")
	writeRecordArray(&b, edgeBytes)
	b.WriteString(",\n  \"nodes\": ")
	writeRecordArray(&b, nodeBytes)
	b.WriteString("\n}\n")
	return b.Bytes()
}

// writeRecordArray writes a JSON array of canonical records at object-value
// depth. An empty array is the compact "[]"; a non-empty array opens on its own
// line with each record re-indented four spaces and separated by ",\n",
// matching the shared canonicalizer's indentation.
func writeRecordArray(b *bytes.Buffer, records [][]byte) {
	if len(records) == 0 {
		b.WriteString("[]")
		return
	}
	b.WriteString("[\n")
	for i, rec := range records {
		if i > 0 {
			b.WriteString(",\n")
		}
		writeIndentedRecord(b, rec)
	}
	b.WriteString("\n  ]")
}

// writeIndentedRecord writes a single canonical record (which carries a
// trailing newline from the canonicalizer) at array-element depth, prefixing
// every line with four spaces so its top-level "{" lands at the four-space
// indent the enclosing array expects.
func writeIndentedRecord(b *bytes.Buffer, rec []byte) {
	rec = bytes.TrimSuffix(rec, []byte("\n"))
	for i, line := range bytes.Split(rec, []byte("\n")) {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("    ")
		b.Write(line)
	}
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
