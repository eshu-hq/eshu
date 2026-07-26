# #5425 — deploy-time events as `ci.deployment_event`

Deploy truth in this family used to be inferred from a workflow job's declared
environment gate. This adds the real thing: the GitHub Deployments API, as a
provider-neutral fact kind, joined to runs and surfaced on the correlation read
model.

## The trap this design exists to avoid

A GitHub deployment carries no `run_id`. Verified against the published API:
`GET /repos/{owner}/{repo}/deployments` returns `id, sha, ref, task,
environment, original_environment, transient_environment,
production_environment, created_at, updated_at, statuses_url, …`, and
`GET /deployments/{id}/statuses` returns `id, state, environment,
environment_url, log_url, created_at, updated_at, …` with `state` one of
`error|failure|inactive|pending|success|queued|in_progress`.

This family's reducer joins on `(provider, run_id, run_attempt)`, and two paths
drop unjoinable evidence in silence:

- `projector/ci_cd_run_correlation_intents.go:70` raises a correlation intent
  only for a generation containing a `ci.run`. A generation of deployment events
  with no run produces no intent at all.
- `reducer/ci_cd_run_correlation_decode.go` skips any evidence bucket whose
  `run.FactID` is empty, with no quarantine, no metric, no dead letter.

A kind added the obvious way would commit facts, keep every gate green, and be
read by nothing.

## What was built instead

`ci.workflow_image_evidence` already solves this shape — repo-scoped evidence
attached to run-anchored decisions — so this mirrors it.

The fact carries no `run_id`. Deployments are fetched inside the same claim as
runs, so they land in the same `CollectedGeneration` by construction, and
`attachDeploymentEventsToRuns` joins each event to every run bucket whose
`CommitSHA` equals the event's `sha`. Runs sharing a head sha all receive the
event; the collector never guesses which run owns a deployment. The intent
trigger and the empty-run guard are both left exactly as they were.

Selection among attached events is deterministic rather than slice-order
dependent: `success` outranks `in_progress` outranks everything else, ties break
on `deployment_id` then `status_id`.
`TestSelectDeploymentEventIsDeterministicAcrossOrder` feeds the same events in
two orders and asserts the same winner.

One kind, one fact per status, parent fields denormalized onto each row —
matching `incident.lifecycle_event` and `work_item.transition`. The stable key is
`(provider, scope_id, repository, deployment_id, status_id)`, excluding `state`
and `updated_at`, so re-polling upserts and `pending → in_progress → success` is
three durable facts rather than one row overwritten. `scope_id` and `repository`
are in the key because GitLab deployment `iid`s are per-project.

Three failure modes are surfaced rather than swallowed: a truncated
deployments list reuses the partial-generation counter with a
`deployments_truncated` reason, an event whose sha matches no fetched run
emits a `deployment_unanchored` `ci.warning`, and a deployment whose own
status window was truncated emits a `deployment_statuses_truncated`
`ci.warning` (the deployment is present, but the transition carrying its
final state may be missing).

## The canonical-environment trap, found by the corpus

The first B-7 assertion filtered `environment=production` and returned zero rows.

The cassette fact says `production` because that is what the API returns, but the
reducer canonicalizes through `environment.Canonical` — `production → prod`,
`staging → stage`, `development → dev` — the same normalization the CI-declared
path already applies. **A consumer filtering on the provider's raw environment
string silently gets nothing back.**

That is worth knowing, so it is pinned in two places rather than patched around:
the corpus floor filters and asserts the canonical token, and
`TestCassetteShapedDeploymentEventsResolveCanonicalEnvironment` drives
cassette-shaped payloads through the correlation builder and asserts
`environment == "prod"` with `environment_evidence == "deploy_event"`.

That framework test should have caught this before the corpus did. The epic's
proof rule puts framework proof before corpus proof and I inverted it here;
the test now closes the gap in milliseconds instead of a three-minute run.

## Seam for #5426

The decision records `environment_evidence` as `deploy_event` or `declared` and
publishes it on the correlation payload and the HTTP read model. #5426 reads that
key into `supplyChainDeploymentContext` and branches at
`supply_chain_impact_runtime.go:68-71` instead of promoting `deployed_image` from
a declared free-text environment alone.

Two notes for whoever picks that up. There are two canonicalizers:
`environment.Canonical` (already used here) and the `canonicalEnvironmentName`
in `go/internal/query/compare_evidence.go` that #5426's issue text cites. And
`githubJob.Environment`/`DeploymentStatus` map to JSON keys the real jobs API
does not return, so `ci.environment_observation` appears to be fixture-only —
which would make deployment events the family's first real runtime environment
source rather than a second one.

## Performance

Collector cost is one extra list call plus N status calls per claim, N bounded
by `MaxDeployments` (default 10) — the same N+1 shape as the existing jobs and
artifacts fetches.

Reducer cost, `BenchmarkBuildCICDRunCorrelationDecisions`, 5,000 runs × 6 facts,
`-count=5`, same machine, same corpus, no deployment events (so this isolates
the cost of the new code merely being present):

| Metric | Before (`2e1560ff8`) | After (`0d42bacc1`) | Input shape |
| --- | --- | --- | --- |
| ns/op (median) | 166,864,631 | 164,070,089 | 5,000 runs, 30,000 facts |
| B/op | 19,355,638 | 19,757,606 | same |
| allocs/op | 260,064 | 260,064 | same |

| Metric | Before | After | Input shape |
| --- | --- | --- | --- |
| ns/op (median) | 1,259,760 | 1,266,078 | shared-repo workflow images |
| B/op | 1,222,262 | 1,262,485 | same |
| allocs/op | 9,821 | 9,821 | same |

