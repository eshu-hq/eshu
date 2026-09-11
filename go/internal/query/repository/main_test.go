// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's capabilities with querycontract before any
// test runs, then runs the suite.
//
// In production the registration comes from root package query's
// baseCapabilityMatrix (contract_capability_matrix.go). Root always links into
// the production binary because it owns the router, so that init() always runs
// there and this file changes nothing about production.
//
// `go test ./internal/query/repository` never links root package query:
// this package cannot import it without an import cycle (root's
// repository_alias.go already imports this package for the Handler
// compatibility alias, #6060), so root's init() functions never run in this
// test binary. Without this TestMain every handler test here fails with the
// capability gate's unsupported_capability 501 -- not because the handler is
// broken, but because no capability was ever registered for it to check
// against.
//
// It calls ContextOverviewSupport and CatalogSupport from capability.go — the
// same declarations root's baseCapabilityMatrix registers in production —
// rather than copies of their fields. See semanticsearch/main_test.go, the
// template this file copies, for the incident that rule was learned from.
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
//
// code_search.content_search is deliberately NOT registered here. The
// repository content route serves its success envelope under that code-family
// capability, but the code family has not moved yet (lane A owns the code_*
// families), so there is no family declaration to reference and no
// copy-the-row-from-root-matrix fallback: a copy would drift silently. Tests
// that drive the content success envelope stay in root package query, where
// the full production matrix is linked.
func TestMain(m *testing.M) {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{Capability: ContextOverviewCapability, Support: ContextOverviewSupport()},
		querycontract.CapabilityRegistration{Capability: CatalogCapability, Support: CatalogSupport()},
	)
	os.Exit(m.Run())
}
