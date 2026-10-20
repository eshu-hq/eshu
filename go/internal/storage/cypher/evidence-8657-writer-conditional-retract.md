# Evidence: #8657 Writer-Side Conditional Retract (Phase-1 Theory Proof)

## Decision, Threshold, Deadline (prove-theory-first record)

- **Decision:** gate the unbounded file retract behind a writer-side prior
  File count probe that fails loud (retryable) when priors exist.
- **Threshold:** the probe plan is index-anchored with db hits linear in
  the repository's own File count (at most the retract's own match phase),
  and non-file-less builds issue zero probes.
- **Deadline:** single-session proof; captured 2026-10-20T19:23Z. Met --
  no clock restart.

## Problem

A file-less build (zero live, zero tombstoned file facts, zero File rows)
over a scope with prior File nodes emits the unbounded
`node.RetractFilesCypher`, writes nothing, and reports success. PR #8669
proved a builder-level rejection cannot see this shape: identical
`(inputFacts, mat)` is retirement-legit in four committed tests and
wipe-illegit only given other generations' history. Only the writer, which
owns the retract, sees both halves. The #8669 evidence matrix
(`zero-live-file-guard-evidence.md` at that head) is the ambiguity proof.

## Probe Shape (exact)

```cypher
MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
RETURN count(f) AS prior_file_count
```

Issued at most once per `CanonicalNodeWriter.Write`, only when the build
selects the unbounded file retract AND carries zero live and zero
tombstoned file facts AND zero File rows AND no `#8668` declaration.
`usesUnboundedFileRetract` is the single predicate the statement builder
and the guard share. Tombstone-driven deletes, first generations, delta
projections, non-repository scopes, and file-bearing builds never probe
(unit-pinned, zero added statements on those paths).

## No-Regression Evidence

Backend: pinned `ghcr.io/eshu-hq/ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
(Cypher 25, planner COST, runtime SLOTTED 2026.08.1), scratch container
`neo4j-8657-proof`. Index state: `CONSTRAINT repository_id ... REQUIRE
r.id IS UNIQUE` (production schema, `go/internal/graph/schema_tables.go`),
used by the plan (`Indexes Used: repository_id: 1`).

Input cardinality: one Repository with 1,001,002 File nodes
(`proof-repo-8657`, production-shaped properties plus REPO_CONTAINS edges,
seeded in ~10 s in 500-row batches), plus an empty Repository control
(`proof-repo-8657-empty`).

PROFILE at 1,001,002 File nodes (via HTTP transactional API; this
backend's Bolt driver drops PROFILE counters):

| Operator | Rows | DB Hits |
|---|---|---|
| ProduceResults `prior_file_count` | 1 | 0 |
| EagerAggregation `count(f)` | 1 | 0 |
| Filter `f:File` | 1,001,002 | 1,001,002 |
| Expand(All) `(r)-[:REPO_CONTAINS]->(f)` | 1,001,002 | 1,001,004 |
| NodeUniqueIndexSeek `UNIQUE r:Repository(id)` | 1 | 2 |

Total database accesses: 2,002,008 (~2 per repo File row), allocated
memory 88 bytes. Cost is linear in the repository's own File count and
independent of store size: the anchor seeks 1 row through the unique
index and the expand walks exactly this repository's degree. The retract
being guarded pays the identical match phase, so the probe at most doubles
the match cost of a statement that previously wiped first and asked never.

Empty control: count 0, 3 total db accesses, same plan shape.

Before/after: before, every file-less build ran the unbounded retract
unconditionally. After, file-less builds pay one anchored COUNT read and
file-bearing builds pay nothing (guard short-circuits before probing;
`TestFilelessWipeGuardSkipsNonWipeShapes` pins zero probes on all five
non-wipe shapes, and `TestUsesUnboundedFileRetractMatchesStatementSelection`
pins the predicate to the statement selection).

Wall corroboration only (host NOT quiet: load ~30 on 16 CPUs with sibling
lanes active, so no wall-time gate per `docs/internal/timing-proof-rules.md`
rule 1): plain COUNT 61-141 ms at 500k-1M rows (spread across runs),
1-4 ms empty. Deterministic plan plus db hits are the proof.

## Concurrency Argument (probe vs retract atomicity)

No new lock, worker, batch, queue, or transaction change: the probe is one
indexed read outside any transaction (serialization is not the fix).

- Conflict domain is scope: the projector claim pool admits no claim for a
  scope with a live lease on another work item
  (`projector_queue_claim_pool_sql.go` NOT EXISTS live-lease guard), claims
  serialize per scope through the claim fence (#7115), and the
  write-start marker (#7389) commits before the first graph write,
  exempting the attempt from heartbeat supersede and holding newer
  same-scope rows behind it in the claim pool (#7469, #7819 fence bump).
- The guard probe runs inside `CanonicalNodeWriter.Write`, hence after the
  marker commits: no legitimate concurrent same-scope projection can start
  between probe and retract. Overlap needs lease expiry plus a live but
  unheartbeating worker -- the modeled stale-attempt class the duplicate
  reclaim and `#7209` zombie heal already cover.
