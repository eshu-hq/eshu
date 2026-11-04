# #10003 PR0: One Reader-Side Delta Overlay Helper

#9991 fixed the manifest dependency read for delta-active git scopes, but its
plumbing was manifest-named and unexported. #10003 rows 1 to 3, #10005, and
#10006 need the same overlay for other readers. The arbiter ruled that they
must reuse one implementation, so this change extracts it first. The manifest
reader stays the only consumer, with no behavior change.

## Change

- `go/internal/storage/postgres/delta_overlay_query.go`:
  `deriveDeltaOverlayBaseQuery(active, scopeParam)` derives a reader's
  base-candidate query from its active text by the #9991 fragment swaps. The
  reader names the placeholder its delta-active scope array binds at (the
  manifest reader keeps `$6`; Crossplane XRD needs `$4`, code-call `$5` and
  `$6`, JVM `$6`, terraform `$1` and `$2`). A fact branch is any
  `fact_records AS fact` token, after `FROM` or `JOIN`, so
  `listTerraformBackendFactsQuery` derives. The derivation fails closed when a
  branch lacks exactly one active scope join and one active generation
  predicate, when the placeholder is invalid or already bound, or when the
  text already uses the `active_delta` alias. The same file holds the target
  lookup (`deltaOverlayTargetsQuery`) and the overlay inputs read
  (`deltaOverlayInputsQuery`), unchanged.
- `go/internal/storage/postgres/delta_overlay.go`:
  `withDeltaOverlaySnapshot` runs a read on one read-only repeatable-read
  snapshot. `deltaTargets` reads the target lookup once per call and shares it
  across the reader's legs. A nested snapshot reuses the open transaction, so
  the impact snapshot and a multi-leg reader each issue one BEGIN.
  `overlayDeltaBase` is the #9991 overlay with a telemetry sink parameter
  (read label, WARN subject, span event, counter). Only reached scopes are
  resolved and counted.
- `go/internal/storage/postgres/delta_union_base.go`: `deltaUnionBaseQuery`
  and `deltaUnionBasePrunedQuery` stay the one definition of the #7797 base
  rule, now batched (coordinator ruling on #10003; a single statement would
  have lost #8181). The base lookup runs once over every
  `(scope, union-base commit)` pair: `unnest` drives a `LATERAL ... LIMIT 1`
  with the unchanged predicate and `ORDER BY`. The prune probe runs once over
  the superseded bases' `retentionHashID` pairs, still hashed in Go.
  `resolveDeltaBases` returns a base or a refusal per scope.
  `resolveDeltaBaseAtCommit` is that call for one pair, which the projector
  and the successor guard keep using. Their statement order and counts stay
  the same, and a probe failure is still a refusal.

## Test changes that are not renames

The base and probe statements now bind arrays and return a key column, so the
fakes changed shape only:

- Positional fakes in `delta_union_test.go`, `delta_union_load_test.go`,
  `delta_union_chain_test.go`, `delta_union_bench_test.go`, and
  `active_package_manifest_dependency_delta_test.go`: each staged base row
  gained its leading `scope_id`, and the pruned-probe row returns the scope
  hash instead of `1`.
- `delta_union_chain_test.go`: the base lookup's commit argument is now the
  one-element `$2` array.
- `successor_guard_harness_test.go` and
  `successor_guard_recheck_harness_test.go`: the base lookup route unwraps
  the one-element arrays and leads the staged row with the scope id
  (`baseRowsFor`). `successor_guard_prior_test.go` reads the commit from the
  array.
- `TestDeltaUnionBaseQueryKeepsPartialIndexPredicates`: `scope_id = $1` and
  `source_commit_sha = $2` became `scope_id = target.scope_id` and
  `source_commit_sha = target.union_base_commit_sha`.

Every other assertion is unchanged. That covers statement counts (4, 5, and 4
in the projector cases, and 1 retention probe in the successor guard), the
#8181 probe-outage refusal, its counters and log, the refused, pruned,
legacy, chained, and mismatch outcomes, and the #9991 manifest cases.

## Proof

- Parity, live: `TestResolveDeltaBasesMatchesPerScopeRuleLive` seeds 12 base
  shapes in one schema: active, completed, intact superseded with an older
  generation pruned, pruned superseded, missing, unactivated, delta, and
  pending at the commit, intact outranking recency, the newest superseded
  pruned, a `generation_id` tie, and chained. For every scope, the stated
  expectation, the pre-#10003 per-scope statements (the oracle, copied from
  `c9c1f40375`), the batched call (2 statements for 12 scopes), and the
  one-pair path agree. It then renames the retention table: every superseded
  base refuses, the others still resolve, and the call is not an error.
  Mutations caught: dropping the active and completed precedence from the
  `ORDER BY`, and dropping the generation hash from the probe.
- Unit tests: placeholders `$1`, `$2`, `$4`, `$5`, `$6`; the byte-identical
  manifest text; the JOIN-shaped derivation; ten fail-closed shapes; one
  target lookup and one BEGIN across nested legs; two statements for four
  scopes; the probe skipped without a superseded base; the batched probe
  outage refusing only superseded scopes; and the manifest read resolving
  three reached scopes in one batch while a fourth, unreached, is neither
  resolved nor counted. Mutations caught: no target cache, no nesting,
  per-scope resolution, the `FROM`-only guard, no placeholder check, and no
  token boundary.
- Before and after on the focused suites (commands below): 159 passing tests
  before and the same names plus the new ones after.

## Performance Evidence:

