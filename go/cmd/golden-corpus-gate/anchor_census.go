// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
)

// residualExplainer is the optional second read a failing census makes: the
// label sets of the unreachable nodes, so the CI log names the shape.
type residualExplainer interface {
	ResidualLabelSets(ctx context.Context) (string, error)
}

// checkAnchorCensus is the corpus-scale census half of the writer-coverage
// gate (#7212). After the replay, on the Neo4j leg, no node with an id may be
// unreachable by the labeled entity-context anchor. The check is required and
// corpus-size independent. Off Neo4j it records a non-required skip: the
// NornicDB loop keeps the unlabeled fallback, so the invariant is not its
// contract.
func checkAnchorCensus(ctx context.Context, source anchor.CensusSource, neo4jLeg bool, r *Report) {
	if !neo4jLeg {
		r.AddCheck("graph", "anchor_census", true, false,
			"skipped: the labeled anchor is a Neo4j-only statement; this leg is not Neo4j")
		return
	}
	verdict := anchor.EvaluateCensus(ctx, source)
	detail := verdict.Detail
	if !verdict.OK {
		if explainer, ok := source.(residualExplainer); ok {
			if sets, err := explainer.ResidualLabelSets(ctx); err == nil && sets != "" {
				detail += "; unreachable label sets: " + sets
			}
		}
	}
	r.AddCheck("graph", "anchor_census", verdict.OK, true, detail)
}

// AnchorCensus implements anchor.CensusSource over the gate's Bolt driver.
func (b *boltGraphCounter) AnchorCensus(ctx context.Context) (anchor.Census, error) {
	result, err := neo4j.ExecuteQuery(ctx, b.driver, anchor.CensusCypher, anchor.CensusParameters(),
		neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return anchor.Census{}, fmt.Errorf("execute anchor census: %w", err)
	}
	if len(result.Records) != 1 {
		return anchor.Census{}, fmt.Errorf("anchor census returned %d rows, want 1", len(result.Records))
	}
	return anchor.ParseCensusRow(result.Records[0].AsMap())
}

// ResidualLabelSets implements residualExplainer: at most 20 label sets with
// their node counts, never an id.
func (b *boltGraphCounter) ResidualLabelSets(ctx context.Context) (string, error) {
	result, err := neo4j.ExecuteQuery(ctx, b.driver, anchor.ResidualByLabelsCypher, anchor.CensusParameters(),
		neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return "", fmt.Errorf("execute anchor census breakdown: %w", err)
	}
	parts := make([]string, 0, len(result.Records))
	for _, record := range result.Records {
		labels, _ := record.Get("labels")
		nodes, _ := record.Get("nodes")
		var names []string
		if list, ok := labels.([]any); ok {
			for _, item := range list {
				if name, ok := item.(string); ok {
					names = append(names, name)
				}
			}
		}
		parts = append(parts, fmt.Sprintf("labels=[%s] nodes=%v", strings.Join(names, ":"), nodes))
	}
	return strings.Join(parts, "; "), nil
}
