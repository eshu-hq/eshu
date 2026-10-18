// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
)

// WorkloadSibling is one Workload node sharing a bare name with a workload
// in another repository, as read back from the graph for SAME_NAME pairing.
// It is an alias for handle.Candidate (identical fields) so
// WorkloadSiblingLookup structurally satisfies handle.SiblingReader
// and every ladder consumer shares one adapter.
type WorkloadSibling = handle.Candidate

// WorkloadSiblingLookup lists every Workload node whose name is in names,
// across all repositories. The materializer pairs across repositories in Go
// (compatibility doc section 1.3): the single-statement inline-match shape is
// the documented NornicDB trailing-WHERE pitfall and is never used.
type WorkloadSiblingLookup interface {
	ListWorkloadSiblings(ctx context.Context, names []string) ([]WorkloadSibling, error)
}

// batchSameNameEdgeUpsertCypher writes one SAME_NAME edge per ordered pair.
// Direction is deterministic (lexically smaller id to larger) and readers
// always traverse it undirected, so concurrent writers converge on one edge;
// probe P6 measured 0 duplicates in 20 concurrent trials on pinned Neo4j.
const batchSameNameEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (a:Workload {id: row.a})
MATCH (b:Workload {id: row.b})
MERGE (a)-[rel:SAME_NAME]->(b)
SET rel.name = row.name,
    rel.relationship_basis = 'name_equality',
    rel.confidence = 0.5,
    rel.evidence_source = row.evidence_source,
    rel.identity_key = 'canonical',
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id`

// writeSameNameEdges links this pass's workloads to same-named workloads in
// other repositories. A nil lookup keeps the legacy write (no edges). The
// edge is written only after this pass's own Workload nodes commit, so both
// endpoints exist; a sibling committed later is paired by that later pass,
// whose own lookup sees this pass's node.
func (m *WorkloadMaterializer) writeSameNameEdges(
	ctx context.Context,
	projection *ProjectionResult,
) (int, error) {
	if m.SiblingLookup == nil || len(projection.WorkloadRows) == 0 {
		return 0, nil
	}
	names := distinctWorkloadNames(projection.WorkloadRows)
	if len(names) == 0 {
		return 0, nil
	}
	siblings, err := m.SiblingLookup.ListWorkloadSiblings(ctx, names)
	if err != nil {
		return 0, err
	}
	pairs := sameNamePairs(projection, siblings)
	if len(pairs) == 0 {
		return 0, nil
	}
	if err := m.executeBatched(ctx, batchSameNameEdgeUpsertCypher, pairs); err != nil {
		return 0, err
	}
	return len(pairs), nil
}

func distinctWorkloadNames(rows []WorkloadRow) []string {
	seen := make(map[string]struct{}, len(rows))
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.WorkloadName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// sameNamePairs builds the ordered cross-repository pairs between this pass's
// workload rows and the graph-read siblings. Only pairs touching at least one
// of this pass's own nodes are written: pairs between two foreign nodes were
// (or will be) written by their own passes, and writing them here would
// restamp another scope's edge with this scope's generation.
func sameNamePairs(projection *ProjectionResult, siblings []WorkloadSibling) []map[string]any {
	own := make(map[string]WorkloadSibling, len(projection.WorkloadRows))
	for _, row := range projection.WorkloadRows {
		if row.WorkloadID == "" || row.RepoID == "" || row.WorkloadName == "" {
			continue
		}
		own[row.WorkloadID] = WorkloadSibling{ID: row.WorkloadID, Name: row.WorkloadName, RepoID: row.RepoID}
	}
	type pairKey struct{ a, b string }
	seen := make(map[pairKey]struct{})
	pairs := make([]map[string]any, 0)
	for _, sib := range siblings {
		if sib.ID == "" || sib.RepoID == "" || sib.Name == "" {
			continue
		}
		for _, mine := range own {
			if sib.Name != mine.Name || sib.RepoID == mine.RepoID || sib.ID == mine.ID {
				continue
			}
			a, b := mine.ID, sib.ID
			if b < a {
				a, b = b, a
			}
			key := pairKey{a: a, b: b}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			pairs = append(pairs, map[string]any{
				"a":               a,
				"b":               b,
				"name":            mine.Name,
				"evidence_source": EvidenceSourceWorkloads,
				"scope_id":        projection.ScopeID,
				"generation_id":   projection.GenerationID,
			})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i]["a"] != pairs[j]["a"] {
			return pairs[i]["a"].(string) < pairs[j]["a"].(string)
		}
		return pairs[i]["b"].(string) < pairs[j]["b"].(string)
	})
	return pairs
}

// countIdentityCollisions records the rows dedupeMaterializerRowsByString
// is about to drop for key: a drop whose repo_id matches the kept row is a
// same_scope_drop (one repository re-deriving its own identity); a drop
// whose repo_id differs means two repositories claimed one identity
// (cross_scope_merge) — the #5385 defect, expected zero after the re-key.
func (m *WorkloadMaterializer) countIdentityCollisions(ctx context.Context, rows []map[string]any, key string) {
	keptRepo := make(map[string]string, len(rows))
	for _, row := range rows {
		id, _ := row[key].(string)
		if id == "" {
			continue
		}
		repo, _ := row["repo_id"].(string)
		if kept, ok := keptRepo[id]; ok {
			mode := "cross_scope_merge"
			if kept == repo {
				mode = "same_scope_drop"
			}
			m.recordWorkloadIdentityCollision(ctx, mode)
			continue
		}
		keptRepo[id] = repo
	}
}

// recordWorkloadIdentityCollision counts one identity collision by mode for
// eshu_dp_workload_identity_collision_total. Nil-safe.
func (m *WorkloadMaterializer) recordWorkloadIdentityCollision(ctx context.Context, mode string) {
	if m == nil || m.Instruments == nil || m.Instruments.WorkloadIdentityCollisions == nil {
		return
	}
	m.Instruments.WorkloadIdentityCollisions.Add(ctx, 1,
		metric.WithAttributes(attribute.String("mode", mode)))
}
