# Dashboard Correctness and Bounded-Read Evidence

This note records the focused local proof for issues #5240, #5242, #5244,
#5245, #5248, #5249, #5250, #5251, and #5253. Ask Eshu's exact
repository-count contract for #5246
lives with its owning package in
[`go/internal/ask/engine/README.md`](../../../go/internal/ask/engine/README.md#exact-indexed-repository-counts).
The browser-session proof for #5240 is intentionally limited to the supported
local and hosted-single-tenant governance modes. Hosted multi-tenant and unknown
governance modes remain fail-closed; this change does not claim cross-tenant
graph isolation.

Admin audit entries for #5242 now receive stable event keys derived from their
record identity instead of colliding on the action label. The focused browser
session route-policy and audit-key tests cover duplicate actions, owner access,
and the fail-closed hosted governance modes. Issue #5249 is covered by the
state-specific live route probes in the final browser gate: a page shell is not
accepted as proof that its requested data populated.

## Final live-console and corpus proof

Performance Evidence: the accepted no-index retained-stack browser run claimed
its isolated identity surface through the normal setup wizard, then executed
all 39 catalogued route/action workflows with that same owner browser-session
cookie. All 39 passed in 116.642 aggregate route seconds. Code Graph passed in
9.351 seconds with twelve owned HTTP responses, all status 200, and zero
console errors. Service Catalog passed in 10.075 seconds, Vulnerabilities in
11.034 seconds, Repositories in 8.535 seconds, Ask Eshu in 10.355 seconds with
the exact 887-repository result, Relationships in 3.639 seconds, Replatforming
in 2.711 seconds, Dead Code in 2.519 seconds, Cloud Drift in 2.801 seconds,
Semantic Search in 2.631 seconds, and Secrets/IAM in 2.362 seconds. The Code
Graph route finished 7.649 seconds inside the runner's 17-second live-browser liveness
cutoff. That cutoff is a harness timeout, not portable performance acceptance:
this proof did not record a machine resource envelope or classify an absolute
target as applicable. The runner's fixed settle/quiet windows, workflow
actions, and screenshots also mean route duration is not API latency.

The first correction run exposed two bootstrap requests aborted when the
runner reset the wizard's still-settling dashboard. The runner now installs
request ownership before setup and refuses to start retained route proof until
the wizard dashboard is quiet. The rerun completed 261 bootstrap requests with
zero aborted or unexpected requests. The durable report retains the first 200
query-free request observations and marks the bootstrap as truncated.

The final API used immutable image id
`sha256:7d1dbb628e57bea37ece63086c961fdade2a291095b69daf4df3f60ae5b33bb4`.
Its binary reports
`proof-ce7c829ea06ac809a6f9d7fb67ef2b8c2172056989ccc07f892807a1c884d8ba`,
which is the SHA-256 manifest of the exact Dockerfile, Go, and local Go SDK
inputs copied by the image build. The corrected browser/runner input manifest
was `160378744517e8ef9c646feaad8e2737f0b023b2d0ce2e9bbe3f2d86f441d8a9`.
The sidecar read the unchanged retained Postgres volume and NornicDB image
`timothyswt/nornicdb-cpu-bge:v1.1.11@sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090`.
`SHOW INDEXES` verified that the rejected `function_legacy_id` index count was
zero after the accepted run.
The proof manifest recorded the operator-attested corpus label
`b09799951df867a5bb5517b7d3cb9657b152b7cf2d54504f03d7e9ce4b4d62ba`:
887 repositories, 961,472 graph nodes, 1,180,403 graph edges, and 7,384,555
Postgres facts at evidence capture. The label was not derived or
authoritatively validated by the runner. The runner independently read the
authoritative repository inventory and failed closed unless its total equaled
the declared 887 repositories.
The original retained API and console remained running on their prior image and
port throughout. The final proof used isolated console port `5182` and API port
`18123`, then removed only that temporary sidecar and auth schema. No database,
graph, collector, reducer, or projector was restarted.

No-Regression Evidence: a failing-first lifecycle regression reproduced the
external-review defect: public-identity verification exited `73`, after which
the prior EXIT trap leaked its owned sidecar. The corrected matrix proves both
explicit and cleanup-time verification failures, original-versus-cleanup exit
status precedence, keep-for-evidence behavior, continued schema cleanup after
sidecar-removal failure, terminal schema-drop failure, and suppression of a
false PASS marker. The real `final5240ad` proof then removed its temporary
sidecar and schema after verifying the shared public identity digest.

The identity and sidecar proof used these local-only commands. Credentials and
retained anchor values were passed only at runtime and omitted from committed
evidence:

```bash
API_INPUT_HASH="$({ printf '%s\0' Dockerfile; \
  git ls-files -z -co --exclude-standard -- go sdk/go; } | sort -z | \
  xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"
RUNNER_INPUT_HASH="$({ printf '%s\0' package.json package-lock.json; \
  git ls-files -z -co --exclude-standard -- \
  apps/console scripts/run-console-live-e2e.sh \
  scripts/run-console-retained-e2e.sh scripts/console-live-e2e-runtime.mjs \
  scripts/lib/console-retained-create-proof-schema.sql \
  scripts/lib/console-retained-verify-public-identity.sql; } | \
  sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"
ESHU_ASK_ENABLED=true \
  ESHU_ASK_NARRATION_ENABLED=true \
  ESHU_SEMANTIC_PROVIDER_PROFILES_JSON="$ASK_PROVIDER_PROFILE_JSON" \
  ESHU_E2E_RETAINED_PROOF_ID=final5240ad \
  ESHU_E2E_RETAINED_API_PORT=18123 \
  ESHU_E2E_CONSOLE_PORT=5182 \
  ESHU_E2E_WIZARD_NEW_PASSWORD="$LOCAL_PROOF_PASSWORD" \
  ESHU_E2E_CORPUS_ATTESTATION="$CORPUS_ATTESTATION" \
  ESHU_E2E_CORPUS_REPOSITORY_COUNT=887 \
  ESHU_E2E_INCIDENT_ID="$INCIDENT_ID" \
  ESHU_E2E_SERVICE_NAME="$SERVICE_NAME" \
  ESHU_E2E_SECRETS_SCOPE_ID="$SECRETS_SCOPE_ID" \
  ESHU_E2E_CLOUD_SCOPE_ID="$CLOUD_SCOPE_ID" \
  ESHU_E2E_AWS_SCOPE_ID="$AWS_SCOPE_ID" \
  ESHU_E2E_SEMANTIC_REPOSITORY_ID="$SEMANTIC_REPOSITORY_ID" \
  ESHU_E2E_SEMANTIC_QUERY="$SEMANTIC_QUERY" \
  scripts/run-console-retained-e2e.sh
```

The rebuilt MCP server advertised 159 tools. Two bounded
`list_indexed_repositories` proof calls included a 1.508471-second cold call and
a 0.035871-second warm call, returned one of 887 repositories with
`truncated=true`, and carried an exact, fresh, production structured envelope.
The exact Ask prompt returned the same authorized total through the browser API
workflow in 10.355 seconds and the standalone MCP transport in 3.022045 seconds;
both paths returned deterministic truth, `{total: 887}`, and
`eshu://api-result/repositories`. The MCP image was
`sha256:013e3a86060c2befffaff0a5e205c8b53cbcff65522570b886b21d99f83fa1b2`
and reported the same
`proof-04b361b18575c62b380248ca35a5789df8317d3773ed13498de31825fafaa59c`
binary manifest. That bounded MCP proof predates only the proof-artifact
isolation and all-scope semantic-search routing follow-ups; neither changes the
measured `list_indexed_repositories` or Ask dispatch. The final API/browser proof
is bound separately to the current `ce7c829e...` source manifest above. The
retained API and MCP remained healthy after the run; Postgres, NornicDB,
collectors, reducer, and projector were not restarted for the read-serving
rollout.

### Relationship catalog concurrency proof

Performance Evidence: a retained-data shim ran the production catalog's 16
typed relationship counts plus seven source-tool breakdown queries with
fan-outs 1, 2, 3, 4, and 7. Fan-out 1 completed in 5.886843 seconds, fan-out 2
in 4.927239 seconds, fan-out 3 in 3.266612 seconds, and fan-out 4 in 1.340105
seconds, all with zero failures. Fan-out 7 returned in 0.806670 seconds only
because two breakdowns failed with Bolt `ConnectivityError: EOF`; that result
is rejected as a faster wrong path.

Accuracy Evidence: the serial baseline and shared-four candidate returned the
same 16 ordered verb tiles with a bidirectional result diff of `0/0`. Two
simultaneous callers sharing four handler-wide slots completed five shim rounds
with zero failures in 1.491540–3.919452 seconds. The production function then
completed five two-caller rounds in 2.933722–5.288262 seconds, each with zero
failures and exact diff `0/0`. Finally, the exact built API returned 200 for 20
of 20 catalog calls across five four-caller rounds and passed both browser
catalog requests in the 39-route gate. Reducer, collector, queue, and graph-write
worker counts were not changed; only these heavy read-side breakdowns share the
measured four-slot cap.

Observability Evidence: the unchanged four-slot limiter now records the
label-free `eshu_dp_relationship_breakdown_permit_wait_seconds` histogram plus
current `eshu_dp_relationship_breakdown_queued` and
`eshu_dp_relationship_breakdown_in_flight` up/down counters. Existing HTTP,
graph-query, error, duration, and truth-envelope telemetry remains available;
no retry, swallowed error, stale cache, or serialized fallback was added.

No-Regression Evidence: `scripts/verify-golden-corpus-gate.sh` passed 418
assertions with zero required failures and zero advisory warnings. The
20-repository pipeline completed in 37 seconds, including an 8-second first
drain, 3-second graph/query phase, and the API/MCP truth checks. The console
unit/component suite passed all 1,342 tests across 212 files; strict
application/E2E TypeScript compilation,
the production console build, and all 73 bundle budgets passed.

No-Observability-Change: the browser authorization and dashboard presentation
fixes retain the existing API, MCP, graph-query, and auth telemetry contracts.
The default live gate now reports one browser-session verdict over all 39
routes. Bearer mode remains an explicit diagnostic-only option and reports its
Profile/Admin exclusions rather than counting them as passes. The
semantic-scope Postgres instrumentation added elsewhere in this change is
documented in its owning evidence note rather than hidden under this marker.

The final retained browser gate ran with explicit real-data anchors and no
checked-in identifiers or credentials:

```bash
ESHU_CONSOLE_E2E_ENV_FILE=/dev/null \
ESHU_E2E_AUTH_MODE=browser_session \
ESHU_E2E_API_BASE="$RETAINED_SESSION_API_BASE" \
ESHU_E2E_POSTGRES_DSN="$ISOLATED_AUTH_RETAINED_DATA_DSN" \
ESHU_AUTH_SECRET_ENC_KEY="$AUTH_SECRET_ENC_KEY" \
ESHU_E2E_WIZARD_NEW_PASSWORD="$LOCAL_PROOF_PASSWORD" \
ESHU_E2E_INCIDENT_ID="$INCIDENT_ID" \
ESHU_E2E_SERVICE_NAME="$SERVICE_NAME" \
ESHU_E2E_SECRETS_SCOPE_ID="$SECRETS_SCOPE_ID" \
ESHU_E2E_CLOUD_SCOPE_ID="$CLOUD_SCOPE_ID" \
ESHU_E2E_AWS_SCOPE_ID="$AWS_SCOPE_ID" \
ESHU_E2E_SEMANTIC_REPOSITORY_ID="$SEMANTIC_REPOSITORY_ID" \
ESHU_E2E_SEMANTIC_QUERY="$SEMANTIC_QUERY" \
npm run console:e2e
```

## Repository workload service-selector truth

Accuracy Evidence: repository story responses already expose workload names at
`deployment_overview.workloads`. The console now consumes that production wire
field instead of relying on a fabricated top-level `service_identity`. A
production-shaped regression proves a real workload name requests its service
story and populates repository service context.

The first final-source retained browser run then exposed an additional real
shape: a repository can carry an internal reducer workload identity beginning
with `reducer_` and containing `_workload_identity_workload_`. Treating that
opaque identity as a service selector produced four HTTP 404 responses. The
exact retained pattern failed its regression before the classifier correction.
After the correction, the accepted repository workflow completed in 8.535
seconds with zero console/network errors and response-backed workspace truth.
Human workload names still load service story context. A reducer-owned opaque
identity is skipped even when it appears before a later valid workload; that
later workload supplies the service selector.

No-Observability-Change: this changes only console selection of an existing
repository-story field. It adds no API contract, backend read, telemetry,
persistence, queue, reducer, or graph-write behavior.

## Changed Since retained-baseline selection

No-Regression Evidence: the empty Changed Since route no longer searches only
the newest global lifecycle page. It checks exact repository lifecycle scopes,
three rows at a time, and stops at the first active/prior pair. Discovery is
capped at 25 repositories and continues past stale catalog entries without
inventing a baseline. Superseded and completed generations are preferred; a
failed predecessor remains a valid exact fact-record baseline when it is the
only retained predecessor, and its lifecycle status remains visible to the
operator.

The first live proof exposed the real failure: `latest_failure` is an object in
the generation-lifecycle wire contract, while the console adapter attempted to
parse it as a string. Every repository with a retained failed generation was
therefore rejected before comparison. The adapter now extracts the failure
message (or class when no message exists). The focused regression and selection
suite passed 27 tests. Against the retained 887-repository catalog, the helper
selected an exact active/prior pair in 17 ms. The changed-since read completed
in 18.2 ms with 16 changed and 6 unchanged facts; the final browser workflow
completed in 2.231 seconds with seven requests and zero console/API errors. The
prior retained run issued 12 requests because React Strict Mode created two
five-request discovery owners; the final component keeps one owner and removes
that duplicate five-request batch without reducing discovery concurrency.

The default discovery now probes at most five repositories concurrently while
preserving catalog-order selection within each batch. One 15-second budget
cancels every in-flight lifecycle request, and page unmount aborts the same
request signal. The worst case is therefore one bounded 15-second discovery,
not 25 sequential 15-second waits (up to 375 seconds). Tests cover concurrency,
the total deadline, caller cancellation, request-signal propagation, and
fractional concurrency below one. React Strict Mode replay no longer aborts
five valid lifecycle reads mid-flight or starts a second owner; a real scope
change or unmount still cancels every stale outstanding request. Production
component tests prove one Strict Mode owner, at most five concurrent probes,
at most 25 total probes, and immediate stale-owner cancellation.

```bash
./node_modules/.bin/vitest --config apps/console/vite.config.ts run \
  src/api/changedSince.test.ts \
  src/console/defaultEntity.test.ts \
  src/pages/changedSinceDefault.test.ts \
  src/pages/ChangedSincePage.test.tsx \
  src/pages/ChangedSincePage.lifecycle.test.tsx \
  src/api/client.test.ts \
  src/api/suggestedQuestions.test.ts \
  src/pages/DashboardPageSuggestedQuestions.test.tsx
```

No-Observability-Change: this corrects bounded lifecycle parsing and default
selection without changing the generation or changed-since API contracts,
telemetry, persistence, queue behavior, or reducer concurrency.

## Incoming NornicDB relationship read (#5244)

The incoming half of `GET /api/v0/code/relationships` now anchors the exact
target entity before expanding the incoming relationship. It retains the same
relationship types, `uid`-then-`id` fallback, row behavior, and source/repository
hydration.

Performance Evidence: on the retained NornicDB v1.1.11 graph, the old incoming
shape did not finish within either a 15-second or 25-second proof timeout for a
known Function target. The target-first shape completed in 0.563 to 1.648 ms on
the same target. Both normalized result sets contained one relationship;
old-minus-new and new-minus-old were `0/0`. This is a bounded interactive read
win, not a claim against the historical #3624 bootstrap target.

This focused one-relationship production-path proof is distinct from the
earlier raw-Cypher registry measurement in `QP-CODE-REL-STORY-INCOMING`, which
used another retained Function target with eight incoming edges and measured
the new query itself at 0.044 seconds. The targets, result cardinalities, and
timing boundaries differ, so those measurements are evidence for the same
anchor shape but are not interchangeable latency samples.

The focused contract command pins exact target matching, prefix-collision
avoidance, empty and duplicate behavior, recursive self-edges, the fallback,
and hydration:

```bash
cd go && GOCACHE=/tmp/eshu-5244-gocache go test ./internal/query \
  -run 'TestNornicDB(IncomingOneHop|OneHopRelationships)' -count=1
```

No-Observability-Change: the route retains the existing graph query tracing,
duration histogram, query trace, and truth envelope. The query reversal adds no
graph write, worker, queue, metric, span name, response field, or runtime knob.

## Cross-route bounded-read attribution and closeout (#5244)

The authenticated browser runner now records cold-bootstrap and per-navigation
first-useful-content and API-quiet readiness times, exact request count,
normalized duplicate groups, encoded bytes, concurrency/ordering, slowest
request, TTFB, download time, and post-response browser work. Dynamic scope and
entity values are redacted; headers, bodies, cookies, credentials, and query
values are never written. `ESHU_E2E_ROUTE_PATHS` permits an exact ordered,
fail-closed diagnostic subset, including repeated paths for cold/warm ownership
comparison, without weakening the complete acceptance gate.

Performance Evidence: the relationship catalog previously issued seven
independent source-tool breakdown reads. On the same retained graph, that exact
shape took 10.950808s. One grouped aggregate across fixed source-owner labels took
1.199711s, returned the same ten rows, and produced the same normalized digest.
The final production shape issues one independent aggregate per source-owner
label under the existing process-wide four-permit cap. The two current owner
reads completed in 1.177100s versus 1.317921s for the grouped call, with the
same ten rows and bidirectional diff `0/0`. Five concurrent callers prove that
four owner reads enter, the fifth waits, cancellation releases its permit, and
every slot remains reusable.

The bootstrap Argo CD snapshot also had a seconds-scale query-shape defect. Its
general `MATCH (n)` plus two-label OR returned the retained 365-row universe in
5.086224s. Two direct, bounded label reads completed in 0.035001s; the second
excludes dual-labeled nodes, and the merged universe kept 365 rows, zero
duplicates, and bidirectional diff `0/0`. Production applies this path only to
the category-only snapshot body, carries the same repository-access predicate
on both reads, fetches at most `limit + 1` per label, and performs one globally
ordered bounded merge. Additional filters keep the general search path.

The multi-type Function story previously paid an unindexed legacy-`id`
collision scan despite finding the canonical `uid`. The content entity ID is
now authoritative: production resolves `uid` first and consults legacy `id`
only when no canonical anchor exists. On a fresh retained Function, the final
UID-first shape completed in 0.009856s with 13 graph calls and one row; the
prior collision-check path took 4.424432s and 14 calls, with ordered diff
`0/0`. A separate post-restart empty-edge target completed in 0.031363s versus
4.955485s with the same 13/14-call split.
Unit tests cover canonical, legacy-ID-only, missing, and unrelated-ID-collision
states. A target-specific startup warmup was measured and rejected because it
did not help another selected Function and delayed readiness.

The follow-on exact browser workflow localized three additional original-route
owners rather than accepting a single green story call as route proof.
Repository-scoped entity resolution and exact code search previously started
from the entity universe and applied repository membership late. On the same
retained entity, the old resolver query took 35.800930s and the
repository-property-anchored replacement took 0.002801s; both returned one
identical ordered row, diff `0/0`. Canonical `content-entity:` resolution now
uses its authoritative content row directly, including an explicit empty result
when that row is missing. The direct-relationship graph-only fallback uses
fixed supported labels for its `uid` then legacy-`id` probes instead of an
unlabeled property scan. The two retained missing-identity probes returned zero
rows in 0.404645s and 0.188298s.

The final exact-image API returned canonical entity resolution in 0.004492s,
exact repository code search in 0.128055s, missing direct relationships as HTTP
404 in 0.623642s, and the 16-verb relationship catalog in 0.903421s. The
matching MCP transport returned structured, non-error results for
`get_code_relationship_story` in 0.011377s and `trace_exposure_path` in
0.002798s. The first direct-relationship compatibility control on the fresh API
process took 5.334448s; five immediate settled-stack repeats took
0.002036-0.002547s with the same empty relationship set. That endpoint is no
longer a Code Graph route owner: the story response owns the typed graph and
source evidence. Every route-owned API/MCP surface completed within the 2-3
second interactive budget on the exact image.

The original exact-workload `POST /api/v0/infra/relationships` control also
returned HTTP 200 in 0.017140s, followed by 0.001938s and 0.004079s repeats,
with the same valid empty relationship set. That handler and its query shape did
not change in this leaf. The final same-input result therefore contradicts an
independent seconds-scale infra-relationship defect and supports classifying
the original greater-than-20-second observation as shared graph contention from
the preceding unbounded/fallback reads. The original request lacked trace
correlation, so the evidence does not claim a more specific historical owner.
No speculative infra-relationship rewrite was added to manufacture a speedup.

Preliminary review also found two harness defects before promotion. The report
redactor now normalizes dynamic service-investigation, invitation-revocation,
identity-provider mapping, and API-token revocation paths, including encoded,
mixed, and numeric identifier fixtures while retaining static route segments.
Workflow milestones can now name pre-interaction useful content explicitly;
Repositories uses its view selector and Admin uses its sign-in-policy tab, so
first-useful timing cannot deadlock on UI that the workflow has not clicked yet.

The retained schema delta adds only
`nornicdb_function_legacy_id_lookup` for the legacy-ID-only fallback. Before the
delta the index was absent; the missing-only bootstrap wrapper executed one DDL
statement in 16.056306s, then an immediate repeat issued zero DDL statements.
The current NornicDB fingerprint is
`cfff663a3a7cae4e7c36823e0304b25f7f046eed2e139951e8a9bf8121b9ba69`
with 290 statements, and the immediately preceding fingerprint remains writer
compatible.

The Code Graph client no longer chains a broad untyped relationship request
after the story. Bootstrap dead-code owns the selected candidate and its source
location; the story owns all six typed edge families, provenance, and related-
node source metadata. The independent import-cycle read remains route owned.
In-flight request coalescing covers Strict Mode replays without retaining stale
results or crossing client/auth boundaries.

The exact rebuilt API and corrected diagnostic runner then passed all four
selected workflows against the retained 887-repository corpus. The final source
manifest was `2f52aee83e136c8aa98eb0dea9ed687d77f6bd673ccb3167e30b056bd15de502`
and its API image digest was
`sha256:d490a0b1f8cb0259a4bbd043dee44f72360c27266f7cb1dc28680c6eb387d24f`.
Code Graph reached first useful content in 60 ms and API-quiet readiness in
1.561s; the workflow verdict completed in 1.702s with exactly two concurrent
requests, zero duplicates, 11,821 encoded bytes, and no console or network
errors. Its slowest owner completed in 14 ms. Relationships reached first useful
content in 1.305s and API-quiet readiness in 2.806s; the workflow verdict
completed in 2.887s. Its single catalog request took 967 ms and
transferred 2,924 bytes. The owning API work and useful-paint milestones meet
the checked-in 2-3 second interactive target; the runner's fixed 1.5-second
quiet window intentionally keeps route readiness later than useful paint.

The same authenticated run proved the pre-interaction milestones rather than
merely unit-testing their selectors. Repositories reached useful content in
24 ms and API-quiet readiness in 1.525s before its retained-data interaction
workflow completed in 8.738s. Admin reached useful content in 813 ms and
API-quiet readiness in 2.314s before its sign-in-policy workflow completed in
3.017s. All four workflows passed with zero console or network errors. The
Repositories and Admin workflow durations include their deliberate multi-step
interactions and are not substituted for their route-ready measurements.

The diagnostic packet also exposed and then proved redaction of Vite `/@fs/`
module URLs to `/@fs/:local-module` plus the dynamic identity-bearing routes
listed above. A post-fix scan found no local path, repository ID,
content-entity ID, credential, or unredacted dynamic route value in the durable
JSON report. The final settled-stack hard load reached first useful content in
1.409s and API-quiet readiness in 4.245s. It recorded 46 requests, 13 normalized
duplicate groups, 2,433,981 bytes, maximum concurrency 19, and a 1.545s
supply-chain impact-findings request as the longest owner. This final load is
not compared as a speedup against a clean graph restart; the earlier retained
cold-reset measurements below remain the repository-inventory theory proof.

Repository-list telemetry attributed 1.729s of that cold request to its bounded
dependency-cluster prepass; repeated prepasses completed below one millisecond.
The cheapest exact-query shim disproved overlapping the repository page and
cluster reads: on the retained corpus the cold sequential pair completed in
1.294344s and the cold parallel pair in 1.299524s. Both returned 101 repository
rows and 29 dependency edges; five warm iterations preserved byte-equivalent
results while the parallel shape saved only 1.9-6.4ms. Production therefore
keeps the sequential backend-safe flow. A cache was not added because the
required graph-generation invalidation, tenant boundary, negative-result,
stampede, and API/MCP consistency contracts are not yet proven. The remaining
cold repository inventory and duplicate-session-read work is a distinct
cross-route long pole rather than evidence that either selected route regressed.

Observability Evidence: the catalog keeps its handler and graph-query telemetry
and exposes label-free permit wait, queued, and in-flight instruments at the
single grouped-aggregate chokepoint. The browser packet attributes request and
render stages without adding secret-bearing traces. UID-first resolution adds
no new metric or span; each graph call remains covered by existing duration and
error telemetry.

## Semantic-search scope and readiness (#5245)

The retained-data cardinality, old/new SQL, exactness matrix, measured timings,
concurrency analysis, commands, and structured markers are recorded in
[`go/internal/storage/postgres/evidence-5245-semantic-search-scope-readiness.md`](../../../go/internal/storage/postgres/evidence-5245-semantic-search-scope-readiness.md).
That proof reports a `0.101 ms` canonical-id resolver, an eight-to-zero
false-pending correction, live BM25 visibility, production pgx vector decoding,
and exact/fail-closed scope behavior. It does not claim a seconds-scale win.

## Dead-code Trait identity and scan bounds (#5248)

No-Regression Evidence: the query now preserves Trait identity instead of
mapping it to Function, keeps repository scope on content rows, and reports the
shared 2,500-row maximum separately from the maximum share one label may use. An
optional exact `candidate_kind` request narrows the raw scan to one advertised
kind. Unsupported values return `400`, and the content reader rejects an
unknown kind before issuing SQL instead of silently querying Functions.

The retained Postgres read model contained 53 Trait entities. The rebuilt API's
`{"candidate_kind":"Trait","limit":100}` request returned 22 dead-code rows;
all 22 were Traits, zero rows leaked another kind, and every returned
`entity_id + repo_id + relative_path` identity matched the direct Postgres Trait
row. The live browser then selected the Trait control, observed the exact
`POST /api/v0/code/dead-code` request with `candidate_kind=Trait`, and rendered
the same 22 exact-kind rows in 2.519 seconds. The prior unscoped 100-row response
contained 100 Functions and no Traits, so the new server-side filter proves the
intended correctness delta rather than hiding it behind a client-side first-page
filter.

Performance Evidence: five warm retained requests measured the unscoped shape
at 48.621/59.878/73.625 ms min/mean/max while scanning 250 rows under a
2,500-row shared bound. The exact Trait shape measured
6.165/10.724/19.518 ms while scanning all 53 Trait rows under the true
2,500-row one-kind bound. A production-scanner saturation shim separately
proved the rejected per-label schedule would expand all three dead-code routes
from 2,500 rows / 10 hydration and reachability pages to 15,000 / 60, while the
final shared round-robin schedule reaches all six labels at 2,500 / 10. The
full table is in
[`go/internal/query/evidence-5248-dead-code-round-robin.md`](../../../go/internal/query/evidence-5248-dead-code-round-robin.md).
This is a correctness and bounded-work improvement,
not a seconds-scale performance claim.

Focused handler, content-reader, and OpenAPI tests run with:

```bash
cd go && GOCACHE=/tmp/eshu-5248-gocache go test ./internal/query \
  -run 'Test(DeadCodeCandidateEntityTypeMapsEveryAdvertisedLabel|ContentReaderDeadCodeCandidateRows|HandleDeadCode|DeadCodeScanRestrictsWorkAndMetadata|OpenAPIDeadCode)' \
  -count=1
```

The changed golden snapshot and its static contract run with:

```bash
(cd go && GOCACHE=/tmp/eshu-5248-gocache go test ./cmd/golden-corpus-gate -count=1)
bash scripts/test-verify-golden-corpus-gate.sh
```

No-Observability-Change: no metric, span, log field, queue, worker, graph write,
or runtime knob changed. Existing dead-code query spans and candidate-scan
metadata remain the diagnostic surface; the response now distinguishes the
maximum share per label from the shared total maximum.

## Operations response negotiation (#5250)

No-Regression Evidence: the operations handler now uses shared success-response
negotiation. Envelope clients receive `{data, truth, error}`, while legacy
`application/json` clients receive the same unwrapped operations object. The
canonical response carries `operations.status`, `exact`, `production`,
`runtime_state`, and `fresh` truth metadata. The rebuilt live route returned
that exact envelope and rendered the Operations board in 1.957 seconds. The
focused handler and console client tests run with:

```bash
cd go && GOCACHE=/tmp/eshu-5250-gocache go test ./internal/query \
  -run '^TestGetOperationsNegotiatesEnvelopeAndPreservesLegacyRawJSON$' -count=1
npm --prefix apps/console test -- src/api/operationsBoard.test.ts
```

No-Observability-Change: the route keeps its existing status reads, HTTP route
attribution, and error reporting. No backend read, metric, span name, log field,
runtime knob, or polling interval changed.

## Vulnerability and findings empty-state truth (#5251)

No-Regression Evidence: an empty reachable-vulnerability result now says that
no affected service was proven by current impact evidence; it no longer
mislabels that result as a missing intelligence collector. Partial failure and
loading states keep available findings visible while naming the unavailable or
pending source. The focused UI tests run with:

```bash
npm --prefix apps/console test -- \
  src/pages/VulnerabilitiesPage.test.tsx src/pages/FindingsPage.test.tsx
```

Production-shaped adapter regressions also prove that two distinct
`finding_id` values sharing one advisory remain separate with their own
package, version, fix, repository, and service evidence. Duplicate exact and
derived rows for the same `finding_id` merge only their service evidence.
Advisory identifiers remain unchanged for labels and detail URLs, while React
and worklist keys use finding identity; advisory fallback detail unions all
affected packages and services.

The retained stack also ran the public OSV-only vulnerability collector with
no private token. The API returned five advisory catalog rows, and an exact
advisory detail read returned HTTP 200 with one source. The final browser route
rendered those real catalog and detail surfaces in 11.034 seconds with five
requests and zero errors. The retained impact-finding routes authoritatively
returned zero rows, and the browser proved the exact no-impact state rather
than accepting a generic empty page.

The production impact-finding wire field is `service_ids`. The console now
normalizes every value, removes duplicate service labels, and merges service
evidence when the same `finding_id` appears in both the exact and derived
bounded responses. A failing-first fixture proved the old adapter discarded those
services and substituted the repository label; the corrected full console suite
passes 1,314 tests across 204 files.

No-Observability-Change: this is a render-state and copy correction over the
existing model provenance. It adds no request, collector, metric, span, log
field, runtime knob, or persisted state.

## Response-backed Code Graph, Findings, Relationships, and Cloud Drift (#5249)

No-Regression Evidence: generic page shells no longer satisfy the live browser
gate. Code Graph requires successful `200` responses from bootstrap dead-code,
route-owned relationship-story, and import-investigation reads before its
canvas counts. The story owns typed edges, provenance, and related-node source
metadata, so the current route does not issue the former redundant untyped
relationship read. Relationships requires the catalog response plus visible verb
rows. Findings requires response-backed source readiness and either real row
cardinality or the exact authoritative empty marker. Adversarial tests prove a
generic SVG, empty shell, or always-rendered table cannot pass alone.

The accepted fingerprinted-image retained proof observed all four Code Graph
responses and one visible canvas in 9.351 seconds; the relationship catalog
response and 16 verb rows in 3.639 seconds; and three bootstrap-snapshot source
responses plus 25 Findings rows in 1.758 seconds. Each workflow recorded the
accepted method, path, status, and owning bootstrap or route phase in the
durable report. A route-owned expectation cannot borrow a matching bootstrap
response.

Cloud Drift now distinguishes `not_requested`, `loading`, `loaded`, and
`unavailable`; only a completed authoritative response may render zero. The
retained database had 19 active AWS scopes and 3,271 active drift findings. The
largest real scope contained 1,824 findings. Its browser workflow observed
HTTP 200 from the multi-cloud, AWS drift, unmanaged-resource, and Terraform
import-plan endpoints; rendered the authoritative multi-cloud empty row, 50
bounded AWS rows, 50 bounded unmanaged rows, and a loaded import-plan state;
and completed in 2.801 seconds with no console or network error.

No-Observability-Change: these changes harden browser proof and render-state
provenance. They add no runtime metric, span, log field, queue, worker, graph
write, collector, or concurrency knob.

## Cloud inventory active-generation readback (#5253)

The cloud inventory list now joins reducer-owned identity facts to each scope's
active generation before pagination. It does not collapse duplicate identities
inside one active generation.

Performance Evidence: on retained local Postgres data, the old query read 5,955
rows for 3,271 identity keys; its first 50-row page contained 28 unique
`cloud_resource_uid` values. `EXPLAIN (ANALYZE, BUFFERS)` reported 6,174 shared
buffer hits, 4.146 ms planning, and 10.437 ms execution. The active-generation
query returned 3,271 rows, produced 50 unique identities on its first page, and
reported 3,237 shared-buffer hits, 1.721 ms planning, and 3.990 ms execution.
Both shapes used the same warm data and the same fact-kind, tombstone, ordering,
limit, and offset predicates.

The focused query-shape and handler command is:

```bash
cd go && GOCACHE=/tmp/eshu-5253-gocache go test ./internal/query \
  -run 'TestCloudInventory(ReadbackSelectsOnlyActiveScopeGenerations|HandlerListsCanonicalIdentities)' \
  -count=1
```

No-Observability-Change: the read keeps the existing `postgres.query` span with
`db.operation=list_cloud_inventory_identities` and the existing cloud inventory
response metadata. It adds no queue, worker, collector, graph write, metric,
span name, or runtime knob.
