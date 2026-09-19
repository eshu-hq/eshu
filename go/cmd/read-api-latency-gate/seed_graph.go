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

// seedLabel runs one UNWIND CREATE for label. The label name is interpolated
// into the Cypher text (Cypher labels cannot be bind parameters); label
// always comes from the fixed infraLabels slice, never from external input,
// so this carries no injection risk.
func seedLabel(ctx context.Context, driver neo4j.DriverWithContext, database, label string, count int) error {
	cypher := fmt.Sprintf(
		`UNWIND range(0, $count - 1) AS i
		 CREATE (n:%s {
		   id: $label + '-seed-' + toString(i),
		   provider: CASE i %% 3 WHEN 0 THEN 'aws' WHEN 1 THEN 'gcp' ELSE 'azure' END,
		   environment: CASE i %% 2 WHEN 0 THEN 'production' ELSE 'staging' END,
		   source_system: $label
		 })`,
		label,
	)

	session := driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, map[string]any{"count": count, "label": label})
	if err != nil {
		return fmt.Errorf("run seed cypher: %w\ncypher=%s", err, cypher)
	}
	if _, err := result.Consume(ctx); err != nil {
		return fmt.Errorf("consume seed result: %w", err)
	}
	return nil
}
