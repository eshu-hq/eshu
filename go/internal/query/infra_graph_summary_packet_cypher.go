// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// graphSummaryRelationshipCounts is the fixed, bounded set of code relationship
// types counted for a repo. Each is counted with its own single-type query
// rather than chained aggregation, mirroring the per-label portability rule in
// infra_ecosystem_overview.go. CALLS/REFERENCES/INHERITS/OVERRIDES are anchored
// at the repo's contained entities (source side); IMPORTS is anchored at the
// repo's File source side.
var graphSummaryRelationshipCounts = []struct {
	relType string
	cypher  string
}{
	{"CALLS", `MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(src)-[r:CALLS]->()
RETURN count(r) AS count`},
	{"IMPORTS", `MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[r:IMPORTS]->()
RETURN count(r) AS count`},
	{"INHERITS", `MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(src)-[r:INHERITS]->()
RETURN count(r) AS count`},
	{"OVERRIDES", `MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(src)-[r:OVERRIDES]->()
RETURN count(r) AS count`},
	{"REFERENCES", `MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(src)-[r:REFERENCES]->()
RETURN count(r) AS count`},
}

// graphSummaryRepoEcosystemCounts are the repo-anchored structural counts. Each
// reuses the narrow count shapes proven by repository/context_counts.go rather
// than a broad OPTIONAL aggregation.
var graphSummaryRepoEcosystemCounts = []struct {
	field  string
	cypher string
}{
	{"file_count", `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
RETURN count(DISTINCT f) AS count`},
	{"workload_count", `MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
RETURN count(DISTINCT w) AS count`},
	{"platform_count", `MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
MATCH (w)<-[:INSTANCE_OF]-(i:WorkloadInstance)
MATCH (i)-[:RUNS_ON]->(p:Platform)
RETURN count(DISTINCT p) AS count`},
	{"dependency_count", `MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON|CORRELATES_DEPLOYABLE_UNIT]->(dep:Repository)
RETURN count(DISTINCT dep) AS count`},
}

// graphSummaryRepoLanguagesCypher returns the repo's languages ranked by file
// count, reusing the repo-anchored file-language shape from
// repository/story_counts.go.
const graphSummaryRepoLanguagesCypher = `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
WHERE f.language IS NOT NULL
RETURN f.language AS language, count(DISTINCT f) AS file_count
ORDER BY file_count DESC`

// The 50,001st raw edge is counted before pair deduplication or ranking. A
// missing UID triggers the unchanged Go path, whose key fallback uses id.
const graphSummaryNeo4jDegreeCypher = `MATCH (source:Function {repo_id: $repo_id})-[call:CALLS]->(target:Function {repo_id: $repo_id})
WITH source, target LIMIT $edge_scan_limit
WITH count(*) AS raw_edges,
     sum(CASE WHEN source.uid IS NULL OR source.uid = '' OR target.uid IS NULL OR target.uid = '' THEN 1 ELSE 0 END) AS invalid_uid_edges,
     collect(DISTINCT [source,target]) AS pairs
UNWIND CASE WHEN size(pairs) = 0 THEN [[null,null]] ELSE pairs END AS pair
WITH raw_edges, invalid_uid_edges, pair[0] AS source, pair[1] AS target
UNWIND CASE WHEN source IS NULL THEN [{n:null,incoming:0,outgoing:0}]
            ELSE [{n:source,incoming:0,outgoing:1},{n:target,incoming:1,outgoing:0}] END AS e
WITH raw_edges, invalid_uid_edges, e.n AS n,
     sum(e.incoming) AS incoming, sum(e.outgoing) AS outgoing
WITH raw_edges, invalid_uid_edges, n, incoming, outgoing,
     incoming+outgoing AS total_degree
RETURN raw_edges, invalid_uid_edges,
       coalesce(n.id,n.uid) AS function_id,
       coalesce(n.name,'') AS function_name,
       coalesce(n.relative_path,'') AS file_path,
       n.uid AS function_key, incoming, outgoing, total_degree
ORDER BY total_degree DESC, incoming DESC, outgoing DESC,
         file_path, coalesce(n.start_line,0), function_name, function_id, function_key
LIMIT $rank_limit`
