// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
)

// TestMain registers this family's capabilities with querycontract before any
// test runs, then runs the suite.
//
// In production the registration comes from root package query's
// baseCapabilityMatrix (contract_capability_matrix.go), which references this
// package's ExactSymbolSupport/FuzzySymbolSupport declarations and the
// repository family's ContextOverviewSupport. Root always links into the
// production binary because it owns the router, so that init() always runs
// there and this file changes nothing about production.
//
// `go test ./internal/query/entity` never links root package query: this
// package cannot import it without an import cycle (root's entity_alias.go
// already imports this package for the EntityHandler compatibility alias,
// #6060), so root's init() functions never run in this test binary. Without
// this TestMain every resolve test here fails with the capability gate's
// missing-matrix panic -- not because the handler is broken, but because no
// capability was ever registered for it to check against.
//
// It calls the family Support declarations — the same declarations root's
// baseCapabilityMatrix registers in production — rather than copies of their
// fields. See repository/main_test.go, the template this file copies, for
// the incident that rule was learned from.
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
func TestMain(m *testing.M) {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{Capability: ExactSymbolCapability, Support: ExactSymbolSupport()},
		querycontract.CapabilityRegistration{Capability: FuzzySymbolCapability, Support: FuzzySymbolSupport()},
		querycontract.CapabilityRegistration{Capability: repository.ContextOverviewCapability, Support: repository.ContextOverviewSupport()},
	)
	os.Exit(m.Run())
}
