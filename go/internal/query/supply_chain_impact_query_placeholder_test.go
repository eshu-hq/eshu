// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// supplyChainImpactPlaceholderPattern matches a Postgres positional parameter.
// The supply-chain SQL family has no dollar-quoted string literals, so every
// "$" followed by digits in these queries is a bind placeholder.
var supplyChainImpactPlaceholderPattern = regexp.MustCompile(`\$(\d+)`)

// TestSupplyChainImpactQueryPlaceholdersMatchBoundArguments is the
// credential-free guard for the whole supply-chain impact read family: for
// every query, the highest $N in the SQL must equal the number of arguments
// its production builder binds, and $1..$N must all be present.
//
// This exists because the same defect can fail two ways. Too few arguments
// makes Postgres reject the statement ("got 23 parameters but the statement
// requires 24"), so that route simply cannot serve traffic. Binding the right
// count in the wrong order is worse and silent: every value lands one slot off,
// predicates compare against values meant for other predicates, and the query
// returns rows an operator has no way to tell are wrong.
//
// The live plan proof for these queries only runs when ESHU_POSTGRES_TEST_DSN
// is set, so it does not run in CI. Before this test, a placeholder added
// without its bind could sit on main until someone happened to run the live
// suite by hand — which is how it got there.
func TestSupplyChainImpactQueryPlaceholdersMatchBoundArguments(t *testing.T) {
	t.Parallel()

	readAt := time.Date(2026, time.August, 12, 15, 4, 5, 0, time.UTC)
	listFilter := SupplyChainImpactFindingFilter{CVEID: "CVE-0000-0000", Limit: 10}
	aggregateFilter := SupplyChainImpactAggregateFilter{CVEID: "CVE-0000-0000"}
	explainFilter := SupplyChainImpactExplanationFilter{CVEID: "CVE-0000-0000", PackageID: "pkg"}

	for name, tc := range map[string]struct {
		query string
		args  []any
	}{
		"list direct": {
			query: listSupplyChainImpactFindingsQuery,
			args:  supplyChainImpactFindingListArgs(listFilter, readAt),
		},
		"list materialized": {
			query: listSupplyChainImpactFindingsFromWinnersQuery,
			args:  supplyChainImpactFindingListArgs(listFilter, readAt),
		},
		"aggregate count": {
			query: supplyChainImpactAggregateCountQuery,
			args:  supplyChainImpactAggregateArgs(aggregateFilter, readAt),
		},
		"aggregate priority buckets": {
			query: supplyChainImpactAggregatePriorityCountQuery,
			args:  supplyChainImpactAggregateArgs(aggregateFilter, readAt),
		},
		"aggregate severity buckets": {
			query: supplyChainImpactAggregateSeverityCountQuery,
			args:  supplyChainImpactAggregateArgs(aggregateFilter, readAt),
		},
		"inventory": {
			query: supplyChainImpactInventoryQuery(
				mustSupplyChainImpactInventoryGroupExpression(t, SupplyChainImpactInventoryByImpactStatus),
			),
			args: supplyChainImpactInventoryArgs(aggregateFilter, readAt, 10, 0),
		},
		"explain": {
			query: explainSupplyChainImpactFindingQuery,
			args:  supplyChainImpactExplanationQueryArgs(explainFilter, func() time.Time { return readAt }),
		},
		"explain by public id": {
			query: explainSupplyChainImpactFindingByPublicIDQuery,
			args:  supplyChainImpactExplanationQueryArgs(explainFilter, func() time.Time { return readAt }),
		},
		"runtime context": {
			query: selectSupplyChainImpactRuntimeContextQuery,
			args:  supplyChainImpactRuntimeContextArgs(nil, nil, nil),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := supplyChainImpactPlaceholderCoverage(tc.query, len(tc.args)); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		})
	}
}

// TestSupplyChainImpactPlaceholderCoverageDetectsSkew proves the guard above
// actually catches the defect it claims to, rather than passing vacuously. It
// runs the same check used by the table test against the real shipped queries,
// once with an argument dropped (the shape that broke the live plan proof) and
// once with an argument too many.
func TestSupplyChainImpactPlaceholderCoverageDetectsSkew(t *testing.T) {
	t.Parallel()

	bound := len(supplyChainImpactFindingListArgs(
		SupplyChainImpactFindingFilter{CVEID: "CVE-0000-0000", Limit: 10},
		time.Date(2026, time.August, 12, 15, 4, 5, 0, time.UTC),
	))
	for name, argCount := range map[string]int{
		"one bind dropped": bound - 1,
		"one bind extra":   bound + 1,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := supplyChainImpactPlaceholderCoverage(
				listSupplyChainImpactFindingsQuery,
				argCount,
			); err == nil {
				t.Fatalf(
					"guard accepted %d arguments for a query the store binds %d for; it would not catch %s",
					argCount,
					bound,
					name,
				)
			}
		})
	}
}

// supplyChainImpactPlaceholderCoverage reports whether query uses exactly
// $1..$argCount, with no gaps and nothing above argCount.
func supplyChainImpactPlaceholderCoverage(query string, argCount int) error {
	seen := make(map[int]struct{}, argCount)
	highest := 0
	for _, match := range supplyChainImpactPlaceholderPattern.FindAllStringSubmatch(query, -1) {
		position, err := strconv.Atoi(match[1])
		if err != nil {
			return fmt.Errorf("parse placeholder %q: %w", match[0], err)
		}
		seen[position] = struct{}{}
		if position > highest {
			highest = position
		}
	}

	if highest != argCount {
		return fmt.Errorf(
			"query uses placeholders up to $%d but the caller binds %d arguments",
			highest,
			argCount,
		)
	}
	missing := make([]string, 0, argCount)
	for position := 1; position <= argCount; position++ {
		if _, ok := seen[position]; !ok {
			missing = append(missing, fmt.Sprintf("$%d", position))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("query binds %d arguments but never references %v", argCount, missing)
	}
	return nil
}
