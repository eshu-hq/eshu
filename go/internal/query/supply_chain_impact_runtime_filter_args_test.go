// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"regexp"
	"strconv"
	"testing"
)

var placeholderPattern = regexp.MustCompile(`\$(\d+)`)

// highestPlaceholder returns the largest $N in query, which is the number of
// arguments the statement binds.
func highestPlaceholder(t *testing.T, query string) int {
	t.Helper()
	highest := 0
	for _, match := range placeholderPattern.FindAllStringSubmatch(query, -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse placeholder %q: %v", match[0], err)
		}
		if n > highest {
			highest = n
		}
	}
	if highest == 0 {
		t.Fatal("no $N placeholders found; the query text or this pattern changed")
	}
	return highest
}

// TestSupplyChainRuntimeFilterListArgsMatchQueryPlaceholders keeps the EXPLAIN
// proof's argument list in step with the queries it binds.
//
// supplyChainRuntimeFilterListArgs drifted once already: a $24::timestamptz was
// added for suppression-expiry evaluation and the helper kept building 23. The
// only thing that caught it was TestSupplyChainImpactRuntimeFilterPlansLive,
// which skips without ESHU_POSTGRES_TEST_DSN and so never runs in ordinary CI --
// and when it did run it failed at bind time with a parameter-count message,
// before producing a plan, so every index assertion it exists for was skipped.
// A test that fails for the wrong reason still looks like signal while hiding
// the absence of its own coverage.
//
// The expected count is DERIVED from the query rather than written here as a
// constant. A hardcoded number only catches the case where someone updates the
// query and the production args but forgets this helper; it passes happily if
// they forget the constant too, which is the same drift one level up. Reading
// the highest $N means adding a placeholder anywhere fails this test until the
// helper is updated with it.
//
// Both queries are checked because they take the SAME slice
// (supply_chain_impact_findings_queries.go:129), so a placeholder added to
// either one alone is a real defect.
func TestSupplyChainRuntimeFilterListArgsMatchQueryPlaceholders(t *testing.T) {
	t.Parallel()

	args := supplyChainRuntimeFilterListArgs(SupplyChainImpactFindingFilter{})

	for name, query := range map[string]string{
		"list direct":       listSupplyChainImpactFindingsQuery,
		"list materialized": listSupplyChainImpactFindingsFromWinnersQuery,
	} {
		t.Run(name, func(t *testing.T) {
			want := highestPlaceholder(t, query)
			if len(args) != want {
				t.Fatalf(
					"supplyChainRuntimeFilterListArgs binds %d arguments, but %s uses $%d; "+
						"a placeholder was added to the query without adding its argument here, "+
						"which fails the live plan proof at bind time before any plan is produced",
					len(args), name, want,
				)
			}
		})
	}
}
