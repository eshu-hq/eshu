// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codetaintevidence builds the code-taint-evidence reducer intent from
// one immutable scope generation: a code_taint_evidence finding when present,
// else the code_dataflow_scanned marker as the retraction-reconcile fallback
// that lets the reducer clear stale CodeTaintEvidence nodes when a prior
// generation's findings were edited away (#2919). A finding always outranks
// the marker regardless of input order — the two kinds are looked up
// independently, with no cross-kind original-order merge. Only the envelope's
// FactKind, FactID, and CollectorKind are read; no payload is decoded, and
// schema-version admission stays with root projection. The source-system
// label is the trimmed CollectorKind alone, a single tier — not the two-tier
// projectorintent.SourceSystem fallback, which would prefer a SourceRef
// identity when one is set. The reducer's DomainCodeTaintEvidence handler
// owns typed decode, quarantine, graph writes, and stale-evidence retraction.
// Root projector assembly owns lookup construction and lifetime, invocation
// order, queue writes, retries, and telemetry.
package codetaintevidence
