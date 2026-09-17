// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// projectorKustomizeOverlayListCypher lists every KustomizeOverlay node in one
// repo. Single anchoring clause (MATCH -> WHERE -> RETURN, no intervening
// WITH or second MATCH) -- the safe read shape documented in
// docs/public/reference/nornicdb-query-pitfalls.md's "Multi-Clause Read
// Queries Silently Corrupt The Projection", mirroring
// ingesterKustomizeOverlayListCypher. Anchored by the #5445
// kustomize_overlay_repo_id index
// (go/internal/graph/schema_tables_indexes.go), not a bare label scan.
const projectorKustomizeOverlayListCypher = `MATCH (ko:KustomizeOverlay)
WHERE ko.repo_id = $repo_id
RETURN ko.uid AS uid, ko.path AS path, ko.base_refs AS base_refs`

// projectorKustomizeOverlayResolver adapts a read-only Bolt session to
// sourcecypher.KustomizeOverlayResolver, so the projector's canonical writer
// -- the binary that re-derives the graph on rebuild -- rebuilds the #5445
// EXTENDS_BASE edge set for a repo's FULL KustomizeOverlay set every cycle
// that touches or delta-deletes any of them. Without it the projector fails
// every kustomize edge rebuild closed and the edge survives only when the
// ingester's writer happens to win the shared-queue claim race for the
// overlay materialization (#6184 runs 5-10). Mirrors
// projectorTerraformStateConfigMatchResolver and
// ingesterKustomizeOverlayResolver.
type projectorKustomizeOverlayResolver struct {
	driver       neo4jdriver.DriverWithContext
	databaseName string
}

// ListKustomizeOverlays implements sourcecypher.KustomizeOverlayResolver. A
// query failure returns the error unchanged so the caller fails closed for
// the whole repo's EXTENDS_BASE rebuild this cycle rather than write a
// partial or wrong edge set.
func (r projectorKustomizeOverlayResolver) ListKustomizeOverlays(
	ctx context.Context,
	repoID string,
) ([]sourcecypher.KustomizeOverlayRow, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, projectorKustomizeOverlayListCypher, map[string]any{"repo_id": repoID})
	if err != nil {
		return nil, fmt.Errorf("run kustomize overlay list: %w", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect kustomize overlay list: %w", err)
	}

	rows := make([]sourcecypher.KustomizeOverlayRow, 0, len(records))
	for _, record := range records {
		uidValue, _ := record.Get("uid")
		pathValue, _ := record.Get("path")
		baseRefsValue, _ := record.Get("base_refs")
		uid, ok := uidValue.(string)
		if !ok || uid == "" {
			continue
		}
		path, _ := pathValue.(string)
		rows = append(rows, sourcecypher.KustomizeOverlayRow{
			UID:      uid,
			Path:     path,
			BaseRefs: projectorKustomizeOverlayBaseRefsFromBoltValue(baseRefsValue),
		})
	}
	return rows, nil
}

// projectorKustomizeOverlayBaseRefsFromBoltValue normalizes a Bolt-decoded
// base_refs property to []string. A node written before this feature landed
// (the #5445 backfill semantic) has no base_refs property at all, which the
// Neo4j Go driver decodes as a nil value here -- normalized to a nil slice,
// not an error. Mirrors kustomizeOverlayBaseRefsFromBoltValue
// (go/cmd/ingester).
func projectorKustomizeOverlayBaseRefsFromBoltValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