- Fail-safe direction (the #6822/#5998 precedent): found refuses with no
  write issued, so no TOCTOU harm is possible on that path; unknown
  (no seam, probe error, negative count) proceeds with the retract exactly
  as before and counts `probe_unsupported`/`probe_failed`.
- Residual risk, stated plainly: a stale attempt's zero-at-probe to
  files-at-retract window can still wipe a newer generation's files; the
  zombie's Ack then hits the supersede fence and `#7209` heals generation
  state, with the newer generation's files restored by its next
  projection. Eliminating that window needs a generation-fenced retract and
  is out of Phase-1 scope. The guard strictly narrows today's unconditional
  wipe; it cannot create one.
- Live concurrency proof:
  `TestLiveFilelessGuardConcurrentGenerations` overlaps four file-less
  attempts with a file-bearing generation on one scope: every file-less
  attempt fails loud and retryable, the bearing generation succeeds, and
  the final File count is exact.

## Observability Evidence

New counter `eshu_dp_canonical_fileless_wipe_guard_total` by closed
`outcome` (`refused_prior_files`, `probe_unsupported`, `probe_failed`;
steady-state projections record nothing). Coverage row added to
`docs/public/observability/telemetry-coverage.md`; verifier
`scripts/verify-telemetry-coverage.sh` passes. Refusals log WARN and tag
the write span with generation, scope, repository, and prior File count;
the error text names all four and points at the `empty_scope` marker
(#8668). Count probes ride the existing `neo4j.execute_probe` span and
`Neo4jQueryDuration{operation=probe}` point with
`db.operation=canonical_count_probe`. No new span name, worker, queue
domain, runtime knob, or backend branch.

## What Remains (post-#8670 rebase, done-state recorded)

1. DONE: `declaredEmptyScopeForGuard` reads `mat.DeclaredEmptyScope`
   (no `TODO(#8668)` left); seam pinned by
   `TestDeclaredEmptyScopeForGuardReadsMaterialization` plus the
   guard-level `TestFilelessWipeGuardHonorsDeclaredEmptyScope`.
2. DONE: rebased onto post-#8670 main; the predicted adjacent-hunk
   conflict on `materialization.go` resolved by keeping both
   `DeclaredEmptyScope` and `FileIntent` (plus gofmt realignment).
   This lane still never reads or writes the marker key itself.
3. NornicDB terminal count: the `PhaseGroupExecutor.ExecuteCountProbe`
   forward is in place, but no NornicDB terminal implements the seam yet,
   so that path reports `probe_unsupported` and proceeds (measured: the
   drain-loop probes prove the Bolt read path works; COUNT aggregation
   there is unverified -- verify in CI live lanes).
4. Reducer chains (`cypherRunnerStatementExecutor`) cannot implement the
   seam (bool-only `QueryCypherExists` runner); they fail fast at the top
   assertion, which is safe, and they never feed `CanonicalNodeWriter`.
5. Adjacent edge, out of scope: a build whose File rows are all dropped by
   the oversized-index-key guard reaches the writer file-less with live
   intent; the guard deliberately does not fire there (per-node WARNs are
   that path's signal, #7058).
