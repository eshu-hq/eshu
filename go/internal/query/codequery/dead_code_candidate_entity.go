// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
)

// This file owns two candidate-scan families: the predicates that say whether a
// row is a dead-code candidate entity, and the incoming-edge probe that decides
// whether such a candidate is reachable. They live together because both are
// keyed on the candidate entity id the shared candidate read returned, and
// because internal/query's non-test file set is pinned by the dirgate
// grandfather ledger, so a family that outgrows its file moves to a sibling
// that already owns it rather than to a new file.

func mergeStrongestDeadCodeIncomingEdge(
	incoming map[string]deadCodeIncomingEdge,
	entityID string,
	edge deadCodeIncomingEdge,
) {
	deadcode.MergeStrongestDeadCodeIncomingEdge(incoming, entityID, edge)
}

func (h *CodeHandler) deadCodeResultsWithGraphIncomingEdges(
	ctx context.Context,
	results []map[string]any,
	label string,
) (map[string]deadCodeIncomingEdge, error) {
	entityIDs := deadCodeResultEntityIDs(results)
	incoming := make(map[string]deadCodeIncomingEdge)
	if len(entityIDs) == 0 {
		return incoming, nil
	}
	access := codeGrantAccessFilter(ctx)
	rows, err := h.Neo4j.Run(
		ctx,
		buildDeadCodeScopedIncomingBatchProbeCypher(label, access),
		access.GraphParams(map[string]any{"entity_ids": entityIDs}),
	)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		entityID := strings.TrimSpace(StringVal(row, "incoming_entity_id"))
		if entityID == "" {
			continue
		}
		// in_grant is projected only by the scoped statement. BoolVal reads an
		// absent column as false, so the unscoped caller -- whose statement has
		// no such column and whose every row is evidence -- must be answered
		// before the column is consulted at all.
		if access.Scoped() && !BoolVal(row, "in_grant") {
			mergeStrongestDeadCodeIncomingEdge(incoming, entityID, deadCodeIncomingEdge{HiddenConsumer: true})
			continue
		}
		method := strings.TrimSpace(StringVal(row, "resolution_method"))
		mergeStrongestDeadCodeIncomingEdge(incoming, entityID, deadCodeIncomingEdge{
			MaxConfidence: codeprovenance.Confidence(method),
			Method:        method,
		})
	}
	return incoming, nil
}

func deadCodeResultEntityIDs(results []map[string]any) []string {
	entityIDs := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		entityID := strings.TrimSpace(StringVal(result, "entity_id"))
		if entityID == "" {
			continue
		}
		if _, ok := seen[entityID]; ok {
			continue
		}
		seen[entityID] = struct{}{}
		entityIDs = append(entityIDs, entityID)
	}
	return entityIDs
}

// buildDeadCodeScopedIncomingBatchProbeCypher builds the one incoming-edge
// probe a scoped caller runs. It expands the candidate's incoming edges once
// and projects the grant per row as in_grant, rather than running a grant-bound
// probe and an unrestricted one and diffing their rows.
//
// Projecting the grant is what keeps the answer honest. Both earlier probes
// RETURN DISTINCTed the (entity, resolution_method) pair, so an ungranted edge
// whose method a granted edge also carried was byte for byte the granted row:
// the diff came back empty and the caller was never told a consumer was hidden
// from them. Grouping on (entity, method, in_grant) keeps it as its own row,
// which is the per-row decision the SQL half already makes with
// consumer_in_grant.
//
// Expanding once is also the cheaper shape. Measured against the pinned
// NornicDB v1.2.3 on one entity with 5,000 incoming edges split across two
// repositories, four interleaved runs of 15 iterations each: this probe's
// median was 274-303us, which is 44-50% below the 497-583us the pair cost and
// 2-14% above what a single probe costs on its own. The overhead over one probe
// has the same sign in all four runs, so it is real rather than noise; it is
// the OPTIONAL MATCH the pair did not pay for. See
// docs/internal/evidence/5167-code-family-batch-1.md.
//
// Two clauses of it are load-bearing on NornicDB and must not be "tidied":
//
//   - count(*) is never read. It is what makes the RETURN an aggregation, and
//     therefore what groups the rows. RETURN DISTINCT cannot be used here: on
//     the pinned backend, DISTINCT after a trailing OPTIONAL MATCH on the
//     relationship-seeded traversal branch is absorbed into the first
//     projection's source text, so incoming_entity_id comes back as the literal
//     string "DISTINCT coalesce(e.uid, e.id)" and nothing is deduplicated.
//     A WITH between the OPTIONAL MATCH and the RETURN is worse: every column
//     comes back null. See docs/public/reference/nornicdb-pitfalls.md, and run
//     the live_nornicdb_dead_code_incoming tests by hand before changing this
//     -- no CI job builds that tag.
//   - the source repository is an OPTIONAL MATCH here precisely because the
//     grant is no longer a filter. A required MATCH would drop the rows this
//     probe exists to report -- a source in a repository the caller was not
//     granted, and a source the graph cannot attribute to any repository. Both
//     project in_grant=false and become the hidden-consumer marker, so the
//     answer is unknown rather than a symbol wrongly reported unused.
//
// An unscoped caller gets the unrestricted probe text unchanged.
func buildDeadCodeScopedIncomingBatchProbeCypher(label string, access querycontract.RepositoryAccessFilter) string {
	if !access.Scoped() {
		return deadcode.BuildDeadCodeIncomingBatchProbeCypher(label)
	}
	if !deadcode.IsDeadCodeCandidateLabel(label) {
		label = "Function"
	}
	return `
		UNWIND $entity_ids AS entity_id
		MATCH (e:` + label + ` {uid: entity_id})<-[rel:CALLS|IMPORTS|REFERENCES|INHERITS|EXECUTES]-(source)
		OPTIONAL MATCH (source)<-[:CONTAINS]-(:File)<-[:REPO_CONTAINS]-(source_repo:Repository)
		RETURN coalesce(e.uid, e.id) as incoming_entity_id,
		       rel.resolution_method as resolution_method,
		       (source_repo IS NOT NULL AND ` + access.GraphCondition("source_repo") + `) as in_grant,
		       count(*) as edge_count
	`
}
