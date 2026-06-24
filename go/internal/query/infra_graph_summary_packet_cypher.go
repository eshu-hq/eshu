// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// graphSummaryHotEntitiesCypher ranks the most-connected functions in a repo by
// total call degree. It is the repo-anchored hub-function degree shape proven by
// hubFunctionsCypher (code_call_graph_metrics.go): anchor on the indexed
// Repository id, expand through REPO_CONTAINS/CONTAINS, count distinct incoming
// and outgoing CALLS with OPTIONAL MATCH, sum into total_degree, then order
// deterministically and bound with LIMIT. The packet omits the optional
// language filter and the SKIP $offset paging arm because the packet returns a
// single bounded top-N slice.
const graphSummaryHotEntitiesCypher = `MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(source_file:File)-[:CONTAINS]->(fn:Function)
OPTIONAL MATCH (repo)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(caller:Function)-[:CALLS]->(fn)
WITH repo, source_file, fn, count(DISTINCT caller) AS incoming_calls
OPTIONAL MATCH (repo)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(callee:Function)<-[:CALLS]-(fn)
WITH repo, source_file, fn, incoming_calls, count(DISTINCT callee) AS outgoing_calls
WITH repo, source_file, fn, incoming_calls, outgoing_calls, incoming_calls + outgoing_calls AS total_degree
WHERE total_degree > 0
RETURN source_file.relative_path AS file_path,
       coalesce(fn.id, fn.uid) AS function_id,
       fn.name AS function_name,
       incoming_calls AS incoming_calls,
       outgoing_calls AS outgoing_calls,
       total_degree AS total_degree
ORDER BY total_degree DESC, incoming_calls DESC, outgoing_calls DESC, source_file.relative_path, fn.name
LIMIT $limit`

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
// reuses the narrow count shapes proven by repository_context_counts.go rather
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
// repository_story_counts.go.
const graphSummaryRepoLanguagesCypher = `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
WHERE f.language IS NOT NULL
RETURN f.language AS language, count(DISTINCT f) AS file_count
ORDER BY file_count DESC`
