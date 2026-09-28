# #7353: Neo4j cold entity-context live-test timeouts

## What failed

`TestLiveNornicDBEntityContextAnswerTruth` and
`TestLiveScopedEntityContextGrant` (`go/internal/query/entity`, build tag
`live_nornicdb_answer_truth`) failed intermittently against a fresh Neo4j.
The failure was a 500 carrying `ConnectivityError: Timeout while reading from
connection`.

## Causes

- **The timeout: emulation.** `docker-compose.live-backend-neo4j.yml` defaulted
  `platform` to `linux/amd64`, so on an arm64 host the Neo4j JVM ran under
  emulation. The entity-context anchor loop plans up to 16 distinct statements
  under one 10 s bounded-read budget. The diagnosis measured that cold planning
  at 11.9 s emulated against 2.0 s native (one Apple silicon host, unequal
  load).
- **The 500: the test's reader.** `entityLiveReader` returned the raw driver
  error once the read outlived its context. That error does not wrap
  `context.DeadlineExceeded`, so the handler's `errors.Is` check fell through
  to a generic 500.

Production never returned this 500 (Neo4jReader already answered 504);
cold-start planning latency after a restart is a separate availability risk,
tracked in #7380. The production `Neo4jReader` checks the caller's
context before it looks at the driver error (`graphReadResult` in
`go/internal/query/neo4j_read_policy.go`), so it already returns a graph-read
deadline and the handler answers 504 `backend_timeout`.
`TestNeo4jReaderDriverTimeoutUnderExpiredContextIsADeadline` and
`TestGetRelationshipsProductionReaderDriverTimeoutAnswers504`
(`go/internal/query/neo4j_read_driver_timeout_test.go`) drive `Neo4jReader`
through its session seam with that exact driver failure. They pass on the
pre-change base `6059663c5f`. They fail when the reader's bounded-deadline
branch is removed.

## Changes

- **Harness:** `scripts/run-live-backend-tests.sh` starts Neo4j on the Docker
  host's native platform, read from `docker version`. The pinned digest
  `neo4j:2026-community@sha256:eabfbb04…` is an OCI index covering
  `linux/amd64` and `linux/arm64/v8`, both `2026.08.1-community-trixie`. An
  explicit `NEO4J_PLATFORM` wins, CI's amd64 runners resolve `linux/amd64` as
  before, and `--backend nornicdb` skips the probe.
- **Test reader:** `entityLiveReader` applies the production context-first rule.
- **Hardening:** `querycontract.ClassifyBoundedGraphReadError` maps any read
  error to `ErrGraphReadDeadline` once the bounded context has expired. A
  canceled context, a live-context error, and an error already carrying
  `ErrGraphUnavailable` keep their mapping. It runs at the shared-deadline read
  sites: entity context, infra `getRelationships`, the tag-history refill loop
  and `writeTagHistoryReadError`. It covers any `GraphQuery` that bypasses
  `Neo4jReader`: test readers, fakes, and future implementations.

## Evidence

No-Regression Evidence: the change adds no Cypher and alters no statement. The
base-to-head diff of `entity/context_handler.go` and
`infra_relationship_filter.go` touches only the post-read error branch, its
comments, and imports. `ClassifyBoundedGraphReadError` runs only inside
`if err != nil`, so a successful read executes no new code. The one addition on
the error path is a `ctx.Err()` call and two `errors.Is` checks. The
`internal/queryplan` source-coverage inventory needed only the two
`source_sha256` re-pins for `(*Handler).GetEntityContext` and
`(*InfraHandler).getRelationships`. Call counts, classes, key bounds, and
`max_results` are unchanged, and the `internal/query` production-builder
query-hash bindings pass unchanged. Live, on native `linux/arm64` Neo4j
2026.08.1 with a fresh stack per run and one seeded answer-truth or scoped-grant
fixture: `TestLiveNornicDBEntityContextAnswerTruth` passed 5/5 in 8.67 to
13.42 s, and `TestLiveScopedEntityContextGrant` passed 5/5 in 10.46 to 20.00 s
(its `in_grant_returns_own_entity` subtest took 2.07 to 2.78 s). Host 1-minute
load was 6.6 to 11.3. In a separate implementer run (not from the diagnosis)
on emulated `linux/amd64` at load about 5.5, the answer-truth test passed 3/3
in 19.65 to 28.12 s:
emulation did not reach the 10 s timeout at that load. The diagnosis's
emulated failures were at load 13 to 22.

Observability Evidence: when the entity-context anchor loop ends in an error,
its warning log ("entity context anchor loop ended with an error before
resolving") carries `failure_class=deadline` for an expired budget. A genuine
read error, including an `ErrGraphUnavailable` the reader already reported,
logs `failure_class=graph_read_error`. The response carries the
`backend_timeout` (504) or `backend_unavailable` (503) error envelope. Unit tests
assert both the log field and the envelope code. On a cold native Neo4j with a
300 ms request deadline, a probe through the live-test reader returned 504 with
`{"error":{"code":"backend_timeout","message":"graph query exceeded its
deadline"}}`. The pre-change handler returned 500 for the same driver error.
Through `Neo4jReader` the existing `query.graph_read.warning` log and the
`outcome=deadline` attribute on `eshu_dp_neo4j_query_duration_seconds` are
unchanged. The live-backend
runner logs `run-live-backend-tests: neo4j platform <value>` for every
compose-managed Neo4j run.

## Not addressed

- The cold planning cost of the 16 distinct anchor statements. It needs a
  PROFILE-backed theory before any change.
- A plan-cache warmup after API start or Neo4j reconnect.
- Awaiting index population in production bootstrap. The diagnosis found every
  index ONLINE immediately after schema apply, and `db.awaitIndexes()` changed
  nothing.
