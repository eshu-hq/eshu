// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package admissiondecision holds the shared reducer admission-decision
// vocabulary that lets any correlation or materialization handler explain,
// in a common shape, why one candidate ended up admitted, rejected,
// ambiguous, stale, missing evidence, permission-hidden, unsupported, or
// unsafe (issue #6061).
//
// [AdmissionDecision] is one shared decision, always keyed by a
// [StableAdmissionDecisionID] derived from domain, scope, generation, anchor,
// and candidate identity so retries and reprojections converge on the same
// row. [AdmissionDecisionEvidence] carries the bounded, redaction-safe
// evidence a decision cites -- a [AdmissionDecisionSourceHandle] rather than
// embedded raw provider payload. [WriteAdmissionDecisions] persists a batch
// through an optional [AdmissionDecisionWriter]; a nil writer or an empty
// batch is a no-op, so a caller never needs its own nil check.
//
// This package is a leaf: it holds only the shared vocabulary, constructors,
// and the persistence seam, never a specific domain's admission logic. Each
// consuming handler (cloud-inventory admission, deployable-unit correlation,
// package-source correlation) owns its own state derivation and calls
// [NewAdmissionDecision] and [NewAdmissionDecisionEvidence] to shape its
// domain's outcome into the shared row. [AdmissionNow] is the clock-resolution
// seam those handlers use to make decision timestamps deterministic in tests:
// pass the handler's own optional now field and AdmissionNow falls back to
// time.Now().UTC() when it is nil.
//
// This package never imports internal/reducer.
package admissiondecision
