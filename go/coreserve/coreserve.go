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
// v0.1.0 surface limits: component.Policy, telemetry.Instruments,
// status.AnswerNarrationStatus, APIRouter, and the ExtraMounts hook are
// deliberately NOT re-exported. External callers leave ComponentPolicy,
// Instruments, NarrationPosture, and ExtraMounts zero (nil/empty), which
// the underlying mux already treats as safe no-op defaults. Widening
// the surface needs a follow-up version, not a local fork.
//
// No-observability-change: pure delegation, no new metric, span, log,
// status field, env var, queue, worker, lease, batch, runtime knob, or
// graph query. Operators keep diagnosing the auth family through the
// existing core-api query handler spans and /readyz probe.
package coreserve

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// CoreServeDeps carries everything NewCoreServeMux needs to build the
// serving mux. It is identical to query.CoreServeDeps: store handles come
// from the serving binary's own wiring, and secret-sealed material must
// never cross this package (the binary builds CoreAuthWiring itself).
type CoreServeDeps = query.CoreServeDeps

// CoreAuthWiring bundles the binary-built auth-family handlers. It is
// identical to query.CoreAuthWiring: every field is optional and a nil
// entry mounts an empty fail-closed handler for its routes.
type CoreAuthWiring = query.CoreAuthWiring

// NewCoreServeMux builds the production serving mux: full handler wiring,
// the backend-gated /readyz probe, and the auth chain. It delegates to
// query.NewCoreServeMux, so the served surface is byte-for-byte the core
// surface; the returned handler answers the whole frozen served-method
// set from stores directly and never dials another API backend.
func NewCoreServeMux(deps CoreServeDeps) (http.Handler, error) {
	return query.NewCoreServeMux(deps)
}