Disposable PostgreSQL 18.6 container (`eshu-lane3-9874-pg`), own database
`lane3_pr0_explain`, all 302 migrations applied in order. It holds 420 git
scopes, each with 20 older superseded fulls and a base full at `c1` (8,861
generations), plus 4,204 retention events (the 10 oldest fulls of every
scope). 40 scopes are delta-active: 4 have a pruned base and 3 record a union
base with no full. Literal parameters (custom plans) unless marked generic;
medians of 5 runs (7 for the one-pair rows).

| statement | execution | planning | buffers | plan |
| --- | --- | --- | --- | --- |
| before: base lookup, one scope | 0.237 ms | 1.039 ms | 35 | bitmap on `scope_generations_scope_generation_idx`, filter, top-1 sort |
| before: prune probe, one scope | 0.044 ms | 0.448 ms | 4 | index only on `generation_retention_events_generation_idx` |
| after: base lookup, 40 pairs | 0.963 ms | 0.784 ms | 971 | nested loop over `unnest`, the same bitmap per pair |
| after: base lookup, 40 pairs, generic plan | 1.118 ms | 0.912 ms | 971 | same |
| after: prune probe, 37 pairs | 0.289 ms | 0.745 ms | 112 | nested loop semi join, index only per pair |
| after: prune probe, 37 pairs, generic plan | 0.273 ms | 0.805 ms | 112 | same |
| before: base lookup, one scope (projector) | 0.157 ms | 0.647 ms | 35 | as above |
| after: base lookup, one pair (projector) | 0.209 ms | 0.725 ms | 35 | as above |

No statement runs a sequential scan. The generic plan keeps the custom plan's
shape, so these statements need no exec-mode pin. Both bind arrays only as
`unnest` inputs, with no `= ANY` residual filter (the #9586 trap).

Base resolution for the 40 reached scopes, one psql session over localhost,
summing client-side statement times: before, 77 statements (40 lookups and
37 probes) took 19.6 ms. After, 2 statements took 4.6 ms. The batched rows
match the per-scope rows for all 40 scopes, and the batched probe returns
exactly the 4 pruned scopes.

The projector and the successor guard keep one pair per resolution: the same
statement count, 35 buffers before and after, and +0.05 ms execution and
+0.08 ms planning per delta load.

The committed #9991 harness
(`active_package_manifest_dependency_delta_bench_test.go`, tag
`perf9991_manifest_delta`) seeds its 156,201-row corpus and counts every
statement of `ListActivePackageManifestDependencyFacts`. It ran twice on
`origin/main` (`a35f7077a6`, whose storage package equals this branch's base)
and twice on this branch, on the same host and container. It now reads the
reached-scope figure from the scopes the batched lookup binds. Statement
totals exclude BEGIN and COMMIT (one each per read):

| state | read | main statements | branch statements | rows |
| --- | --- | --- | --- | --- |
| delta-active, 4 reached scopes | npm | 54 (23 active pages, 1 target lookup, 21 base pages, 4 base lookups, 4 probes, 1 inputs) | 48 (the same, with 1 base lookup and 1 probe) | 20,000 both |
| delta-active | maven, no rows | 3 | 3 | 0 |
| no delta-active scope | npm | 42 | 42 | 20,000 |
| no delta-active scope | maven, no rows | 2 | 2 | 0 |

Wall time is not comparable across these runs. The host load average moved
between 9 and 54 on 12 cores while they ran, and the unchanged active-only
leg itself moved from 1.52 s to 3.48 s. Within each run, the new read over
the active-only read for the delta-active npm shape was 2.66x and 2.14x on
main and 2.25x and 2.29x on the branch. For the no-delta npm shape it was
1.00x and 0.98x on main and 0.97x and 1.00x on the branch. The saving is the
six statements above, a few milliseconds of the multi-second read. The base
pages still dominate the read (#9611 owns that rewrite).

## Behavior notes

- A failing base lookup now fails the read before any scope is decided. The
  #9991 loop counted the decisions it had already made for earlier scopes.
  Now the read counts only `union_resolve_failed`, once. The read returns the
  same error either way.
- In a transaction (the reader snapshot, the successor guard's commit
  transaction), a failed probe statement aborts the transaction. The refusal
  is recorded, and the next statement fails. This predates #10003 and is
  unchanged.

## Known limits

- The derivation is a text swap. It needs every fact branch aliased `fact`,
  with the active joins written exactly as the manifest texts write them. A
  reader written another way fails closed and needs its text aligned first.
- `tfstate` cannot call these helpers directly: `postgres` imports `tfstate`
  (`compat_tfstate.go`), so #10006 needs an injected seam or a move.
- The target lookup is unbounded (every delta-active git scope per read),
  as recorded in the #9991 note.

## Observability Evidence:

The manifest reader records the same signals with the same names: counter
`eshu_dp_package_manifest_dependency_read_delta_union_total` by `read` and
the closed `outcome` set, one
`package_manifest_dependency_read.delta_union` span event per read, and WARN
lines starting `manifest dependency read delta base missing` or
`manifest dependency read delta carries no delta_generation repository fact`.
The projector's `projector.delta_union` outcomes, the #8181 debug line
`delta union retention probe failed; refusing closed`, and the successor
guard decisions are unchanged; their existing tests pass on their original
assertions. A later reader passes its own sink.

## Commands

The env file sets `ESHU_POSTGRES_TEST_DSN`,
`ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN`, and
`ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN` (with its disposable opt-in) to the
container's `postgres` database.

- `go test ./internal/storage/postgres -count=1 -run '<delta-union, guard, manifest, overlay set>' -v`
  with the live DSNs set, before and after.
- `go test ./internal/storage/postgres -count=1` (unit, no DSN).
- `go test -tags perf9991_manifest_delta ./internal/storage/postgres -run TestManifestDeltaReadStatementCountsLive -count=1 -v`
  on `origin/main` and on this branch.
