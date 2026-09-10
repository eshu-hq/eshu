// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships_test

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers the relationship capability rows the moved HTTP tests
// drive through. Production registers them from root package query's
// contract_capability_matrix.go init(), which this package's test binary
// does not import (root imports the leaves, so that would be an import
// cycle). Without a row the handler panics in BuildTruthEnvelope. The
// values mirror those matrix entries; separate ceiling variables follow
// querycontract's aliasing note.
func TestMain(m *testing.M) {
	derived := querycontract.TruthLevelDerived
	exact := querycontract.TruthLevelExact
	direct := querycontract.CapabilitySupport{
		LocalLightweightMax:   &derived,
		LocalAuthoritativeMax: &exact,
		LocalFullStackMax:     &exact,
		ProductionMax:         &exact,
	}
	for _, capability := range []string{
		"call_graph.direct_callers",
		"call_graph.direct_callees",
		"symbol_graph.imports",
		"symbol_graph.inheritance",
	} {
		querycontract.SetCapabilitySupport(capability, direct)
	}
	transitive := querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &exact,
		LocalFullStackMax:     &exact,
		ProductionMax:         &exact,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	}
	for _, capability := range []string{
		"call_graph.transitive_callers",
		"call_graph.transitive_callees",
	} {
		querycontract.SetCapabilitySupport(capability, transitive)
	}
	os.Exit(m.Run())
}
