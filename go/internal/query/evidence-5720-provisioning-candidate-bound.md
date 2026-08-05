# Evidence: provisioning-candidate read gained a deterministic, structurally bounded LIMIT (#5720)

`queryProvisioningRepositoryCandidates` (`deployment_trace_support_helpers.go`)
is the single read every route that runs service enrichment shares
(service/workload context and story, `/investigations/services/{name}`, and
`/impact/trace-deployment-chain`) to derive `dependents`,
`consumer_repositories`, and `provisioning_source_chains`. Baseline: `ORDER BY
repo.name, relationship_type` with a conditionally appended `LIMIT $limit` —
the clause was omitted entirely whenever the caller passed `limit <= 0`, so
those callers read unbounded. After: `ORDER BY repo.name, repo.id,
relationship_type, relationship_reason` with an unconditional inline `LIMIT
$limit`, paired with a self-clamp (`limit <= 0 || limit >
maxIndirectEvidenceSearchLimit` both resolve to
`maxIndirectEvidenceSearchLimit`) so the row count returned to Go — and
therefore the deduplicated candidate count — is now a real, structurally
guaranteed maximum of `maxIndirectEvidenceSearchLimit` (100) for every caller,
including the previously-unbounded zero-limit path. The anchor is unchanged: a
single-key seed on `Repository.id = $repo_id` over the same five inbound edge
types
(`PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN|READS_CONFIG_FROM`).
`repo.id` closes the repo-level LIMIT tie two distinct repositories sharing a
display name could otherwise hit at the backend (mirrors the
`loadUncorrelatedCloudResourceCandidatesBounded` precedent
(`cloud_resource_candidates.go`'s `ORDER BY n.name, n.id`) and the
package-wide convention in `catalog.go`, `code_complexity_queries.go`,
`code_quality.go`, `code_relationship_story_class.go`, and
`code_relationship_story_graph.go`); `relationship_type` and
`relationship_reason` additionally close the same-repo row tie so LIMIT
cannot silently drop one row's reason for a same-repo/same-type pair. Backend
NornicDB (default canonical graph backend per this file), Neo4j compatibility
unaffected.

## Round 2 P1-1: the bound existed but was never disclosed

Bounding the read (above) fixed correctness but created a silent accuracy
gap. Every downstream `*_count` field —
`service_story_dossier.go`'s `graph_dependent_count`/`content_consumer_count`,
`service_story_overview.go`'s `dependent_count`/`consumer_repository_count`/
`provisioning_source_chain_count`, `service_story_dossier.go`'s
`result_limits.downstream_count` — and the "truncated" flags in
`buildServiceDownstreamConsumers` and `buildServiceResultLimitsWithContext`
compared row counts against `serviceStoryItemLimit` (50). But
`queryProvisioningRepositoryCandidates`'s own bound on the default path is
`defaultIndirectEvidenceSearchLimit` (25) — every service-story, service-
context, and `investigate_service` call leaves `MaxDepth` at zero
(`boundedTraceEnrichmentLimit(0)` resolves to 25), so the disclosure
threshold (50) was structurally unreachable from a read bounded at 25. A
service with 40 distinct dependent repositories returned 25 rows and
reported `graph_dependent_count: 25`, `dependent_count: 25`, `truncated:
false` — the count and the disclosure disagreed, and nothing in the response
told a caller more rows existed.

The fix: `queryProvisioningRepositoryCandidates` now probes `limit+1` rows
(mirroring the `loadUncorrelatedCloudResourceCandidatesBounded` precedent's
"over-fetch by one row to detect truncation without a second count query"),
trims back to `limit` before grouping, and returns a `truncated bool`
alongside the candidate slice. `service_query_enrichment.go` carries that
bool onto `workloadContext` as `dependents_truncated` (from
`buildGraphDependents`, which is 1:1 over the candidate slice — no separate
cap), `provisioning_source_chains_truncated` (same, via
`loadProvisioningSourceChainsFromCandidates`), and
`consumer_repositories_truncated`. The last of the three has a second,
independent truncation source: `loadConsumerRepositoryEnrichmentFromCandidates`
merges the graph candidates with content-evidence consumer matches and then
applies its own final `consumers[:limit]` cap, which can trim rows even when
the graph candidates were not truncated (content evidence can name consumer
repositories the graph read never saw). That function now takes the upstream
`candidatesTruncated` bool as a parameter and returns its own `truncated`
bool, OR-ing the two sources together so neither is lost.

`buildServiceDownstreamConsumers` and `buildServiceResultLimitsWithContext`
(`service_story_dossier.go`) OR these three `workloadContext` flags into
their existing `truncated` fields, so the dossier and result-limits response
sections report `truncated: true` even when every list stays well under
`serviceStoryItemLimit`. `impact_trace_deployment_response.go` surfaces the
same three flags directly on the `/impact/trace-deployment-chain` response
as `dependents_truncated`, `consumer_repositories_truncated`, and
`provisioning_source_chains_truncated`, mirroring the existing
`uncorrelated_cloud_resources_truncated` field. All three are documented in
`openapi_paths_impact.go`.

The 25-vs-50 mismatch that made this gap possible also leaked into the
disclosed numbers themselves. Round 7 P2-5 found `result_limits` reporting
`{limit: 50, downstream_count: 25, truncated: true}`, which tells a caller
more than 50 downstream rows existed when the truth is more than 25. Round 2's
claim here that "the counts, caps, and truncation flags are internally
consistent at every value of the default limit" was therefore wrong, and is
withdrawn. `result_limits` and `coverage_summary` now both report
`downstream_read_limit` — the bound that actually fires, derived from
`boundedTraceEnrichmentLimit(0)` rather than a restated constant — alongside
the 50-row rendering cap. Which of the two numbers a display section chooses
to fill remains a display-budget question and is still left as-is.

No-Regression Evidence: pure ordering/bounding/disclosure fix; the anchor,
seed cardinality, and edge-type set are unchanged, and the returned candidate
count is still capped at `maxIndirectEvidenceSearchLimit` (100). The read
gains two ORDER BY keys (round 1, already covered) and now probes one row
past the disclosed limit (`limit+1`) to detect truncation without a second
count query — a one-row difference on the same single-key anchor, not a
shape change, so this trades a full before/after benchmark for the
no-measurable-regression statement `cypher-query-rigor` permits for a pure
correctness/disclosure fix.

Proof (round 7; earlier revisions of this entry quoted a `5524 passed` figure
that no counting method reproduces — it is withdrawn, and this entry now
quotes the suite result lines verbatim instead of a count):

```
ok  	github.com/eshu-hq/eshu/go/internal/query	5.134s
ok  	github.com/eshu-hq/eshu/go/internal/mcp	4.527s
ok  	github.com/eshu-hq/eshu/go/internal/queryplan	1.493s
ok  	github.com/eshu-hq/eshu/go/internal/query	16.364s   (go test -race)
```

The queryplan manifest's `source_sha256` for
`queryProvisioningRepositoryCandidates` was re-audited against the edited
function body (typed audit, not a digest bump alone): `class: keyed_support`
and `key_bound: single_key` are still true of the new source — the seed is
still the single `Repository.id = $repo_id` anchor, and `rows[:limit]` still
runs before grouping. `max_results` was corrected from 100 to 101 in round 7
(P2-1): in that manifest `max_results` is the requested backend row bound, not
the returned count — the identical over-fetch sibling
`loadUncorrelatedCloudResourceCandidatesBounded` (limit 50) declares
`max_results: 51` — and this read now issues `LIMIT
maxIndirectEvidenceSearchLimit + 1`. The queryplan validator only checks
`max_results > 0`, so nothing else would have caught it;
`TestMaxIndirectEvidenceSearchLimitMatchesManifestMaxResults` remains the only
binding between the const and the manifest.

No-Observability-Change: the read adds no span, metric, label, or new log
event; the existing `service_query.stage_started`/`service_query.stage_completed`
timer events with `stage="graph_provisioning_candidates"`
(`service_query_timing.go`) diagnose stage latency and the post-filter
candidate count.

## Round 7: what the disclosure now covers, and what it does not

Round 2 shipped the disclosure and this entry then claimed "there is nothing
left to defer." That was false, and the sentence is withdrawn. Three things
were wrong:

- The production wiring had no test. Every step between the bounded read and
  the wire — enrichment writing the three keys, `buildDeploymentTraceFields`
  reading them, `attachOptionalFields` emitting them — could be deleted with
  the full `./internal/query` suite green (`verify-openapi.sh` checks route
  parity, not response fields). `service_query_truncation_wiring_test.go` now
  drives the real handler and the real enrichment over a seeded graph and
  asserts both directions.
- `GET /investigations/services/{service_name}` shipped clipped counts with no
  disclosure at all: its only truncation field was the 50-row repository
  marker, structurally unreachable from a 25-row read. `coverage_summary` now
  ORs the three signals in.
- The comment claiming truncation came from exactly two sources was wrong.
  Three more existed, two of them UPSTREAM of both disclosed ones — the
  four-hostname `indirectEvidenceHostnameLimit` cap, the `trimmedHostnames`
  cut, and each per-search content row cap. A consumer reachable only through
  a dropped hostname never entered the merged set, so neither disclosed source
  could see it. All three now feed the same returned bool, and
  `loadConsumerRepositoryEnrichmentFromCandidates` carries the full
  enumeration.

Scoped callers: `candidatesTruncated` is computed from the raw graph read,
which the backend bounds with LIMIT before
`filterProvisioningRepositoryCandidatesForAccess` runs. Round 7 P1-4 fixed the
resulting false negative — a scoped caller whose grant was emptied by the
filter previously received neither rows nor a truncation flag, an empty answer
that read as complete. The disclosure writes now sit outside the
`len(...) > 0` guards. The converse case (a scoped caller whose complete grant
survives the filter still sees `truncated: true`) is retained deliberately:
the rows past the LIMIT cut were never read, so they cannot be shown to fall
outside the grant, and over-disclosing the bound is the only direction that
cannot manufacture a false claim of completeness.

Still uncovered, stated plainly rather than implied closed:

- `repositorySemanticEntityLimit` (5000) clips the per-chain nested entity
  evidence `loadProvisioningSourceChainsFromCandidates` reads. It bounds the
  evidence attached to a chain rather than which repositories appear, so it
  cannot drop a consumer from the set, and it is pre-existing. It is not
  disclosed.
- A per-search content read that returns exactly its row cap is reported as
  truncated. That read carries no over-fetch probe, so "exactly limit" and
  "limit plus more" are indistinguishable; the conservative direction is
  chosen and the flag can therefore be true when nothing was actually dropped.
- `downstream_read_limit` is derived from `boundedTraceEnrichmentLimit(0)`
  because every route that renders `result_limits` or `coverage_summary`
  enriches with `MaxDepth` unset. A future route that both passes a non-zero
  `MaxDepth` and renders one of those blocks would have to thread its own
  bound through; nothing enforces that today.
