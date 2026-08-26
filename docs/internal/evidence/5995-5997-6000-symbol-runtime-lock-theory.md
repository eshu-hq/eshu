# #5995 / #5997 / #6000 symbol-runtime trio blocker mechanism — theory-proof

Records the Prove-The-Theory-First shims run against the `pg_advisory_xact_lock`
partition-lease mechanism BEFORE any fault-cell code was written for the
`handles_route` (#5995), `invokes_cloud_action` (#5997), and `runs_in` (#6000)
materialized-edge families. Both obvious fault-cell blockers for these three
families are illegal or vacuous, and neither fact is discoverable without
reading the Go lockstep tests -- which is why a shim was needed at all, and why
its result is recorded here rather than only in a PR description.

## Why the two obvious blockers do not work

All three families' rows are built by `buildSymbolRuntimeIntentRows` INSIDE
`CodeCallMaterializationHandler.Handle` -- the same handler the `code_calls`
family already covers (`go/internal/reducer/symbol_runtime_refresh_intents.go:66`).
That single fact rules out both handler-stage and runner-stage forms of the
`shared_intent_lock` blocker used by `code_calls`, `rationale_edges`,
`inheritance_edges`, and `shell_exec`:

- `(shared_intent_lock, wait_stage=handler)` -- the wait_key would have to be
  the routed claim domain, `code_call_materialization`, byte-identical to
  `code_calls`' own declared wait_key
  (`scripts/lib/ifa_family_registry/rows/02_code_calls.sh:19`). Two
  handler-stage families cannot share one wait_key:
  `TestIfaFamilyRegistryHandlerWaitKeysAreExclusive`
  (`go/internal/reducer/ifa_family_registry_wait_key_coherence_test.go`)
  rejects it outright, naming both families.
- `(shared_intent_lock, wait_stage=runner)` -- rejected by a different rule,
  independent of any wait_key collision:
  `TestIfaFamilyRegistryWaitStageAndKeyCohere` (same file, the shared_intent_lock rule)
  requires `blocker_kind=shared_intent_lock` to pair with `wait_stage=handler`
  only, because that blocker's mandatory retry-above-baseline proof reads
  `fact_work_items.attempt_count` scoped to wait_key, and
  `shared_projection_intents` has no `attempt_count` column at all -- the proof
  could never pass, and the cell would fail blaming the reducer for a row shape
  that was never wrong.

So before writing any cell for this trio, the open question was whether a
THIRD shape -- a blocker on the shared partition-lease mechanism itself, scoped
by domain rather than by handler or by wait_key -- could genuinely intercept
one of these families without touching the other two, `code_calls`, or the
runner-stage non-vacuity poll. That is a claim about production locking
behavior, not about registry wiring, so it needed a shim rather than a
read of the source.

## The theory under test

Production claims a shared-projection partition lease with a
TRANSACTION-scoped advisory lock keyed by projection domain --
`claimPartitionLeaseSQL` (`go/internal/storage/postgres/shared_intents.go:161`),
called from `SharedIntentStore.ClaimPartitionLease` (:283):

```sql
WITH domain_claim_lock AS (
    SELECT pg_advisory_xact_lock(hashtext('shared_projection_partition_leases'), hashtext($1))
)
```

THEORY: a blocker holding that same two-key advisory lock for one domain (a)
genuinely stalls that domain's lease claim, (b) does not conflict with the
runner-stage non-vacuity poll against `shared_projection_intents`, and (c)
leaves every other domain's claim free.

## Shim 1 -- session-scoped blocker, 3/3 reproducible

Ephemeral `postgres:18-alpine` (the repo's own image,
`docker-compose.yaml:47`), one Docker container per run, no concurrent
writers besides the shim's own psql sessions. Script:
`.trio-notes/shim1-advisory-lock.sh` (not committed -- `.trio-notes/` is
excluded via this clone's own `.git/info/exclude`, a per-clone rule rather
than a repo-tracked convention; results quoted here instead).

The blocker in this first shim takes a SESSION-scoped `pg_advisory_lock` on
`(hashtext('shared_projection_partition_leases'), hashtext('handles_route'))`
and sleeps. Five controls, run 3 times each with identical PASS/FAIL results
(only wall-clock timing varied between runs):

| control | result |
|---|---|
| blocker lock genuinely granted (`pg_locks` row observed) | `advisory\|1155623211\|3401466860\|ExclusiveLock\|t\|ifa_shim_blocker` |
| POSITIVE: `handles_route` claim while armed | BLOCKED, ~5.05-5.08s, killed by a 5s `statement_timeout` |
| NEGATIVE: `code_calls` claim while armed | succeeded in 61-75ms -- domain-scoped, not global |
| NO-DEADLOCK: runner-stage `shared_projection_intents` poll while armed | 40-50ms -- advisory lock does not conflict with it |
| RELEASE: `handles_route` claim after the blocker session is terminated | succeeded in 77-81ms -- the blocker, not something else, was the cause |
| FALSE-GREEN DETECTOR: an exited `psql -c` session holds zero advisory locks | 0 rows, confirmed |

### The false green this shim produced first, and the fix

The first run of this shim "passed" while proving NOTHING. Its readiness gate
was `pg_isready -U eshu -d eshu`, which greens against the initdb bootstrap
server BEFORE the target database exists. The blocker `psql` session then
failed to connect (`FATAL: database "eshu" does not exist` --
`.trio-notes/shim1-falsegreen-pgisready.txt`), no lock was ever held, and the
POSITIVE control claimed the `handles_route` lease freely in 33ms -- a result
that looks identical in shape to a real pass (a fast `CLAIMED_handles_route`)
but proves the opposite of what the control exists to prove.

Fix: readiness must poll `psql -U eshu -d eshu -tAX -c 'SELECT 1'` against the
real target database, not `pg_isready` against the server process, and the
shim must DIE (not merely warn) if the blocker's own lock is not independently
observed granted before any control runs. After that fix, three consecutive
runs (`.trio-notes/shim1-run1.txt`, `-run2.txt`, `-run3.txt`) reproduced the
identical PASS set above, byte-identical except for elapsed-time jitter (52nd
vs 61st vs 67th millisecond, all `SHIM1_RESULT=PASS`).

This is the most useful thing in this document for a future reader repeating
the experiment: readiness-via-`pg_isready` is a trap that produces a clean,
plausible-looking pass while the thing under test never ran at all.

## Shim 2 -- the shape actually specified, 3/3 reproducible

Shim 1 proved the mechanism could work but used the wrong lock variant for a
fault cell: `pg_advisory_lock` is SESSION-scoped and would leak across a
`docker exec` boundary or a dropped connection. The registry's actual blocker
shape must be TRANSACTION-scoped, byte-parallel to the existing
`_ifa_generic_table_lock_start` shape
(`scripts/lib/ifa_fault_generic_table_lock.sh:36-67`), so it self-releases on
every exit path including a `kill -9` of the blocking session:

```sql
SET application_name = '...'; BEGIN;
SELECT pg_advisory_xact_lock(hashtext('shared_projection_partition_leases'), hashtext('handles_route'));
SELECT pg_sleep(60);
ROLLBACK;
```

Held-check also changed: rather than reconciling `hashtext`'s signed `int4`
output against `pg_locks`' unsigned `objid` column (sign fragility with no
upside -- `hashtext('handles_route')` is `-893500436` but the same value reads
as `3401466860` in `pg_locks`, per shim 1's own key dump), shim 2 checks
`pg_try_advisory_xact_lock` from a separate session and reads back `t`/`f`
directly. Same ephemeral `postgres:18-alpine` container setup, same real
`SELECT 1` readiness gate. Script: `.trio-notes/shim2-xact-blocker.sh`.

Six controls, including a PRE-ARM check that the predicate can genuinely
return both `t` and `f` (not merely `f` after arming, which alone would not
prove the probe can fail):

| control | result |
|---|---|
| PRE-ARM: `handles_route` and `code_calls` keys both free before arming | both `t` |
| ARM: transaction-level blocker on `handles_route` | probe flips to `f` within 250ms |
| SCOPING: `code_calls` key stays free while `handles_route` is held | `t` -- domain-scoped |
| POSITIVE: production claim shape (the real `WITH domain_claim_lock AS (...)` CTE) against `handles_route` while armed | BLOCKED, ~5.04-5.05s |
| NO-DEADLOCK: runner-stage wait poll while armed | 29-42ms, unaffected |
| SELF-RELEASE: `handles_route` key free again after the blocker's session ends (ROLLBACK/terminate, no explicit release call) | `t` |

Three consecutive runs (`.trio-notes/shim2-run1.txt`, `-run2.txt`, `-run3.txt`)
produced the identical PASS set, `SHIM2_RESULT=PASS` each time, varying only in
elapsed-time jitter (5040ms / 5047ms / 5042ms for the positive control; 29ms /
42ms / 37ms for the no-deadlock poll).

## Disposition: proven, and deliberately not used in this change

The mechanism is proven at the lock layer: a transaction-scoped advisory lock
on the production partition-lease key genuinely, reproducibly, and
domain-scopedly blocks one family's lease claim without affecting a sibling
family, the runner-stage non-vacuity poll, or requiring an explicit release
call.

It is NOT used in this change. `handles_route`, `runs_in`, and
`invokes_cloud_action` ship Design A: `blocker_kind=none`, `wait_stage=runner`,
`wait_key=<own family>`, and one `cell_failgraphwrite_<family>` cell per family
anchored at that family's own edge-type MERGE template
(`scripts/lib/ifa_family_registry/rows/09_handles_route.sh`,
`10_runs_in.sh`, `11_invokes_cloud_action.sh`). This is a deliberate scope
decision, not an oversight: the repo's bar for claiming a
`materialized_edges:<family>` coverage row operates per-GATE, not per
blocker-mechanism-type -- a family must trigger and be asserted by BOTH the
`ifa-determinism` and `ifa-fault-injection` gates
(`go/internal/ifa/AGENTS.md:93-98`, enforced by
`TestEveryCoveredFamilyTriggersBothLiveGates`), and nothing in that bar
requires every possible fault dimension within the fault-injection gate.
`sql_relationships` is live precedent for exactly this shape:
`IFA_FAMILY_BLOCKER_KIND[sql_relationships]="none"`
(`scripts/lib/ifa_family_registry/rows/01_sql_relationships.sh:15`) while
resting its fault-coverage CLAIM on `cell_failgraphwrite_sql` -- the family
also dispatches `cell_killworker_sql`, but that cell's own documented
weakness (no lock is acquired before the kill, so it cannot prove the kill
landed mid-handler) is why the family's real claim rests on the graph-write
cell instead, not because no kill-worker cell exists.

Mid-pipeline kill/reclaim coverage is therefore a NAMED, tracked gap for these
three families, not a silent absence -- see
`go/internal/reducer/materialized_edge_family_blocker_shape_test.go`'s
`materializedEdgeFamilyBlockerLockstepExclusions` entries for `DomainHandlesRoute`,
`DomainRunsIn`, and `DomainInvokesCloudAction`, which name it explicitly. The
mechanism this document proves -- a transaction-scoped
`pg_advisory_xact_lock` on `shared_projection_partition_leases` keyed by
domain, used as a `runner_lease_hold` blocker -- is the de-risked design for
closing that gap, tracked as #6208. A future follow-up should name the
mechanism (`runner_lease_hold`) and cite #6208 rather than re-run this proof
from scratch.

## Appendix: HANDLES_ROUTE write-order rationale

Migrated here from a pre-implementation research note that lived under the
git-excluded `.trio-notes/` (see the earlier note on that exclusion) so the
citation resolves after merge. Recorded once here rather than duplicated at
every call site that relies on it
(`go/internal/ifa/materializededges/materialized_edges_handles_route.go` and
the `handles_route` expected-edges fixture's own `note` field both point
back to this section).

The intent-level dedupe key is `functionID + "\x00" + repositoryID + "\x00"
+ routePath + "\x00" + httpMethod` (`go/internal/reducer/handles_route_intents.go:80`)
-- it includes `http_method`, so GET and POST on the same path produce TWO
distinct intent rows, each with its own `PartitionKey =
functionID+"->"+repositoryID+":"+routePath` (`handles_route_intents.go:101`),
which is identical for both methods since the partition key does NOT include
the method.

But the graph-write MERGE identity is only `(Function, HANDLES_ROUTE,
Endpoint)` -- no relationship property, including `http_method`, is part of
the Cypher MERGE. So both the GET-row and the POST-row MERGE the SAME
relationship instance; each SETs `rel.http_method` to its own value, and
whichever row's write lands last -- their shared partition key means they
are always in the same partition and processed in whatever intra-batch order
the worker uses, not a documented ordering guarantee -- wins the final
stored `http_method` value. Net effect: exactly one edge, with a
non-deterministic `http_method` when more than one method is present.

This is why the `handles_route` vacuity guard asserts `http_method` as a
SET-only Property only when every intent row that collapses onto one edge
identity agrees on exactly one method (e.g. GET-only `/healthz`): asserting
it for a multi-method edge (GET+POST `/widgets`) would be flaky by
construction, since the value depends on write order the reducer does not
guarantee.

## Limits

Both shims are plan/mechanism proofs on a single ephemeral container with no
concurrent writers beyond the shim's own sessions, and no measurement of
contention under the reducer's real worker count or partition-claim retry
behavior. They show the lock's scoping and non-interference properties hold;
they are not a throughput, contention, or lease-storm measurement. Re-run
these shims (or their successors) if `claimPartitionLeaseSQL`'s key
derivation, the advisory-lock namespace hash, or the runner-stage poll's query
shape change.

## Runtime impact of the extraction seam

The `perf-evidence` gate flags six changed files as hot-path surface:
`go/internal/reducer/symbol_runtime_refresh_intents.go`,
`go/internal/ifa/symbol_runtime_family_odu.go`, and the four
`go/internal/ifa/materializededges/materialized_edges_*` files. This section
records why the change does not move the runtime contract, and how that was
established rather than assumed.

No-Regression Evidence: no measurement is reported because there is nothing
whose latency could have moved -- a benchmark here would time unmodified code,
which is the fabrication this section exists to avoid. Three structural facts
carry that, each verified on this branch rather than reasoned about.

First, be exact about which files are gate-only, because an earlier draft of
this section was not. Five of the six are: `symbol_runtime_family_odu.go` and
the four `materialized_edges_*` files. The sixth,
`go/internal/reducer/symbol_runtime_refresh_intents.go`, is NOT -- it lives in
`internal/reducer` and contains production code. `buildSymbolRuntimeIntentRows`
is defined there and runs inside `CodeCallMaterializationHandler.Handle`
(`code_call_materialization.go:175`) on every code-call materialization. A file
holding production code is not a file off the production path; what is off the
path is the added function, and the claim has to be made at that granularity.

Second, no pre-existing line in that file changed. `git diff --numstat
origin/main...HEAD -- go/internal/reducer/symbol_runtime_refresh_intents.go`
reports `46 0`. Zero deletions is what makes "byte-identical" rigorous rather
than rhetorical: a modified line appears in a unified diff as a delete plus an
add, so zero deletions means no existing line was touched.
`buildSymbolRuntimeIntentRows` and the three per-family builders beneath it are
byte-identical to `origin/main`, and the whole diff is one added function plus
its doc comment. Scope that to the file, not the package: the only other
non-test reducer change is `materialized_edge_families.go` at `6 4`, whose four
deletions are comment lines -- zero non-comment changes, but not zero
deletions.

Third, the added function has no runtime caller. It has exactly one caller in
the tree, `materializededges/materialized_edges_symbol_runtime_shared.go:53`;
every other `rg 'ExtractSymbolRuntimeIntentRows'` hit is a doc comment or a
string literal inside an error message, so a naive match count over non-test
files reads as 14 hits across 6 files and is wrong. The package holding that caller,
`internal/ifa/materializededges`, is linked by exactly one main package. Two
independent derivations agree: enumerating every main package in the `go`
module (54 of them -- 52 under `cmd/`, plus two generators elsewhere, neither
of which links it) returns only `cmd/ifa`; and the package's non-test import
closure is four files, all under `go/cmd/ifa`. Count main packages with
`go list -f '{{.Name}}' ./...`, not with a textual sweep. The sweep overcounts
by one: `rg -l '^package main' go/ | xargs -n1 dirname | sort -u | wc -l`
reports 55 package directories, because `internal/resolutionparity` contains
the string `package main` at line 78 of a `_test.go` file as embedded fixture
content. Note the pipeline rather than the bare `rg -l`, which reports 861
files -- a package spans many, so the file count is not the package count and
neither figure is reproducible from the other.
`go list` reports that package as `resolutionparity`. A grep for a package
clause cannot tell a declaration from a fixture, and this is the one place in
this document where that distinction changes a number. Other Go modules in this repo cannot
reach it, and not because nobody tried: `internal/` is PATH-scoped, not
module-scoped -- the Go rule is that an import of a path containing an
`internal` element is disallowed from outside the tree rooted at that
directory's parent, and module boundaries do not enter into it. So the
constraint is that a package must be rooted at `github.com/eshu-hq/eshu/go/` to
import this at all, and no other module in the repo claims a path under that
prefix: 19 `go.mod` files, with the siblings under `eshu/tools/`,
`eshu/sdk/go/` (a different prefix from `eshu/go/`) and `eshu/examples/`, plus
fixture modules on unrelated paths. The `go` module is therefore the entire
candidate set rather than merely the set that was searched -- and that stays
true only while no new module claims a path under `eshu/go/`. None of the five runtime services
(`cmd/api`, `cmd/mcp-server`, `cmd/ingester`, `cmd/reducer`,
`cmd/bootstrap-index`) links it.

Two corrections worth carrying forward rather than quietly fixing. An earlier
draft claimed the package was "not linked into any shipped binary", derived
from `go list -deps` over four hand-picked commands; it is linked, into
`cmd/ifa`. A subset probe can establish presence but never absence -- enumerate
the population and let the exceptions announce themselves. Note also that
`internal/reducer` itself IS linked into 36 of the 52 `cmd/` main packages,
`cmd/reducer` among them, so `ExtractSymbolRuntimeIntentRows` is compiled into
the reducer binary; it simply has no caller there. Do not tighten this into "absent from
the reducer binary", which would be false.

Backend/version and input shape are therefore not applicable to a throughput
claim: the added function runs only under the two Ifá proof gates, against the
single committed cassette (1 repository, 3 files, 3 `content_entity` facts, 2
`shared_followup` facts -- nine facts total), producing a terminal set of 2
HANDLES_ROUTE, 2 RUNS_IN, and 1 INVOKES_CLOUD_ACTION edges. Those counts are
asserted exactly by the gates, so an output regression fails them. Work-shape
changes are not covered by that assertion -- nothing here would catch an extra
pass over the envelopes, a redundant allocation, or an accidental O(N^2) in the
entity index that still produced the same edges -- and do not need to be: the
added function executes only under those gates on that nine-fact cassette, so
there is no production cost for a work-shape change to regress.

No-Observability-Change: no span, metric, counter, log line, or status field is
added, removed, renamed, or re-cardinalised. The production emitters are
untouched by the additive diff above, and the new code is gate-only, so there
is no runtime surface for an operator to observe differently at 3 AM than
before this change. The Ifá cells' own diagnostics (marker assertions, dead
letter counts, digest comparisons) are gate-time output, not operator
telemetry, and are covered by the gates rather than by dashboards.
