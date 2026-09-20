// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// infraLabels lists the graph node labels seeded to make the #6793 per-label
// aggregate cost visible (go/internal/query/infra_resource_aggregates.go's
// allInfraLabels / querycontract.AllInfraLabels taxonomy).
var infraLabels = []string{
	"TerraformResource",
	"TerraformStateResource",
	"K8sResource",
	"CloudResource",
	"CloudFormationResource",
	"ArgoCDApplication",
	"CrossplaneXRD",
	"HelmChart",
}

// SeedGraphOptions configures SeedGraph.
type SeedGraphOptions struct {
	URI          string
	Username     string
	Password     string
	DatabaseName string
	// NodesPerLabel is the synthetic node count created for each infra label.
	NodesPerLabel int
}

// SeedGraph bulk-creates opts.NodesPerLabel synthetic nodes per infraLabels
// entry, one UNWIND CREATE per label, so the infra resource aggregate
// (#6793) has enough infra-labeled nodes for its per-label MATCH cost to be
// measurable. Node properties (provider, environment) are populated so the
// aggregate's by-provider/by-environment grouping has real buckets to walk,
// not an all-null degenerate case.
func SeedGraph(ctx context.Context, opts SeedGraphOptions) error {
	auth := neo4j.NoAuth()
	if opts.Username != "" {
		auth = neo4j.BasicAuth(opts.Username, opts.Password, "")
	}
	driver, err := neo4j.NewDriverWithContext(opts.URI, auth)
	if err != nil {
		return fmt.Errorf("open graph driver: %w", err)
	}
	defer func() { _ = driver.Close(ctx) }()

	for _, label := range infraLabels {
		if err := seedLabel(ctx, driver, opts.DatabaseName, label, opts.NodesPerLabel); err != nil {
			return fmt.Errorf("seed label %s: %w", label, err)
		}
	}
	return nil
}

// iacGraphSeedBatchSize bounds the UNWIND parameter list per write. NornicDB's
// cost per row grows with batch size on a label carrying a uid UNIQUE
// constraint: measured on a fresh scratch NornicDB v1.3.3 writing 50,000
// TerraformResource rows over 150,000 pre-existing anonymous nodes, batches of
// 250 took 6.6s, 1,000 took 14.3s, and 5,000 took 91.7s; one unbatched 50k-row
// UNWIND stalled a live run for 15+ minutes (issue #6797). 250 sits in the
// near-linear region.
const iacGraphSeedBatchSize = 250

// SeedIaCGraphNodes bulk-creates graph nodes correlated by uid with facts'
// EntityID (via buildIaCGraphNodeRows), one UNWIND CREATE per label, so
// go/internal/query/iac/resources.go's Postgres-then-graph hydration can find
// a matching row for every Postgres-selected IaC inventory candidate SeedGraph
// (or SeedIaCFacts.entity_id) already wrote. This is additional to, not a
// replacement for, SeedGraph's anonymous bulk infraLabels nodes.
func SeedIaCGraphNodes(ctx context.Context, opts SeedGraphOptions, facts []SeedIaCFact) error {
	auth := neo4j.NoAuth()
	if opts.Username != "" {
		auth = neo4j.BasicAuth(opts.Username, opts.Password, "")
	}
	driver, err := neo4j.NewDriverWithContext(opts.URI, auth)
	if err != nil {
		return fmt.Errorf("open graph driver: %w", err)
	}
	defer func() { _ = driver.Close(ctx) }()

	byLabel := groupIaCGraphNodeRowsByLabel(buildIaCGraphNodeRows(facts))
	for _, label := range iacEntityTypes {
		for _, batch := range batchIaCGraphNodeRows(byLabel[label], iacGraphSeedBatchSize) {
			if err := seedIaCGraphLabelNodes(ctx, driver, opts.DatabaseName, label, batch); err != nil {
				return fmt.Errorf("seed IaC graph nodes for label %s: %w", label, err)
			}
		}
	}
	return nil
}

// seedIaCGraphLabelNodes runs one UNWIND CREATE for all of rows' nodes under
// label. label always comes from iacEntityTypes (via SeedIaCFact.EntityType),
// never external input, so interpolating it into the Cypher text carries no
// injection risk (same reasoning as seedLabel's doc comment).
func seedIaCGraphLabelNodes(ctx context.Context, driver neo4j.DriverWithContext, database, label string, rows []IaCGraphNodeRow) error {
	params := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		params = append(params, map[string]any{
			"uid":           r.UID,
			"id":            r.ID,
			"name":          r.Name,
			"generation_id": r.GenerationID,
			"provider":      r.Provider,
			"resource_type": r.ResourceType,
		})
	}

	cypher := fmt.Sprintf(
		`UNWIND $rows AS row
		 CREATE (n:%s {
		   uid: row.uid,
		   id: row.id,
		   name: row.name,
		   generation_id: row.generation_id,
		   provider: row.provider,
		   resource_type: row.resource_type
		 })`,
		label,
	)

	session := driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, map[string]any{"rows": params})
	if err != nil {
		return fmt.Errorf("run seed cypher: %w\ncypher=%s", err, cypher)
	}
	if _, err := result.Consume(ctx); err != nil {
		return fmt.Errorf("consume seed result: %w", err)
	}
	return nil
}

// seedLabel creates count anonymous nodes for label, one bounded UNWIND CREATE
// per bulkNodeRanges batch over precomputed parameter rows (infraNodeRows). The
// label name is interpolated into the Cypher text (Cypher labels cannot be bind
// parameters); label always comes from the fixed infraLabels slice, never from
// external input, so this carries no injection risk. Both the range bound and
// the property values are computed in Go: see bulkNodeRanges and infraNodeRows
// for the two NornicDB behaviors that make Cypher-side computation wrong.
func seedLabel(ctx context.Context, driver neo4j.DriverWithContext, database, label string, count int) error {
	props := `id: row.id,
		   provider: row.provider,
		   environment: row.environment,
		   source_system: row.source_system`
	batch := bulkGraphSeedBatchSize
	if infraLabelNeedsIdentity(label) {
		props += `,
		   uid: row.uid,
		   resource_type: row.resource_type,
		   source_fact_id: row.source_fact_id`
		batch = iacGraphSeedBatchSize
	}
	cypher := fmt.Sprintf("UNWIND $rows AS row\n\t\t CREATE (n:%s {\n\t\t   %s\n\t\t })", label, props)

	for _, r := range bulkNodeRanges(count, batch) {
		if err := runSeedBatch(ctx, driver, database, cypher, map[string]any{"rows": infraNodeRows(label, r)}); err != nil {
			return fmt.Errorf("nodes %d..%d: %w", r.First, r.Last, err)
		}
	}
	return nil
}

// runSeedBatch runs one write statement to completion in its own session.
func runSeedBatch(ctx context.Context, driver neo4j.DriverWithContext, database, cypher string, params map[string]any) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("run seed cypher: %w\ncypher=%s", err, cypher)
	}
	if _, err := result.Consume(ctx); err != nil {
		return fmt.Errorf("consume seed result: %w", err)
	}
	return nil
}
