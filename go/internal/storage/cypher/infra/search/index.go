// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// indexStateCypher reads the state of the infra search key index; no row
// means the index does not exist.
const indexStateCypher = "SHOW INDEXES YIELD name, state WHERE name = $name RETURN state"

// IndexState returns the Neo4j state of graph.InfraSearchKeyIndexName
// (ONLINE, POPULATING, FAILED, ...), or "" when the index does not exist.
// The read cutover (#7245 PR-2) trusts the index only while it is ONLINE.
// Neo4j only.
func IndexState(ctx context.Context, runner Runner) (string, error) {
	rows, err := runner.Run(ctx, indexStateCypher, map[string]any{"name": graph.InfraSearchKeyIndexName})
	if err != nil {
		return "", fmt.Errorf("read %s state: %w", graph.InfraSearchKeyIndexName, err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	state, _ := rows[0]["state"].(string)
	return state, nil
}

// CountUnkeyedSearchable returns, per infra search label, the nodes that
// carry no overflow flag and hold at least one searched field (Fields: the
// twelve properties the key joins and the free-text and "::" equality
// predicates read). The read cutover's readiness probe (#7245 PR-2) uses it
// instead of CountUnkeyed: a node that holds none of those fields can never
// match a non-empty search, so the index cannot lose it. Such nodes are
// written today by the posture and identity writers that MERGE an infra node
// by uid without the key clause (ec2_block_device_kms_posture_node_writer.go,
// ec2_internet_exposure_node_writer.go, ec2_instance_identity_node_writer.go,
// s3_internet_exposure_node_writer.go, rds_posture_node_writer.go, and the
// KustomizeOverlay base_refs write in canonical_kustomize_edges.go) when they
// run before the canonical upsert. CountUnkeyed keeps counting every node
// without a flag. A field counts when it is not null, whatever its type, so
// the count is a superset of the nodes the search can match. Neo4j only.
func CountUnkeyedSearchable(ctx context.Context, runner Runner) (map[string]int64, error) {
	labels := graph.InfraSearchKeyLabels()
	counts := make(map[string]int64, len(labels))
	for _, label := range labels {
		rows, err := runner.Run(ctx, countUnkeyedSearchableCypher(label), nil)
		if err != nil {
			return nil, fmt.Errorf("count unkeyed searchable infra nodes for label %s: %w", label, err)
		}
		counts[label] = firstInt(rows, "unkeyed")
	}
	return counts, nil
}

// countUnkeyedSearchableCypher builds the per-label CountUnkeyedSearchable
// statement.
func countUnkeyedSearchableCypher(label string) string {
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		terms = append(terms, "n."+field+" IS NOT NULL")
	}
	return "MATCH (n:" + label + ")\n" +
		"WHERE n." + graph.InfraSearchOverflowProperty + " IS NULL\n" +
		"  AND (" + strings.Join(terms, " OR ") + ")\n" +
		"RETURN count(n) AS unkeyed"
}
