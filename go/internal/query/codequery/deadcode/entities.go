// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// DeadCodeResultEntityIDs returns the distinct candidate entity ids in row
// order, skipping rows with no entity id.
func DeadCodeResultEntityIDs(results []map[string]any) []string {
	entityIDs := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		entityID := strings.TrimSpace(querycontract.StringVal(result, "entity_id"))
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

// BuildDeadCodeScopedIncomingBatchProbeCypher builds the one incoming-edge
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
func BuildDeadCodeScopedIncomingBatchProbeCypher(label string, access querycontract.RepositoryAccessFilter) string {
	if !access.Scoped() {
		return BuildDeadCodeIncomingBatchProbeCypher(label)
	}
	if !IsDeadCodeCandidateLabel(label) {
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
