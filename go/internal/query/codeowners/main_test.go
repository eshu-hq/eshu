// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's capability with querycontract before any
// test runs, then runs the suite.
//
// In production this capability is registered by root package query's init()
// in contract_capability_matrix_ext.go. Root always links into the production
// binary (it owns the router), so that init() always runs there and
// production is unaffected by this file.
//
// `go test ./internal/query/codeowners` never links root package query:
// this package cannot import it without an import cycle (root's
// family_codeowners_shim.go already imports this package for the
// CodeownersOwnershipHandler compatibility alias, #6060), so root's init()
// functions never run in this test binary. Without this TestMain, every
// handler test in this package fails with the capability gate's
// unsupported_capability 501 -- not because the handler is broken, but
// because no capability was ever registered for it to check against.
//
// It registers through OwnershipSupport in capabilities.go -- the same
// constructor a follow-up lane should point root's row at -- never a copy
// of its fields. A copied row is what the packagereg TestMain still
// carries, under a comment asking the next editor to keep it in sync;
// nothing enforces that, and the semanticsearch TestMain already had to
// relearn the lesson when two fields flipped while its tests stayed green
// against a profile production no longer served.
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
func TestMain(m *testing.M) {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{
			Capability: codeownersOwnershipCapability,
			Support:    OwnershipSupport(),
		},
	)
	os.Exit(m.Run())
}
