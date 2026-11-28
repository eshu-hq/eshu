// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package coreserve is the versioned shared auth serving library
// (eshu-hq/eshu#11618): the serving seam of package query
// (CoreServeDeps, CoreAuthWiring, NewCoreServeMux) re-exported under
// module github.com/eshu-hq/eshu/go/coreserve so exactly one binary
// computes a given auth family while eshu-api and core-api converge on
// the same pinned implementation during the transition.
//
// Every exported name is a type alias (identity-preserving, no conversion
// layer, no copied query tree): fixture Store implementations written
// against this package satisfy the underlying query interfaces directly.
// NewCoreServeMux delegates to query.NewCoreServeMux; this package adds
// no middleware, no routes, and no runtime path of its own.
//
// The surface splits across three files: coreserve.go carries the serving
// seam (CoreServeDeps, CoreAuthWiring, NewCoreServeMux), types.go carries
// the auth-safe provider and CoreServeDeps field types an external binary
// needs to populate the seam without importing package query, and
// handlers.go carries the auth-family handler aliases naming every
// CoreAuthWiring field. Construction and store wiring stay in the serving
// binary, which alone may touch secret-sealed material: this package never
// imports internal/secretcrypto, and secret-sealed material must never
// cross it.
//
// v0.1.0 surface limits: component.Policy, telemetry.Instruments,
// status.AnswerNarrationStatus, APIRouter, and the ExtraMounts hook are
// deliberately NOT re-exported. External callers leave ComponentPolicy,
// Instruments, NarrationPosture, and ExtraMounts zero (nil/empty), which
// the underlying mux already treats as safe no-op defaults. Widening
// the surface needs a follow-up version, not a local fork.
//
// Module discipline: this directory is its own module
// (github.com/eshu-hq/eshu/go/coreserve) with no replace directives; the
// parent require pins the merge-commit pseudo-version until the tag
// sequence in README.md completes, after which consumers pin the
// coreserve tag and never a pseudo-versioned fork.
//
// No-observability-change: pure delegation, no new metric, span, log,
// status field, env var, queue, worker, lease, batch, runtime knob, or
// graph query. Operators keep diagnosing the auth family through the
// existing core-api query handler spans and /readyz probe.
package coreserve
