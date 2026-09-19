// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// expectedGraphNodeCounts is the per-label node count a correctly seeded graph
// holds: nodesPerLabel anonymous nodes on every infraLabels entry, plus one
// correlated node per SeedIaCFact of that entity type (see SeedIaCGraphNodes).
func expectedGraphNodeCounts(nodesPerLabel int, facts []SeedIaCFact) map[string]int {
	expected := make(map[string]int, len(infraLabels)+len(iacEntityTypes))
	for _, label := range infraLabels {
		expected[label] = nodesPerLabel
	}
	for _, f := range facts {
		expected[f.EntityType]++
	}
	return expected
}

// graphCountMismatches returns one sorted "label: got X, want Y" line for every
// label whose actual count differs from expected.
func graphCountMismatches(expected, actual map[string]int) []string {
	var mismatches []string
	for label, want := range expected {
		if got := actual[label]; got != want {
			mismatches = append(mismatches, fmt.Sprintf("%s: got %d, want %d", label, got, want))
		}
	}
	sort.Strings(mismatches)
	return mismatches
}

// VerifyGraphNodeCounts counts the nodes under every label in expected and
// fails when any count differs. A seed that silently under-creates is a
// vacuous corpus: the bulk infra seed held one node per label instead of
// 150,000 for as long as nothing checked (issue #6797), so the latency numbers
// it produced said nothing about graph scale.
func VerifyGraphNodeCounts(ctx context.Context, opts SeedGraphOptions, expected map[string]int) error {
	auth := neo4j.NoAuth()
	if opts.Username != "" {
		auth = neo4j.BasicAuth(opts.Username, opts.Password, "")
	}
	driver, err := neo4j.NewDriverWithContext(opts.URI, auth)
	if err != nil {
		return fmt.Errorf("open graph driver: %w", err)
	}
	defer func() { _ = driver.Close(ctx) }()

	actual := make(map[string]int, len(expected))
	for label := range expected {
		// label comes from infraLabels/iacEntityTypes, never external input.
		count, err := countLabel(ctx, driver, opts.DatabaseName, label)
		if err != nil {
			return fmt.Errorf("count %s: %w", label, err)
		}
		actual[label] = count
	}
	if mismatches := graphCountMismatches(expected, actual); len(mismatches) > 0 {
		return fmt.Errorf("seeded graph does not hold the expected node counts: %s", strings.Join(mismatches, "; "))
	}
	return nil
}

func countLabel(ctx context.Context, driver neo4j.DriverWithContext, database, label string) (int, error) {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, fmt.Sprintf("MATCH (n:%s) RETURN count(n) AS c", label), nil)
	if err != nil {
		return 0, err
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, err
	}
	value, ok := record.Get("c")
	if !ok {
		return 0, fmt.Errorf("count query returned no column c")
	}
	count, ok := value.(int64)
	if !ok {
		return 0, fmt.Errorf("count query returned %T, want int64", value)
	}
	return int(count), nil
}
