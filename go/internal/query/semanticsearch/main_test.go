// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's capability with querycontract before any
// test runs, then runs the suite.
//
// In production the registration comes from root package query's
// baseCapabilityMatrix (contract_capability_matrix.go). Root always links into
// the production binary because it owns the router, so that init() always runs
// there and this file changes nothing about production.
//
// `go test ./internal/query/semanticsearch` never links root package query:
// this package cannot import it without an import cycle (root's
// semantic_search_alias.go already imports this package for the
// SemanticSearchHandler compatibility alias, #6060), so root's init()
// functions never run in this test binary. Without this TestMain every handler
// test here fails with the capability gate's unsupported_capability 501 -- not
// because the handler is broken, but because no capability was ever registered
// for it to check against.
//
// It calls Support() from capability.go — the same declaration root's
// baseCapabilityMatrix registers in production — rather than a copy of its five
// fields. The copy is what the earlier version of this file had, under a
// comment asking the next editor to keep it in sync; nothing enforced that, and
// flipping two of the fields left this package's tests green against a profile
// production no longer served.
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
func TestMain(m *testing.M) {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{Capability: Capability, Support: Support()},
	)
	os.Exit(m.Run())
}
