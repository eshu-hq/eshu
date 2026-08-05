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
  disclosed. Re-verified in round 8: that function appends `entry`
  unconditionally, so the repository always appears and only its nested
  evidence is clipped.
- ~~`serviceEvidenceFileLimit` is a generic shared upstream evidence bound
  rather than a consumer-set bound. It is the natural edge of this enumeration
  and is not disclosed.~~ **Wrong; corrected in round 9 below.** It is a
  consumer-set bound. The sentence described what the limit counts (files) and
  concluded from that what it drops (nothing reachable), which does not follow:
  the hostnames are extracted from those files.
- A per-search content read that returns exactly its row cap is reported as
  truncated. That read carries no over-fetch probe, so "exactly limit" and
  "limit plus more" are indistinguishable; the conservative direction is
  chosen and the flag can therefore be true when nothing was actually dropped.
- The hostname affinity narrowing (source 2b, added in round 8) sets the flag
  whenever it discards any hostname, whether or not a consumer was reachable
  only through one of the discarded ones. Same conservative direction, same
  false-positive shape as the per-search cap above.
- `downstream_read_limit` is derived from `boundedTraceEnrichmentLimit(0)`
  because every route that renders `result_limits` or `coverage_summary`
  enriches with `MaxDepth` unset. A future route that both passes a non-zero
  `MaxDepth` and renders one of those blocks would have to thread its own
  bound through; nothing enforces that today.
- `go/internal/query/AGENTS.md` (1312 lines) and
  `docs/public/reference/http-api/context-and-stories.md` (already over the
  limit before this branch) both exceed `CLAUDE.md`'s 500-line rule. Neither
  split belongs in this change. Raised with the owner rather than filed.

## Round 8: the enumeration was wrong a third time, and the wiring could be cut

Two findings, one root defect at two altitudes: the disclosure signal could be
discarded without any test noticing, and the enumeration it carries was still
short by one source.

**The production wiring could drop sources 2 through 5.** Replacing
`consumers, consumersTruncated, err := loadConsumerRepositoryEnrichmentFromCandidates(...)`
with `consumersTruncated := candidatesTruncated` in `service_query_enrichment.go`
passed the entire suite. That one line discards the hostname cap, the
`trimmedHostnames` cut, the per-search cap, and the final merge cap -- exactly
the sources round 7 had just added. Nothing caught it because
`deployment_trace_truncation_disclosure_test.go` asserts those sources only
against the helper (seven call sites, none through the enrichment or a handler),
and the wiring cases in `service_query_truncation_wiring_test.go` seeded a
workload with no hostnames, so `consumersTruncated` and `candidatesTruncated`
were equal in every scenario they exercised.
`TestTraceDeploymentChainDistinguishesConsumerTruncationFromCandidateTruncation`
now drives the real handler with the two flags deliberately opposed: an
untruncated candidate read alongside a full-page per-search content read, so
`consumer_repositories_truncated` is true while `dependents_truncated` and
`provisioning_source_chains_truncated` are absent.

**A sixth source, in the same function as source 2.**
`boundedIndirectEvidenceHostnamesForService` has two drop paths and returned one
bool. The bool came only from the 4-cap; the affinity narrowing, which discards
every hostname carrying no distinctive token from the service's own name,
reported nothing. Measured on service `orders-api` with hostnames
`orders.example.com`, `legacy-billing.acme.test`, `cart-gw.acme.test`: `in=3
out=[orders.example.com] truncated=false` -- two of three dropped silently, well
under the 4-cap. A service on a legacy or vanity domain therefore got a
complete-looking `consumer_repository_count` with `truncated: false`, which is
the same failure round 7 fixed for the cap.

