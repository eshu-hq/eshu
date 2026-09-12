// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's capability with querycontract before any
// test runs, then runs the suite.
//
// In production the registration comes from root package query's init() in
// contract_work_item.go. Root always links into the production binary
// because it owns the router, so that init() always runs there and this file
// changes nothing about production.
//
// `go test ./internal/query/workitem` never links root package query: this
// package cannot import it without an import cycle (root's
// work_item_alias.go already imports this package for the Handler
// compatibility alias, #6642), so root's init() functions never run in this
// test binary. Without this TestMain every handler test here fails with the
// capability gate's unsupported_capability 501 -- not because the handler is
// broken, but because no capability was ever registered for it to check
// against.
//
// It calls EvidenceSupport from capability.go, rather than a copy of its
// fields. Root's contract_work_item.go still carries its own equal
// capabilitySupport literal for production until #6642 Part C adopts this
// function directly; TestWorkItemEvidenceCapabilityLockstep
// (go/internal/query/capability_lockstep_work_item_test.go) guards the two
// against drift in the meantime. See repository/main_test.go, the template
// this file copies.
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
func TestMain(m *testing.M) {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{Capability: EvidenceCapability, Support: EvidenceSupport()},
	)
	os.Exit(m.Run())
}
