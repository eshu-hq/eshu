// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// bootstrapTerraformStateConfigMatchCandidateCountCypher counts, per batch
// row, how many TerraformResource nodes match its (repo_id, name) pair.
// Identical to cmd/projector's and cmd/ingester's
// *TerraformStateConfigMatchCandidateCountCypher: single anchoring clause
// (UNWIND -> MATCH -> RETURN, no intervening WITH, OPTIONAL MATCH, or second
// MATCH) -- the safe read shape documented in
// docs/public/reference/nornicdb-query-pitfalls.md's "Multi-Clause Read
// Queries Silently Corrupt The Projection". See
// cmd/projector/terraform_state_config_match.go for the NornicDB probe notes
// this shape was validated against.
const bootstrapTerraformStateConfigMatchCandidateCountCypher = `UNWIND $rows AS row
MATCH (c:TerraformResource {repo_id: row.owning_repo_id, name: row.address})
RETURN row.uid AS uid, count(c) AS candidate_count`

// bootstrapTerraformStateConfigMatchResolver adapts a read-only Bolt session
// to sourcecypher.TerraformStateConfigMatchResolver (#5443), so
// bootstrap-index's canonical writer can fail closed on an ambiguous
// MATCHES_STATE candidate instead of silently fanning an edge out to every
// TerraformResource node sharing a (repo_id, name) pair -- no uniqueness
// constraint backs that pair; tf_resource_unique is (name, path,
// line_number). Mirrors cmd/projector/terraform_state_config_match.go's
// projectorTerraformStateConfigMatchResolver and
// cmd/ingester/terraform_state_config_match.go's
// ingesterTerraformStateConfigMatchResolver.
type bootstrapTerraformStateConfigMatchResolver struct {
	driver       neo4jdriver.DriverWithContext
	databaseName string
}

// CountConfigMatchCandidates implements
// sourcecypher.TerraformStateConfigMatchResolver. Runs the whole batch as
// one read-only Bolt statement
// (see bootstrapTerraformStateConfigMatchCandidateCountCypher); a query
// failure returns the error unchanged so the caller fails closed for every
// row in the batch rather than guess.
func (r bootstrapTerraformStateConfigMatchResolver) CountConfigMatchCandidates(
	ctx context.Context,
	queries []sourcecypher.TerraformStateConfigMatchQuery,
) (map[string]int, error) {
	if len(queries) == 0 {
		return nil, nil
	}
	rows := make([]map[string]any, len(queries))
	for i, q := range queries {
		rows[i] = map[string]any{
			"uid":            q.UID,
			"owning_repo_id": q.OwningRepoID,
			"address":        q.Address,
		}
	}

	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, bootstrapTerraformStateConfigMatchCandidateCountCypher, map[string]any{"rows": rows})
	if err != nil {
		return nil, fmt.Errorf("run terraform state config-match candidate count: %w", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect terraform state config-match candidate count: %w", err)
	}

	counts := make(map[string]int, len(records))
	for _, record := range records {
		uidValue, _ := record.Get("uid")
		countValue, _ := record.Get("candidate_count")
		uid, ok := uidValue.(string)
		if !ok || uid == "" {
			continue
		}
		count, err := bootstrapTerraformStateConfigMatchCandidateCountAsInt(countValue)
		if err != nil {
			return nil, fmt.Errorf("terraform state config-match candidate count for uid=%q: %w", uid, err)
		}
		counts[uid] = count
	}
	return counts, nil
}

// bootstrapTerraformStateConfigMatchCandidateCountAsInt normalizes a
// Bolt-decoded count() result to int. The Neo4j Go driver decodes a Cypher
// integer as int64; this also accepts float64 defensively, matching this
// repository's existing bolt-count-decode precedent (internal/storage/cypher's
// boltCount test helper).
func bootstrapTerraformStateConfigMatchCandidateCountAsInt(value any) (int, error) {
	switch v := value.(type) {
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("unexpected candidate_count type %T", value)
	}
}
