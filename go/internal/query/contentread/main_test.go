// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's capability with querycontract before any
// test runs, then runs the suite.
//
// In production this capability is registered by root package query's init()
// functions (contract_capability_matrix.go's baseCapabilityMatrix entry for
// code_search.content_search). Root always links into the production binary
// (it owns the router), so those init() functions always run there and
// production is unaffected by this file.
//
// `go test ./internal/query/contentread` never links root package query:
// contentread cannot import it without an import cycle (root's
// content_read_alias.go already imports contentread for the ContentHandler
// compatibility alias, #6060), so root's init() functions never run in this
// test binary. Without this TestMain, every handler test in this package
// panics in BuildTruthEnvelope with a missing-capability error -- not
// because the handler is broken, but because no capability was ever
// registered for it to check against. The value below is copied faithfully
// from the root file named above; it must be kept in sync if that entry
// changes.
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
func TestMain(m *testing.M) {
	derived := querycontract.TruthLevelDerived
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{
			Capability: "code_search.content_search",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   &derived,
				LocalAuthoritativeMax: &derived,
				LocalFullStackMax:     &derived,
				ProductionMax:         &derived,
			},
		},
	)
	os.Exit(m.Run())
}
