// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cicdruncorrelation builds the CI/CD run-correlation reducer intent
// from one immutable scope generation: a ci.run fact when present, else a
// ci.artifact fact, so an artifact arriving without a co-located run still
// triggers the reducer's bounded historical-run patch (#5770). A ci.run
// always outranks a same-generation ci.artifact regardless of input order —
// two independent FirstOfKind probes, not a merged anchor selection (#5710).
// Only the envelope's FactKind, FactID, SourceRef, and CollectorKind are
// read; no payload is decoded, and schema-version admission stays with root
// projection. The intent's source-system label is the shared two-tier
// projectorintent.SourceSystem fallback (trimmed SourceRef.SourceSystem,
// else trimmed CollectorKind) — the pre-extraction local helper had the
// identical body, so this is a behavior-preserving substitution, not a
// change. The reducer's CICDRunCorrelationHandler owns the full-snapshot and
// bounded-patch correlation logic, the cross-scope container-image-identity
// read, and the durable decision write; root projector assembly owns lookup
// construction and lifetime, invocation order, queue writes, retries, and
// telemetry.
package cicdruncorrelation
