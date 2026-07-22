// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// terraformStateConfigMatchCandidateCountCypher counts, per batch row, how
// many TerraformResource nodes match its (repo_id, name) pair. Single
// anchoring clause (UNWIND -> MATCH -> RETURN, no intervening WITH,
// OPTIONAL MATCH, or second MATCH) -- the safe read shape documented in
// docs/public/reference/nornicdb-query-pitfalls.md's "Multi-Clause Read
// Queries Silently Corrupt The Projection". A WITH+collect()+WHERE(size())
// variant of this same lookup was probed against the pinned NornicDB
// backend while building this fix and silently dropped every row: the
// property-dot-access on the post-aggregation grouping-key variable
// resolved to null, and the WHERE filter after the aggregating WITH dropped
// 100% of rows even in the single-candidate case. This shape -- aggregate
// directly in the RETURN of the SAME clause as the anchor MATCH, no
// intervening WITH -- was verified correct for both the ambiguous (count=2)
// and unique (count=1) case against the pinned NornicDB build; see
// TestProjectorTerraformStateConfigMatchResolverLive.
const terraformStateConfigMatchCandidateCountCypher = `UNWIND $rows AS row
MATCH (c:TerraformResource {repo_id: row.owning_repo_id, name: row.address})
RETURN row.uid AS uid, count(c) AS candidate_count`

// projectorTerraformStateConfigMatchResolver adapts a read-only Bolt session
// to sourcecypher.TerraformStateConfigMatchResolver (#5443 P1 review
// finding), so the canonical writer can fail closed on an ambiguous
// MATCHES_STATE candidate instead of silently fanning an edge out to every
// TerraformResource node sharing a (repo_id, name) pair -- no uniqueness
// constraint backs that pair; tf_resource_unique is (name, path,
// line_number).
type projectorTerraformStateConfigMatchResolver struct {
	driver       neo4jdriver.DriverWithContext
	databaseName string
}

// CountConfigMatchCandidates implements
// sourcecypher.TerraformStateConfigMatchResolver. Runs the whole batch as
// one read-only Bolt statement (see terraformStateConfigMatchCandidateCountCypher);
// a query failure returns the error unchanged so the caller fails closed
// for every row in the batch rather than guess.
func (r projectorTerraformStateConfigMatchResolver) CountConfigMatchCandidates(
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

	result, err := session.Run(ctx, terraformStateConfigMatchCandidateCountCypher, map[string]any{"rows": rows})
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
		count, err := terraformStateConfigMatchCandidateCountAsInt(countValue)
		if err != nil {
			return nil, fmt.Errorf("terraform state config-match candidate count for uid=%q: %w", uid, err)
		}
		counts[uid] = count
	}
	return counts, nil
}

// terraformStateConfigMatchCandidateCountAsInt normalizes a Bolt-decoded
// count() result to int. The Neo4j Go driver decodes a Cypher integer as
// int64; this also accepts float64 defensively, matching this repository's
// existing bolt-count-decode precedent (internal/storage/cypher's
// boltCount test helper).
func terraformStateConfigMatchCandidateCountAsInt(value any) (int, error) {
	switch v := value.(type) {
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("unexpected candidate_count type %T", value)
	}
}
