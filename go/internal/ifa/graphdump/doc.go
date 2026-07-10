// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package graphdump canonicalizes an arbitrary property graph (any label
// set, any edge type) into a stable byte form so two reads of the graph can
// be compared for exact equality. It is the graph-truth half of Ifá's P3
// determinism matrix (issue #4396, design doc
// docs/internal/design/4389-ifa-conformance-platform.md, Layer 2): after
// replaying the same Odù at worker counts N ∈ {1, 2, 4, ...}, the matrix
// canonicalizes the resulting graph at each N and asserts the bytes are
// identical, so a divergence is a real concurrency defect (a MERGE race, a
// dropped write, an ordering-dependent projection) rather than a
// scan-order or backend-ID artifact.
//
// # Content addressing, not ID addressing
//
// A graph backend element ID (NornicDB's or Neo4j's internal node/edge
// handle) is run-local: it depends on insertion order and internal storage
// layout, not on graph content, so two runs of an identical write workload
// can assign the same logical node different element IDs. If Canonicalize
// referenced edge endpoints by element ID, every determinism comparison
// would spuriously fail even when the projected graph is truly identical.
//
// graphdump avoids this by making node identity content-addressed: a node's
// canonical form is sha256(canonical JSON of {labels, props}), and an edge's
// canonical form references its endpoints by that digest rather than by any
// backend handle. Reader (see reader.go) enforces this at the type level —
// Node and Edge never carry an element ID field — so a caller cannot
// accidentally leak one in.
//
// Soundness of content-addressing rests on one assumption about the graph
// being dumped: every distinct node must carry at least one property that
// distinguishes it from any other node with the same labels. Two nodes with
// identical labels and identical (post-denylist) properties collapse to the
// same digest, and any edge touching either endpoint resolves to that shared
// digest — so a race that swapped edges between two truly-identical,
// identity-less nodes could hide behind equal digests (a false green). This
// holds in Eshu because every canonical writer materializes its node with a
// stable `uid` property (e.g. CloudResourceNodeWriter keys the node on it via
// `MERGE (r:CloudResource {uid: row.uid})`), so genuinely distinct nodes
// never share a digest. A future writer that materializes identity-less auxiliary
// nodes would weaken this guarantee and must be given a stable identity
// property before graphdump can soundly compare it.
//
// # Reused canonical JSON core
//
// graphdump reuses go/internal/replay's CanonicalizeValue for the actual
// JSON serialization (sorted object keys, no HTML escaping, stable indent,
// idempotent output) rather than writing a second canonicalizer — internal/
// ifa's own AGENTS.md already mandates this reuse for Odù comparison, and
// the same reasoning applies here: two independent "canonical JSON" cores
// in one repository is exactly the kind of drift this package exists to
// prevent. graphdump passes replay a zero-value CanonicalOptions rather than
// DefaultCanonicalOptions: the defaults collapse an `observed_at` value to a
// fixed sentinel and derive `generation_id` from `scope_id`, which is
// correct for a *cassette* (a recorded envelope where those fields are
// deliberately volatile run metadata) but wrong for a *graph node*, where
// `observed_at`, `generation_id`, `uid`, and `source_fact_id` are
// deterministic content produced by the fact pipeline. graphdump instead
// owns a single, separate, deliberately narrow normalization: the
// denylist in normalize.go.
//
// # The over-normalize / under-normalize tradeoff
//
// Every property Canonicalize drops widens the set of graphs that compare
// equal. Drop too little and a genuinely deterministic pipeline fails the
// matrix on a false red (a harmless, expected timestamp churns the digest).
// Drop too much and a real concurrency defect — a race that corrupts one
// property on one node — disappears into a false green, which is worse: it
// is exactly the failure mode "Serialization Is Not A Fix" and the repo's
// accuracy-first motto exist to prevent. graphdump's answer to this
// tradeoff is to start from the evidence: a prove-the-theory shim run
// against a real populated NornicDB (demo-org cassette, AWS/Terraform/code
// collector output) found zero wall-clock, run-local, or internal-ID
// properties exposed through labels()/properties() other than the reducer's
// orphan-sweep marker. denylistKeys therefore ships with exactly one entry,
// each entry documented with the concrete write path that stamps it. Adding
// a new entry requires the same evidence bar: a demonstrated volatile key
// observed on a real graph, not a hypothetical one.
//
// Array-element reordering (sorting values within a single property's array,
// for properties whose element order is not semantically meaningful) is
// deliberately NOT implemented in this slice: the same shim found no
// property needing it. The hook is documented here rather than built
// speculatively — if Layer 2/3 evidence later surfaces an
// order-nondeterministic array property, add it as a new, evidenced,
// per-key opt-in next to normalizeProps, following the same
// evidence-first bar as denylistKeys. Do not sort every array prop by
// default: a property whose order IS meaningful (an ordered list value)
// would silently lose that information, an under-normalization in the
// opposite direction — a false green that hides a real ordering defect.
package graphdump