The affinity path was never tested in the direction where it fires. Round 7's
own hostname-cap subtest states that it routes around it on purpose ("Nine
hostnames, none carrying the service's own distinctive token, so the affinity
filter falls through to the first-N fallback"). Isolating the cap that way is
reasonable; declaring the enumeration complete without ever exercising the other
branch is what let this survive three rounds. The returned bool now ORs
`len(affine) < len(unique)`, the source is documented as 2b on the
`loadConsumerRepositoryEnrichmentFromCandidates` enumeration, and the
three-hostname case above is a regression subtest.

**Scoped over-disclosure: right behavior, wrong reasoning.** Round 7 justified
disclosing the flag to scoped callers with "the dropped rows were never read, so
they cannot be shown to fall outside the grant." That reasons about what the
server knows, not what the client can infer, and it is wrong.
`candidatesTruncated` is computed from the raw pre-authorization read, so it
fires on global cardinality regardless of grant. Combined with the documented
`max_depth` x 10 scaling (capped at 100), a scoped caller can sweep `max_depth`
over 1..10, read the flag at bounds 10, 20, ... 100, and recover the global
provisioning-candidate row count to within 10 across [10,100] -- a coarse
cardinality oracle over out-of-grant data, newly amplified by this branch's
`downstream_read_limit`. The behavior is kept: suppressing the flag
reintroduces round 7's false negative, and a silent false claim of completeness
is worse than a bucketed count when no repository identity is exposed and the
caller already holds the service. The code comment and the public doc now say
the flag is a global signal rather than a grant-relative one, and name the
sweep.

The public reference also asserted each flag is present "only when" the matching
read hit a bound "and absent otherwise", while this document's *Still uncovered*
section listed two deliberate false-positive conditions. The internal note
disclosed them and the public API reference denied them. Corrected to "when",
with both conditions carried into the public doc.

`findings[].summary` on `GET /investigations/services/{service_name}` was the
other half of round 7's P1-1. The response object carried the signal; the
human-readable string next to it still rendered a 40-dependent service as "25
graph dependent(s), 0 content consumer repo(s)" with no marker. It now takes a
`(bounded)` suffix, chosen per family (downstream families read
`dependents_truncated`/`consumer_repositories_truncated`, upstream reads
`provisioning_source_chains_truncated`) rather than from a single OR, so a list
that was not bounded is not marked as if it were.

`max_depth` declares `minimum`/`maximum` in `openapi_paths_impact.go` and
declares neither on the MCP tool. The clamping rationale applies to both, so the
split is now stated on the OpenAPI const: every sibling `max_depth` in that
document declares both bounds, and MCP is the deliberate deviation because a
validating MCP client turns an advertised bound into a client-side rejection of
a value the handler would have clamped.

Both round-8 fixes were proven by mutation, and both mutants were killed by
assertions rather than compile errors:

- Replacing the returned bool with `consumersTruncated := candidatesTruncated`
  at `service_query_enrichment.go:176` reds exactly one test,
  `TestTraceDeploymentChainDistinguishesConsumerTruncationFromCandidateTruncation`,
  on `consumer_repositories_truncated = <nil>, want true`. Before this change
  that mutant passed the whole package.
- Dropping `|| len(affine) < len(unique)` from the affinity branch reds two
  named cases:
  `TestBoundedIndirectEvidenceHostnamesPrefersServiceOwnedHosts` and
  `TestLoadConsumerRepositoryEnrichmentDisclosesUpstreamHostnameAndSearchBounds/hostname_affinity_narrowing_discloses`.
- Making `serviceInvestigationBoundedSummary` return its input unchanged reds
  four subtests of `TestServiceInvestigationFamilySummaryMarksBoundedReads`,
  including `marker keeps the counts`, which pins the exact rendered string.

The first of those two was asserting the defect. It read "the affinity filter
dropped three vendor hosts, but it selected rather than capped ... nothing was
lost to the bound", and its expectation was `truncated: false`. That assertion
is inverted in this round, with the reasoning recorded next to it.

Suite result lines quoted verbatim; no pass count is quoted, because `rtk`
computes `N passed` over filtered, truncated output and reports different totals
for the same tests depending on how many packages are in the run.

```
ok  	github.com/eshu-hq/eshu/go/internal/query	3.338s
ok  	github.com/eshu-hq/eshu/go/internal/queryplan	1.594s
ok  	github.com/eshu-hq/eshu/go/internal/mcp	3.262s
ok  	github.com/eshu-hq/eshu/go/internal/query	14.741s   (go test -race)
```

These are warm-cache wall times from the final run of the round-8 branch state;
they measure the suite, not the change, and are quoted only to identify the
runs.

`golangci-lint run ./...` reported `0 issues`. `scripts/verify-openapi.sh`
reported `OpenAPI surface clean: 253 HandleFunc routes, 253 OpenAPI path
entries`. `go run ./cmd/capability-inventory -mode=verify` reported `capability
catalog and surface inventory verified`.
`ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main scripts/verify-performance-evidence.sh`
reported `benchmark and observability markers found for hot-path changes`. The
strict mkdocs build completed in 39.87 seconds.

No-Regression Evidence: round 8 adds one boolean OR in
`boundedIndirectEvidenceHostnamesForService` and one string suffix in a
findings summary. No query shape, anchor, index, or row bound changed, so the
no-measurable-regression statement above still holds unchanged.

No-Observability-Change: no span, metric, label, or log event was added or
altered.

## Round 9: a seventh source, and a bound that was structurally dead

**Source 0 — the service evidence file read.** `loadServiceQueryEvidence`
called `ListRepoFiles(ctx, repoID, serviceEvidenceFileLimit)` — 5000, a real
SQL `LIMIT` with `ORDER BY relative_path` in `ContentReader.ListRepoFiles`.
Every hostname the service surfaces is extracted from those files, and those
hostnames are what `searchConsumerEvidenceAnyRepo` searches other repositories
for. A hostname living only in a file past the cut is never extracted, so a
consumer repository reachable only through it never enters the merged set.
That is the criterion rounds 7 and 8 used to justify disclosing sources 2 and
2b, applied one read further upstream.

It was worse placed than the others: no over-fetch probe, no `len(files) >=
limit` check, and `loadServiceQueryEvidence` returned
`(ServiceQueryEvidence, error)` with no field a truncation signal could ride
out on. It could not reach any flag even in principle. The *Still uncovered*
entry above dismissed it as "a generic shared upstream evidence bound rather
than a consumer-set bound" — reasoning from what the limit counts to what it
drops, which is exactly the step that let it hide through four rounds.

The fix follows the in-package `repositoryTreeFileLimit+1` precedent
(`repository_tree.go`): `listServiceEvidenceFiles` probes
`serviceEvidenceFileLimit+1`, trims back to the bound, and reports a bool that
lands on a new unexported `ServiceQueryEvidence.filesTruncated`.
`enrichServiceQueryContextWithOptions` threads it into
`loadConsumerRepositoryEnrichmentFromCandidates` as a new
`evidenceFilesTruncated` parameter, numbered source 0 because 1 through 5 are
referenced by number elsewhere in the package.

It is fed only to `consumer_repositories_truncated`. `dependents_truncated`
and `provisioning_source_chains_truncated` both derive from the candidate
slice, which this read does not touch, so ORing it into them would stamp a
bound that never applied to those lists.

**Source 3 was structurally dead in production.** The mutant deleting
`truncated = true` from the `trimmedHostnames[:limit]` cut survived the full
suite. `boundedIndirectEvidenceHostnamesForService` has already capped the list
at `indirectEvidenceHostnameLimit` (4), and every production limit comes from
`boundedTraceEnrichmentLimit`, whose smallest result is 10
(`defaultIndirectEvidenceSearchLimit` = 25 when `MaxDepth` is unset, otherwise
`MaxDepth` x 10 clamped to `maxIndirectEvidenceSearchLimit` = 100). The branch
therefore needs a limit of 1, 2, or 3, which today only the test-only
`loadConsumerRepositoryEnrichment` / `...WithLimit` wrappers can supply. The
direction is safe — over-disclosure, never a false negative — but the code
enumeration and the public reference both presented it as a live bound. It is
now exercised by a regression rather than removed, because it is what would
start firing first if either constant moved, and the unreachability is stated
where the source is enumerated.

**Also named, not folded in.** The `repoID == "" || repoName == ""` skip in
`queryProvisioningRepositoryCandidates` drops only rows the graph returned
without an identity. Such a row carries nothing a caller could render or
address, so dropping it loses no reachable consumer. It was previously
unnamed; it is named on the enumeration now so the next round does not have to
rediscover that it was considered. `repositorySemanticEntityLimit` stays
undisclosed on the reasoning already recorded above.

Both round-9 fixes were proven by mutation. Every mutant was killed by an
assertion, not a compile error:

- `serviceEvidenceFileLimit+1` reverted to `serviceEvidenceFileLimit` in
  `listServiceEvidenceFiles` reds
  `TestLoadServiceQueryEvidenceDisclosesTheFileListBound/a_full_page_discloses,_and_the_overflow_hostname_is_gone`
  and
  `TestTraceDeploymentChainDisclosesTheServiceEvidenceFileBound/a_full_evidence_file_page_discloses_only_the_consumer_flag`.
- Dropping `|| evidenceFilesTruncated` from
  `loadConsumerRepositoryEnrichmentFromCandidates` reds
  `TestLoadConsumerRepositoryEnrichmentDisclosesTheServiceEvidenceFileBound`
  and the same trace subtest.
- Passing a literal `false` instead of `evidence.filesTruncated` at the
  production call site in `service_query_enrichment.go` reds only the trace
  subtest — the wiring case is the only thing holding that line in place, the
  same gap round 8 found for the per-search cap.
- Deleting `truncated = true` from the source-3 cut reds
  `TestLoadConsumerRepositoryEnrichmentDisclosesTheHostnameLimitCut` and
  nothing else. Before this round it reds nothing at all.

The file-bound regression seeds the same corpus one file apart, so the only
variable is which side of the cut the single hostname-bearing file lands on.
The shorter half asserts the hostname IS extracted, which is what stops the
longer half from passing because the fixture never produced a hostname.

Newly-disclosed false positive, carried into the public reference alongside the
two round-8 ones: a repository with more than 5000 indexed files now reports
`consumer_repositories_truncated` whether or not the files past the bound held
any hostname. The bound is on files; the hostnames are derived from them, and
the read cannot tell which of the two happened.

No-Regression Evidence: round 9 adds one `+1` to an existing `LIMIT` argument,
one boolean OR, and one parameter. The file read fetches at most one extra row
and trims it before any loop; no query shape, anchor, index, or terminal row
bound changed, so the no-measurable-regression statement above holds unchanged.

No-Observability-Change: no span, metric, label, or log event was added or
altered.

## Round 10 and later

Round 10 rewrote the membership rule this enumeration runs on, and the record
for it lives in `evidence-5720-truncation-enumeration.md` rather than here --
this file was already 436 lines when round 10 opened, and `CLAUDE.md` says to
split before 500, not at it.
Read that file for what makes a bound a member of the enumeration, why the
hostname affinity narrowing lost its number, and why the row bound on
`queryProvisioningRepositoryCandidates` can fire without dropping a repository.
