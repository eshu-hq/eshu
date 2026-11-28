// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package coreserve is the versioned shared auth serving library
// (eshu-hq/eshu#11618). See doc.go for the contract.
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
