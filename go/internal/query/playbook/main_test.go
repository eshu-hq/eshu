// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbook

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's capability with querycontract before any
// test runs, then runs the suite.
//
// In production the registration comes from the Part C leaf query/contract's
// init() in capability_matrix.go, keyed by CapabilityQueryPlaybooks. The
// production binary always links that leaf through root, which owns the
// router, so that init() always runs there and this file changes nothing
// about production.
//
// `go test ./internal/query/playbook` never links root package query: this
// package cannot import it without an import cycle (root's
// query_playbook_alias.go already imports this package for the Handler
// compatibility alias, #6642), so root's init() functions never run in this
// test binary. Without this TestMain, TestQueryPlaybookHandler* fails with
// BuildTruthEnvelope's "query capability \"query.playbooks\" missing from
// capability matrix" panic -- not because the handler is broken, but because
// no capability was ever registered for it to check against.
//
// It calls Support from capabilities.go, rather than a copy of its fields.
// The contract leaf's capability_matrix.go still carries its own equal
// capabilitySupport literal for production until a follow-up lane adopts
// this function directly; TestQueryPlaybookCapabilityLockstep
// (go/internal/query/capability_lockstep_query_playbook_test.go) guards the
// two against drift in the meantime. See workitem/main_test.go, the template
// this file copies.
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
func TestMain(m *testing.M) {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{Capability: Capability, Support: Support()},
	)
	os.Exit(m.Run())
}
