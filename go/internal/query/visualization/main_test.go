// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's capability with querycontract before any
// test runs, then runs the suite.
//
// In production the capability is registered by root package query's
// contract_capability_matrix.go. Root always links into the production
// binary (it owns the router), so production is unaffected by this file.
//
// `go test ./internal/query/visualization` never links root package query:
// this package cannot import it without an import cycle (root's
// visualization_alias.go imports this package for the compatibility
// aliases, #6642), so root's registration never runs in this test binary.
// Without this TestMain, Handler.derive panics in the truth-envelope builder
// because no capability was ever registered for it to look up.
//
// It registers through PacketDerivationSupport in capability.go, never a
// copy of its fields; the root lockstep test asserts root's row agrees with
// that constructor field by field.
func TestMain(m *testing.M) {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{
			Capability: PacketDerivationCapability,
			Support:    PacketDerivationSupport(),
		},
	)
	os.Exit(m.Run())
}
