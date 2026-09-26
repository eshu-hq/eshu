// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownership

// The three ownership statements are grant-free owner projections: each
// anchors on the page's own keys (the keyed node first, `WHERE n.<key> IN
// $uids`), walks the one owning edge, and returns the owner's repository id.
// The grant is applied in Go (RepositoryAccessFilter.AllowsRepositoryID), so
// statement cost does not depend on the caller's grant size.
//
// Historical, NornicDB only (measured on the pinned NornicDB build before the
// 2026-09-25 Neo4j rule; docs/internal/evidence/5167-impact-grant.md):
// the grant-anchored alternatives (UNWIND $grant_ids AS g MATCH (owner {repo_id: g})
// -[...]->(n) WHERE n.uid IN $uids) cost 6-10 s at grant 128 x 2000 keys for
// CloudResource and WorkloadInstance and 32-106 s for TerraformStateResource,
// whose TerraformResource.repo_id has no index; the owner projections cost
// ~0.24 s per 2048 keys per class at ChunkSize, at any grant size. Per-chunk
// cost grows with the square of the key list, so small chunks win. The
// keyed-node-first direction matters: starting the TerraformStateResource
// pattern at the TerraformResource side took 6.6 s for 2000 keys.
//
// Each statement returns `uid` (the matched key, whatever property it is) and
// `repo_id` (the owner's repository id), one row per distinct pair, ordered
// and capped at $row_limit (RowLimit) so owner fan-in on a widely shared node
// cannot grow a chunk's result without bound. The cap bounds returned rows,
// not the owner edges expanded before it; budget.go records the hub cost. ORDER BY and LIMIT sit on the
// RETURN clause: a WITH ... ORDER BY ... LIMIT form drops the ORDER BY on the
// pinned build.

// CloudResourceOwnerCypher returns the repo_id of every WorkloadInstance that
// USES each asked CloudResource. A CloudResource with no USES owner returns no
// row, so it stays ungranted.
const CloudResourceOwnerCypher = `MATCH (n:CloudResource)<-[:USES]-(wi:WorkloadInstance)
WHERE n.uid IN $uids
RETURN DISTINCT n.uid AS uid, wi.repo_id AS repo_id
ORDER BY uid, repo_id
LIMIT $row_limit`

// WorkloadInstanceOwnerCypher returns the Repository every asked
// WorkloadInstance has a DEPLOYMENT_SOURCE edge to (the rescue for an instance
// whose own repo_id is ungranted). WorkloadInstance nodes are keyed on id: the
// canonical writer (storage/cypher/canonical.go) sets no uid.
const WorkloadInstanceOwnerCypher = `MATCH (wi:WorkloadInstance)-[:DEPLOYMENT_SOURCE]->(r:Repository)
WHERE wi.id IN $uids
RETURN DISTINCT wi.id AS uid, r.id AS repo_id
ORDER BY uid, repo_id
LIMIT $row_limit`

// TerraformStateResourceOwnerCypher returns the repo_id of every
// TerraformResource that MATCHES_STATE each asked TerraformStateResource.
const TerraformStateResourceOwnerCypher = `MATCH (n:TerraformStateResource)<-[:MATCHES_STATE]-(t:TerraformResource)
WHERE n.uid IN $uids
RETURN DISTINCT n.uid AS uid, t.repo_id AS repo_id
ORDER BY uid, repo_id
LIMIT $row_limit`

// statementFor returns the owner statement for a statement-checked class.
func statementFor(class Class) string {
	switch class {
	case ClassWorkloadInstance:
		return WorkloadInstanceOwnerCypher
	case ClassCloudResource:
		return CloudResourceOwnerCypher
	case ClassTerraformStateResource:
		return TerraformStateResourceOwnerCypher
	default:
		return ""
	}
}