The no-deployment-event benchmark measured -1.7% at the final head (164.07 ms
against 166.86 ms), inside run-to-run spread on both sides. An independent re-run of both
benchmarks resolved the same two deltas NEGATIVE (-0.72% and -0.20%, branch
faster), which is the clearest statement that these are noise and not a
regression: the sign is not stable between samples. **allocs/op is identical on
both benchmarks**, which is the load-bearing number: the extra bytes come from
the wider evidence struct, not from new allocations per fact.

The attach path doing actual work is measured separately, on its worst case —
`attachDeploymentEventsToRuns` joins on sha equality with no index, so a corpus
where every run shares one sha is quadratic:

| Metric | Before repo guard | After repo guard (`0d42bacc1`) | Input shape |
| --- | --- | --- | --- |
| ns/op (median) | 14,048,640 | 14,943,837 | 1,000 runs + 300 events, one repository, one sha |
| B/op | 10,963,045 | 10,963,048 | same |
| allocs/op | 21,529 | 21,529 | same |

300,000 sha comparisons in about 15 ms. A corpus where runs carry distinct shas
is linear; this is the number worth knowing because it is the ceiling.

The cross-repository guard costs **+6.4% on this path** (14.05 ms to 14.94 ms
median), and that is a real measured delta rather than noise: the two sample
sets do not overlap (13.965-14.114 ms before, 14.888-15.065 ms after). It is a
second field comparison per candidate event, so on the worst-case shape — every
event a sha match, 300,000 candidate pairs — it lands where you would expect.
allocs/op is unchanged at 21,529, so the cost is comparison work, not
allocation.

That is accepted deliberately. The guard closes a latent cross-repository
join: a commit sha is only unique within a repository, and the fact already
carried `RepositoryID`. 0.9 ms on the quadratic worst case, on a path that runs
once per reducer intent, is a fair price for not silently attaching one
repository's deployment to another repository's run. It is recorded here rather
than smoothed over, so a future reader deciding whether to keep the guard has
the number.

No-Observability-Change: no new metric names. The truncation path reuses the
existing partial-generation counter with a new reason label, and the unanchored
path emits a `ci.warning` fact rather than a metric. Every new non-test file
under `go/internal/**` has a `telemetry-coverage.md` row naming the existing
`eshu_dp_ci_cd_run_*` instrument that covers it — the gate the row commitment
is for only covers `go/internal/**`, not the new file under `sdk/go/factschema`.

## Verification

```
$ cd go && go test ./internal/reducer ./internal/query ./internal/mcp \
    ./internal/projector ./internal/collector/... ./cmd/reducer -count=1
ok — all packages

$ cd sdk/go/factschema && go build ./... && go test ./... && go generate ./... && git status --porcelain -- .
(empty — generators idempotent)

$ cd go && go run ./cmd/fact-kind-registry -repo-root .. -spec ../specs/fact-kind-registry.v1.yaml -check
fact-kind-registry: generated artifacts are current

$ cd go && go run ./cmd/capability-inventory -mode verify
capability catalog and surface inventory verified

$ bash scripts/verify-payload-usage-manifest.sh
ok

$ bash scripts/verify-telemetry-coverage.sh
verify-telemetry-coverage: docs and instruments agree, no new untracked stages

$ bash scripts/verify-openapi.sh
OpenAPI surface clean: 252 HandleFunc routes, 252 OpenAPI path entries
```

B-7 golden corpus gate against `0d42bacc1`:

```
summary: 506 pass, 0 required-fail, 2 advisory-warn
=== PASS: B-7 golden corpus gate green (elapsed 158s, budget ceiling 1800s) ===

[PASS] GET /api/v0/ci-cd/run-correlations?environment=prod&limit=10&repository_id=repository:r_69256c06:
       "correlations" has 1 results; item fields [environment environment_evidence] present;
       values [correlations[].environment correlations[].environment_evidence]
```

Both advisory warnings are phase timing only; `phase_collect` reflects the
`GATE_COLLECTOR_SETTLE_SECONDS=75` override this run set, not a pipeline change.

The floor is non-vacuous by construction: before this change no cassette in the
repo carried an environment at all, so an environment-filtered read returned
zero for every argument. Its first run proved that — it went red.

## Known limitation

A newest-id watermark detects missed deployments but cannot see a new status
appended to an older deployment outside the fetched window, because statuses
append to parents that may have scrolled out. This is a bounded correlation gap
of the same class as the Lambda `code_sha256` gap in #5454, stated here and in
the package README rather than hidden behind a heavier cursor.

`deploymentEventEnvelope` (`go/internal/collector/cicdrun/github_actions_deployments.go`)
deliberately denormalizes the parent deployment's `environment` onto every
status row and does not decode a `deployment_status` event's own
`environment` field (`githubDeploymentStatus` in `types_deployments.go`
intentionally omits it). GitHub allows a status event to report a different
environment than its parent deployment. Since the winning event's
`Environment` becomes deployment truth
(`classifyCICDDeploymentEventEnvironment`), a status-level environment
override is invisible to the correlation today: the run resolves to the
parent deployment's environment even when a later status reported a
different one.

`unanchoredDeploymentWarnings` (`go/internal/collector/cicdrun/ghactionsruntime/source_deployments.go`)
compares each fetched deployment event's sha only against `runHeadSHAs`, the
runs fetched in the SAME claim window. The reducer's
`attachDeploymentEventsToRuns` attaches across every active run fact in
scope, not only the current claim's window. In steady state, a deployment
whose sha belongs to an older run outside this claim's `max_runs` window is
flagged `deployment_unanchored` even though the reducer will still attach it
correctly once that older run's facts are in scope. This is a false-positive
operator signal, not a correlation gap — the same limitation is called out in
`go/internal/collector/cicdrun/ghactionsruntime/README.md`.
