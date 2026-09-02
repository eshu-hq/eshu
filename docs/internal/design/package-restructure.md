# Package restructure: from flat thousand-file directories to a tree a human can read

Status: proposed. Research: 8-agent measured sweep, 2026-08-11 (per-package
family inventories, symbol-level dependency measurement, gate blast surface).
Tracked as epic #6053; this document is its committed plan.

## The problem, in numbers

| Package | .go files, one flat dir | Non-test / test |
|---|---|---|
| `internal/query` | 1,903 | 877 / 1,026 |
| `internal/reducer` | 1,269 | 536 / 733 |
| `internal/mcp` | 338 | 130 / 208 |
| `internal/parser` (root) | 259 | 47 / 212 |
| `internal/collector` (root) | 250 | 111 / 139 |
| `cmd/eshu` | 233 | 121 / 112 |
| `internal/projector` | 188 | 92 / 96 |
| `internal/coordinator` | 124 | 66 / 58 |

Three splits are corrected from the research, all counted on disk when this
was committed with `rg --files -g '*.go' --max-depth 1` against each
directory and the `*_test.go` subset of the same list. Collector read
`~240 / ~10+3`, which is the package-clause split (247 files say `package
collector`, 3 say `package collector_test`), not the filename split: it is
111 / 139. Projector and coordinator read `94 / 94` and `62 / 62`, which
the appendix marks "approx" and this table had dropped the hedge on; they
are 92 / 96 and 66 / 58. All three totals were already right.

The other rows are the research's own count and have drifted a little since
2026-08-11, which is the point the appendix makes about re-running the
census before any move: query now reads 1,910 files (879 / 1,031), reducer
1,270 (536 / 734), and mcp 340 (130 / 210). Parser and cmd/eshu still match
exactly.

3,165 of the query+reducer files were added since 2026-07-01. The cause is
our own 500-line file cap: it splits files but says nothing about
directories, so every split adds flat files. A gate fixed file size and
created directory sprawl. The fix is the same move one level up: a
directory gate, then a measured migration.

Why this matters beyond taste: a contributor opening `internal/query` sees
1,903 files and concludes nobody curates this codebase. The counter to
"this looks machine-generated" is a tree a stranger can navigate. And the
directories we create become the module seams #4047/#4398 (the
package-extraction program) need — a family graded "clean" today is a
candidate repo tomorrow.

## Part 1: the gate (lands first, conflicts with nothing)

Model it on the existing 500-line cap, which has two implementations that
must stay in lockstep: the golangci-lint plugin
(`tools/golangci-lint-filelength/filelength.go`) that CI runs, and the bash
mirror in `scripts/dev/precommit-go.sh` (`filecap` / `filecap-all`) that
pre-commit and the local gate runner use. The directory gate follows the
same pattern:

- **Rule 1 — size:** max 40 non-test `.go` files per package directory
  (tests excluded; they pair with subjects). 40 keeps every
  already-healthy package green and catches sprawl early.
- **Rule 2 — naming:** a file whose name prefix matches a sibling
  subdirectory's package name belongs in that subdirectory (catches the
  "new file dodges the tree" regression).
- **Escape hatch:** same `//nolint:<gate>` convention on the package line
  with a written justification (27 files use this for filelength today).
- **Grandfather:** digest-pinned list of the current offenders (the #5335
  gate's pattern) so the gate lands green and the list only shrinks;
  editing a grandfathered directory's file count upward un-grandfathers it.
- **Registry entry** in `specs/ci-gates.v1.yaml` triggered on `go/**`
  (broad glob = immune to the two-layer registry/workflow drift trap).
- **BITES proof** required: seeded violation goes red naming the directory
  and the two legal exits; green on revert.

Three of the families Part 3 calls clean and moves early are themselves over
the cap, counted on disk when this was committed (non-test `.go` files at
depth 1): `reducer/supply_chain_impact` 63, `query/code` 85,
`query/supply_chain` 61. Moving any of them into one new directory would
create a directory that fails this gate the moment it exists. They land
pre-split into nested subdirectories in the same move PR — the shape the
collector plan already uses (`gitrepo/snapshot`, `gitrepo/selection`).
Grandfathering is for directories that exist today and never for one a move
PR creates, because the pinned list only shrinks. The rest of the early
movers clear the cap: `query/impact` 39, `collector/git_snapshot` 24,
`collector/git_selection` 21, `reducer/container_image_identity` 18,
`reducer/code_call_materialization` 25.

## Part 2: harden the gates BEFORE anything moves

The research found the scariest class isn't gates that break — it's gates
that **pass silently on nothing** after a move. Fix these first, as their
own PR, before any file moves:

1. Non-recursive `go test ./internal/query -run '<names>'` in at least six
   scripts (`verify-replay-coverage-gate.sh:51`,
   `verify-hosted-governance-proof.sh:58,60,96,98`,
   `verify-ask-eshu-local-proof.sh:192,229`,
   `verify-hosted-governance-remote-compose-proof.sh:61,92`,
   `verify-query-plan-profile.sh:53`, `verify-query-plan-regression.sh:9`)
   plus `specs/ci-gates.v1.yaml:2166` — a `-run` regex matching zero tests
   exits 0 ("no tests to run"). Same class in
   `mcp-schema-drift.yml:199` and `security_intelligence_release_gate.sh:277`
   for `./internal/mcp`. Change to `/...` where safe; where a `-run` pin
   must stay package-scoped, add a "matched at least N tests" assertion.
2. `scripts/verify-route-coverage.sh:112` uses `find -maxdepth 1` — a moved
   handler file silently drops out of route-coverage checking.
3. `go/internal/payloadusage/load.go:37-64` and `:95-113` resolve
   decode-seam files with a NON-recursive
   `filepath.Glob(dir/"factschema_decode*.go")`, so a seam file moved into a
   subdirectory drops out of the manifest gate without a word. The reducer
   glob does fail when it matches nothing at all; the projector, query,
   loader, relationships and replay globs deliberately accept an empty
   match while those families migrate. Neither case catches a PARTIAL move,
   which is the one a restructure produces. (The research cited
   `paths.go:99-143`, which documents this behavior in the `Paths` field
   comments rather than implementing it.)
4. Run `go/cmd/ci-gates validate --drift` (checkPathFilterCoverage) in the
   registry gate by default, not only on demand — it exists precisely to
   catch registry-vs-workflow filter drift, but only checks literal
   triggers and only when invoked.
5. The 10 literal single-file gate triggers (list in the research:
   `git_snapshot_entity_buckets.go`, `materialized_edge_families.go`,
   `shared_projection.go`, 3× `sql_relationship_*`, 3×
   `gcp_resource_materialization*`, `intent.go`, plus
   `canonical.go`/projector and `mcp_setup*`/cmd-eshu) each need updating in
   the same PR as their move — the hardening PR adds an existence check so
   a stale trigger fails loudly (the telemetry-coverage gate already does
   this with `path_target_exists`; copy it).

`docs/public/observability/telemetry-coverage.md` (473 rows citing exact
paths) will fail LOUDLY on moves — that's correct behavior; each move PR
rewrites its rows. The parser ledgers
(`language-feature-parity-ledger.v1.yaml`, `parser-backing-ledger.v1.yaml`)
and cmd/eshu's spec references (`backend-conformance.v1.yaml:98-101`,
`ci-gates.v1.yaml:1269-1271,1848`) are the same shape. `internal/mcp` has
~80 hardcoded `go test ./internal/mcp` references across five spec files.

## Part 3: target layouts (measured, per package)

Full family tables with counts and extraction grades live in the
[research appendix](restructure-research.md). Summary of what moves and
what stays:

**collector (250 flat root files):** one new `gitrepo/` umbrella package
absorbing the git-specific families — snapshot(64), selection(39),
docs(41), observability, submodule, workflow-image, tfstate-glue,
service-catalog-glue, codeowners-glue, refs, tracked, webhook, priority,
fair-dispatch. Root keeps the shared seam every collector kind uses:
`Service`/`Source`/`Committer` and the `claimed_service*` family (backs ~15
other collector kinds).
git_snapshot↔git_selection↔git_source is a measured 3-way production
import cycle — they move together into gitrepo, not into separate
packages, until a dependency-inversion refactor earns the split. Five glue
families need disambiguated names (gitsubmodule, gittfstate, …) because
same-named sibling packages already exist.

**Correction, landed with #6056.** This section originally also listed
`git_source_types.go` and `git_fact_builder*` as staying in the root. They
do not, and could not. `git_source_types.go` declares `RepositorySnapshot`,
whose fields reach `GitRef`, `TerraformStateCandidate`,
`FunctionSummarySnapshot`, `FunctionSourceSnapshot` and
`DataflowFunctionSnapshot`, and that data model is woven through
git_snapshot_* and git_selection_*. Pinning the file in the root and letting
the compiler pull back every declaration it transitively needed converged at
**103 of the 111** non-test root files — the documented seam would have moved
eight files and left the directory as it was. Both files moved into
`gitrepo`, which cuts the root to 19 files and drops its grandfather row
entirely.

Two consequences worth knowing before the remaining children:

- The leaf emitters could not be peeled on their own either. Every one of
  them needs the fact-stream writer and the content records that the fact
  stream also calls into, so a leaf-only move closes an import cycle. The
  shared half became `gitrepo/gitmodel`, and the leaves sit below it:
  `gitrepo -> leaf -> gitmodel`, one direction only.
- `gitrepo` itself stays over the 40-file cap at 66 and carries a
  grandfather row. The remaining overage is the snapshot/selection/source
  cycle, which needs the dependency inversion this epic deliberately did not
  bundle with a move. The ledger still improves: the root's row at 111 is
  gone and the replacement is 66, and dirgate's ratchet means any later
  extraction has to re-pin it lower.

No-Regression Evidence: the eshu_search family move (#6061) relocates the eight
non-test eshu_search_*.go files and their eleven test siblings out of the reducer
root into `internal/reducer/eshusearch`, with no logic change. Unlike the earlier
hoists in this epic the root keeps NO aliases: every caller was repointed to
import the leaf directly, across internal/reducer, internal/projector,
internal/storage/postgres and cmd/reducer. Six symbols the family reached through
root forwarders now call their real owners -- Intent, Result and
ResultStatusSucceeded to contract, uniqueSortedStrings to payloadcore,
reducerWriterNow and reducerFactCollectorKind to factwrite -- each verified to be
a one-line forwarder at the base rather than a distinct implementation.
Baseline `a83fc5c62` (this branch's merge-base with `origin/main`, not the
moving tip: main advances continuously while a PR waits, so a tip-named citation
is stale on arrival), after `7e919b6fc` (the commit that lands this move's
code), go1.27.0
darwin/arm64. This crosses a package boundary, so
inlining is measured rather than assumed: go build -gcflags=-m
./internal/reducer/... reports unique can inline names 1378 -> 1378, compared as a
SET with comm in BOTH directions rather than by totals, since a matching total is
also what a swap looks like. Zero names lost, zero gained -- the identical set is
what a pure relocation with no forwarders produces, and both sets carry 1378
entries so the probe is not vacuous. Reducer root drops 515 -> 507 and the dirgate
row is re-pinned DOWN to 507 / 59186458cff3, re-derived with the tool's own algorithm rather than hand-computed.

No-Observability-Change: the move relocates the search-document writer and its
timing accumulator without altering either. The existing coverage rows for
eshu_search_document_writer.go, _index_writer.go and _write_timings.go are
repointed to the new path in the same change, and the signals they name --
eshu_dp_search_index_mutations_total, eshu_dp_search_index_errors_total and
eshu_dp_search_index_write_duration_seconds -- are unchanged.

No-Regression Evidence: the reducer writer-primitive hoist (#6061) moves the two
fact-writer primitives reducerWriterNow and reducerFactCollectorKind out of
workload_identity_writer.go into `internal/reducer/factwrite` as Now and
CollectorKind, with no logic change; the root keeps both under their original
unexported names in reducer_fact_write_compat.go, the file that already forwards
factwrite.Execer and factwrite.VersionedRow, so all 22 and 20 non-test
root files that call them were untouched. Baseline `a6ff376ab`, go1.27.0 darwin/arm64. This crosses a
package boundary, so inlining can genuinely shift and is measured rather than
assumed: `go build -gcflags=-m ./internal/reducer/...` reports unique `can inline`
names 1376 -> 1378, compared as a SET with `comm` in BOTH directions rather than by
totals, since a matching total is also what a swap looks like. Zero names lost.
Two gained -- `CollectorKind` and `reducerWriterNow`, the latter because it is now
a one-line forwarder. The hoist is a prerequisite rather than a cleanup: a trial
move measured the eshu_search family (8 non-test files) as blocked on six
symbols, of which these two were the only ones without a leaf owner already; the
other four resolve to `contract` and `payloadcore`.

No-Observability-Change: neither function emits a metric, span, or log, and
neither performs I/O. They compute a UTC timestamp and a normalized column value
that the batch writers stamp on rows; the instrumentation on that path lives with
the writers and is unchanged.

No-Regression Evidence: the reducer sharedintent hoist (#6061) moves
SharedProjectionIntentRow, SharedProjectionIntentInput, BuildSharedProjectionIntent,
stableIntentID (exported as StableIntentID in the leaf), SharedProjectionAcceptanceKey and the Row.AcceptanceKey method out
of shared_projection.go into a new `internal/reducer/sharedintent` leaf, with no
logic change; the root keeps aliases under the original names plus one forwarder,
so no caller changed. Baseline `38b745974`, after `579d17cbb`, go1.27.0
darwin/arm64. This crosses a package boundary, so inlining can genuinely shift and
is measured rather than assumed: `go build -gcflags=-m ./...` whole-module reports
unique `can inline` names 11825 -> 11826, compared as a SET with `comm` in both
directions rather than by totals, since a matching total is also what a swap looks
like. Zero names lost. One gained -- `BuildSharedProjectionIntent`, which became
inlinable because it is now a one-line forwarder to `sharedintent.Build`, the same
effect the packagesourcecore hoist below had on `extractPackageSourceRepositories`.
The probe was confirmed non-vacuous (both sets over 11000 names) before that zero
was believed. Correctness: `go build ./...` and `go vet ./...` both exit 0 with no
output, `go test ./internal/reducer/...` passes 9 packages, and six new leaf tests
pin the behaviour that would otherwise break silently -- StableIntentID against an
exact digest (it keys every intent already persisted in Postgres, so a
serialization change orphans in-flight rows rather than updating them), its
insensitivity to map insertion order, IdentityKey altering the hashed identity but
not the stored partition key, and AcceptanceKey reporting false rather than a
zero-value key. Input shape and terminal row counts are unchanged by construction:
no query, Cypher, batch size, worker count, lease, or queue behaviour is touched,
and the reducer root non-test file count is unchanged at 519, so the dirgate ledger
needs no edit. Why this hoist at all: those three symbols are referenced by 47
non-test root files across roughly 23 domains, and a family that must import the
root to name an intent shape closes an import cycle -- the most common single
blocker among the remaining #6061 moves.

No-Observability-Change: no metric, span, log field, status field, or runtime knob
changes. The moved code is plain data plus two pure functions; the worker, runner,
readiness, lease-heartbeat, unroutable-quarantine and batch-selection machinery
that carries this domain's instrumentation all stays in shared_projection.go, and
the intent rows it emits are byte-identical because StableIntentID is pinned.
No-Regression Evidence: the reducer codeintel move (#6061) relocates eleven
files -- `code_reachability_projection*.go` and `code_root_verdicts*.go`, four
non-test and seven test -- from the reducer root into
`go/internal/reducer/codeintel/` with `git mv` and no logic change. Baseline
`5b1f4381d`, after `ae9ebc2e6`, go1.27.0 darwin/arm64. This is the first move in
this epic that crosses a package boundary rather than hoisting helpers, so calls
that were intra-package become cross-package and inlining can genuinely change;
that is measured here rather than assumed. `go build -gcflags=-m ./...`
whole-module, unique `can inline` names: 11825 base -> 11825 after, and the two
name SETS are identical -- zero lost, zero gained, verified by `comm` in both
directions rather than by comparing totals, because a matching total is also
what a swap looks like. The probe was confirmed non-vacuous (both sets over
11000 names) before that zero was believed. Correctness: `go build ./...` and
`go vet ./...` both exit 0 with no output, `go test ./internal/reducer/...` and
`go test ./internal/storage/postgres/... ./cmd/reducer/... ./internal/query/...`
both exit 0 across 18 packages with no FAIL line, and `reducer/codeintel` itself
passes in 1.7s. Input shape and terminal row counts are unchanged by
construction: no query, Cypher, batch size, worker count, lease, or queue
behaviour is touched, and the moved code is the same bytes modulo its package
clause and import qualifiers -- ten of the eleven files are `R099` renames to
git, and the eleventh is `R087` only because it is an external test package that
requalifies every type reference. Safety rests on that mechanical equivalence
plus the unchanged inlining sets, not on the test suite alone.

No-Observability-Change: no metric, span, log field, status field, or runtime
knob changes. The two `telemetry-coverage.md` rows this move touches are path
repoints only (the root path for `code_root_verdicts.go` ->
`.../codeintel/code_root_verdicts.go`), with their prose and their
`No-Observability-Change` justifications unchanged; the code-root verdict
builder and the route-liveness join remain pure in-process functions covered by
the CodeReachability projection runner's existing `eshu_dp_reducer_executions_total`,
`eshu_dp_reducer_run_duration_seconds`, and Postgres query spans, and neither
emits a metric of its own before or after the move.

No-Regression Evidence: the reducer packagesourcecore hoist (#6379, #6061)
extracts `packageSourceHint`, `packageSourceRepository`,
`extractPackageSourceRepositories`, `matchPackageSourceRepositories`, and
`canonicalPackageSourceURLKey` out of `package_source_correlation.go`, with no
logic change. `packageSourceRepositoryIDFromScope` moves with them: it is a
pure dependency `extractPackageSourceRepositories` needs and has no
independent reason to stay in root, and its one other caller
(`supply_chain_impact_python_reachability.go`) keeps reaching it through the
same root forwarder every other caller uses. The five named symbols were not
extracted whole as a family because `BuildPackageSourceCorrelationDecisions`
and the handler that classifies a hint into a correlation outcome are called
only from `package_source_correlation.go` and
`package_source_correlation_handler.go` themselves (649 lines together), while
seven other reducer-root files read these symbols directly and never call that
handler, each needing a different subset verified against actual call sites:
`package_consumption_correlation.go` and `package_publication_correlation.go`
read `Repository`/`Hint` and `ExtractRepositories`;
`container_image_identity_provenance.go` reads `Hint`, `Repository`,
`ExtractRepositories`, `MatchRepositories`, and `CanonicalURLKey`;
`container_image_identity_slsa.go` reads only `ExtractRepositories`;
`service_catalog_correlation_classify.go` and
`service_catalog_correlation_lookup.go` read only `CanonicalURLKey`; and
`supply_chain_impact_python_reachability.go` reads only
`RepositoryIDFromScope`. A family move would drag the handler's ~650 lines
along to deliver these ~65. Bodies are unchanged except for renames
(capitalized identifiers) and one direct call pulled inline:
`extractPackageSourceRepositories` now calls `payloadcore.PayloadStr` and
`payloadcore.FirstNonBlank` directly rather than through the root's
`payloadStr`/`firstPackageSourceURL` forwarders, matching the PR1
(`payloadcore`) precedent of repointing a moved caller straight at the
already-extracted leaf it depends on rather than leaving it going back through
root. `extractPackageSourceHints` (unmoved, stays in root) was also repointed
from `firstPackageSourceURL` to `payloadcore.FirstNonBlank` directly, and
`firstPackageSourceURL` itself was deleted rather than left with one caller --
review caught that leaving it live would have meant one behavior
(trim-and-first-non-empty) with two implementations in two packages and no
compiler signal if they drifted, which is exactly what this issue's own
"duplicating them would fork behaviour with no compiler signal" argument
warns against. `git diff --name-only <base>..HEAD -- testdata/ specs/` is
empty, so the golden-corpus recordings and the end-to-end snapshot cannot
move. Whole-module `go build ./...` and `go vet ./...` exit 0, and
`go test ./internal/reducer/... -count=1` and `go test ./... -count=1` both
exit 0. `packagesourcecore` carries its own `source_test.go` covering all four
exported functions, including two mutation-proven pins on the behaviors that
diverge from a naive reimplementation: `RepositoryIDFromScope` returning the
whole trimmed scope (not `""`) when unprefixed, and `MatchRepositories`
partitioning stale matches rather than filtering them out.

Codegen: measured, not assumed, per the PR4 precedent below, on both a
narrow and a whole-tree scope. Re-derived after this branch was rebased onto
a newer `origin/main` mid-review (absorbing #6372's schemadecode hoist,
which also touches `internal/reducer`'s file count) -- the branch's earlier
base (`1f0e1e172`) is no longer this PR's merge-base and its numbers are not
comparable to this head. `go build -a -gcflags=-m ./internal/reducer`,
counting `inlining call to` on go1.27.0, reports 13858 sites on this PR's
current base (`07995985e`, this branch's actual merge-base after the rebase)
and 13860 here: up 2 -- the identical delta the pre-rebase measurement found,
because this PR's own changes touch none of the files the rebase's three
intervening commits touched. This narrow, non-recursive invocation never
covers `packagesourcecore` itself on either ref. The whole-tree
`go build -a -gcflags=-m ./internal/reducer/...` reports 14568 base -> 14576
head: up 8, matching a separate-context review's own independently measured
delta of the same two numbers (offset by the same rebase-driven base shift).
The per-name set difference on the whole-tree run accounts for every delta:
`extractPackageSourceRepositories` (+6) and `matchPackageSourceRepositories`
(+2) are the new one-line forwarders appended to the end of
`package_source_correlation.go` -- kept in that file rather than a separate
compat file so the reducer root's dirgate-pinned file count (519, unchanged)
does not grow -- now inlined at every one of their call sites (verified
against that file and each caller's file:line).
`packageSourceRepositoryIDFromScope` (+1) is the same shape for the one
remaining root caller, `supply_chain_impact_python_reachability.go:97`.
`CanonicalURLKey` (+2) is `packagesourcecore.MatchRepositories`'s own two
internal call sites, newly visible because the whole-tree scope now compiles
the leaf itself. `packagesourcecore.CanonicalURLKey` (+4) is a nested inline
one level up: at every site where the `canonicalPackageSourceURLKey` forwarder
itself gets inlined (`container_image_identity_provenance.go:88`,
`service_catalog_correlation_lookup.go:24`,
`service_catalog_correlation_classify.go:222`, plus the forwarder's own
definition in `package_source_correlation.go`), the call it makes to
`packagesourcecore.CanonicalURLKey` inlines a second level into the same site.
`canonicalPackageSourceURLKey` itself drops from 5 to 3 call sites: the 2 it
loses are the calls that used to live inside `matchPackageSourceRepositories`'s
own body, which moved to the leaf and now calls `CanonicalURLKey` directly
(the +2 above). `payloadStr` (250->245, down 5) loses exactly the 5 call
sites that lived inside the old `extractPackageSourceRepositories` body, which
now calls `payloadcore.PayloadStr` directly instead.
`payloadcore.FirstNonBlank` (88->90, up 2) gains one call from
`extractPackageSourceRepositories`'s move and one from
`extractPackageSourceHints`'s repoint (both described above);
`firstPackageSourceURL` (2->0) loses both of its call sites and the
definition itself is deleted. `strings.TrimPrefix` and its runtime-inlined
`stringslite.*` siblings show no net delta on the whole-tree scope: the one
call inside `packageSourceRepositoryIDFromScope`'s body moved from root to
`packagesourcecore.RepositoryIDFromScope`, not away from the reducer tree, so
a scope that covers both sides sees no change (the earlier narrow,
root-only measurement had reported this as -1, which was correct for that
narrower scope but not for the tree as a whole). The `can inline` definition
set (`sed -nE 's/.*: can inline //p' | sort -u`, whole-tree) confirms every
side is accounted for: `firstPackageSourceURL` is a genuine loss (deleted).
`extractPackageSourceRepositories.func1` also leaves the set, but this is a
RENAME, not a loss -- the same `sort.SliceStable` comparator closure reappears
as `ExtractRepositories.func1` in the gained set, caught by re-checking a
prior pass's set-difference read that had misread this same rename as a
regression. Four more names join the set:
`CanonicalURLKey`, `extractPackageSourceRepositories`,
`matchPackageSourceRepositories`, and `packageSourceRepositoryIDFromScope`,
all newly inlinable one-liners. Every entry on both sides is accounted for by
the move itself; no caller lost inlinability.

No-Observability-Change: no metric, span, log field, status field, or runtime
setting is added, removed, or renamed. The package-source correlation
decisions built from these values stay covered by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.
Two telemetry-coverage rows are added: one for the new leaf source file, one
for the root's forwarders (which live in `package_source_correlation.go`, not
a separate file). Neither is a repoint of a prior row -- no row named either
file before this PR.

No-Regression Evidence: the reducer factwrite hoist (#6061 PR4) moves the
batched fact-write path and extracts the Execer port, with no logic change. The
two batch-insert files move whole by `git mv`. batch_insert_versioned.go is
rename-only: capitalizing the exported identifiers is its only edit.
batch_insert.go also gained new godoc — on `BatchInsertSource` (:118-129),
`BatchInsertConflict` (:185-187), `BatchInsertQuery` (:206-207), `ChunkArgs`
(:332-336), and a rewritten opening on `BatchInsertPrefix` (:12) — so the only
STATEMENT edits, not body edits, are capitalizing the exported identifiers.
The statement fragments, the ON CONFLICT target, the chunk size, and the
last-write-wins deduplication by fact ID are byte-identical, which matters more
here than anywhere else in this stack:
deduplication is a correctness requirement, not a tuning knob, because two rows
sharing a fact ID in one batch collide on the conflict target and fail the whole
chunk. `git diff --name-only <base>..HEAD -- testdata/ specs/` is empty, so the
golden-corpus recordings and the end-to-end snapshot cannot move. Backend and
version unchanged (NornicDB plus Postgres, default local profile); input shape
unchanged; terminal queue and row counts unchanged, because no queue, lease, or
transaction-scope behavior is touched.

Codegen: measured, not assumed, because PR1 established that a forwarder can
cost a CALLER its inlinability by raising it past Go's budget of 80. A net
count cannot find a named regression, so the set difference is what follows.
`go build -a -gcflags=-m ./internal/reducer`, counting `inlining call to` on
go1.27.0, reports 14322 sites on this PR's base (the factload head) and 14337
here: up 15. The head measurement EXCLUDES factwrite itself — this narrow,
non-recursive `-gcflags=-m ./internal/reducer` never covers a subpackage on
either ref — so it says nothing about factwrite's own inlining, only about
the reducer root's.

The `can inline` set (`sed -nE 's/.*: can inline //p' | sort -u`) confirms no
caller lost inlinability. 4 names leave the root's set: the two
`dedupeReducerFactRowsByFactID` generic instantiations and two closures
(`reducerBatchInsertFacts.func1`, `reducerBatchInsertVersionedFacts.func1`)
that lived inside the old, now-moved function bodies. All four are
definitions that moved, not calls that regressed; the closures disappear
because the root functions that contained them are now one-line forwarders
with no inner closure of their own. 6 names join it: the same
`dedupeReducerFactRowsByFactID` generic function reappears twice — both
instantiated at `factwrite.Row` (`reducer_fact_write_compat.go:65`), once
reported under its named type and once under its gcshape form, now that
`reducerFactRow` is an alias to `factwrite.Row` rather than a locally declared
type — plus the four new compat forwarders
(`reducerBatchInsertFacts`, `reducerBatchInsertVersionedFacts`,
`execReducerFactChunk`, `reducerFactChunkArgs`), which are now inlinable
one-liners. Every entry on both sides is accounted for by the move itself;
none is an unrelated caller. The only negative call-site deltas are
`fmt.Errorf` (933 -> 931) and `errors.New` (1013 -> 1011), both by 2, matching
the two `fmt.Errorf` wrapper calls that left the root's own body when
`ExecChunk` and `execVersionedChunk` moved with their statements. No
regression.

No-Observability-Change: no metric, span, log field, status field, or runtime
setting is added, removed, or renamed. The batch statements stay covered by
`eshu_dp_postgres_query_duration_seconds` and the owning pass by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.
One telemetry-coverage row named a moved file and is repointed rather than
duplicated; two further rows are added for the new files that had none.

No-Regression Evidence: the reducer factload hoist (#6061 PR3) moves the scoped
fact loader and extracts the FactLoader port, with no logic change. The loader
file moves whole by `git mv`; the only body edits are capitalizing the exported
identifiers and calling payloadcore directly where a PR1 forwarder stood.
`git diff --name-only <base>..HEAD -- testdata/ specs/` is empty, so the
golden-corpus recordings and the end-to-end snapshot cannot move. Backend and
version unchanged (NornicDB, default local profile); input shape unchanged;
terminal queue and row counts unchanged, because the push-down and fallback
behavior is byte-identical and no queue, lease, Cypher, or projection path is
touched. The retry classification is unchanged, which is the one behavior here
that could alter runtime shape: widening it would loop and narrowing it would
dead-letter a scope generation.

Codegen: this PR adds forwarders, and PR1 measured that a forwarder can cost a
CALLER its inlinability by raising it past Go's budget of 80. The net count
rises — `go build -a -gcflags=-m ./internal/reducer` counting `inlining call to`
gives 14248 on this PR's base and 14322 here on go1.27.0 — but a net count
cannot detect a named loss, so the set difference is what follows.

Three functions gain inlinability (`loadFactsForKinds`,
`loadFactsForKindAndPayloadValue`, `classifyFactLoadError`, now one-line root
forwarders). Five LOSE it, at cost 77→94 (78→95 for
`loadDocumentationMaterializationFacts`) against budget 80:

- `loadCodeownersOwnershipMaterializationFacts` (codeowners_ownership_delta_scope.go)
- `loadDocumentationMaterializationFacts` (documentation_edge_delta_scope.go)
- `loadRationaleMaterializationFacts` (rationale_delta_scope.go)
- `loadShellExecMaterializationFacts` (shell_exec_materialization.go)
- `loadSubmodulePinMaterializationFacts` (submodule_pin_delta_scope.go)

The mechanism inverts PR1's. At base, root `loadFactsForKinds` had a real body
and was not inlinable, so calling it cost a caller about 57. Here it is a
one-line forwarder that IS inlinable, so the compiler inlines it and the
inlined cross-package call pushes each of the five callers from 77 to 94.

Accepted rather than fixed, and the reason is the shape of those five: each is
a thin wrapper that immediately issues a store read, so one non-inlined call is
nanoseconds against a millisecond query. That is an argument from shape, not a
benchmark — the magnitude is not measured. `//go:noinline` on the forwarders
would restore the costs and is a hack; repointing the callers at `factload`
directly would widen this PR past its scope. The four `retryableFactLoadError`
methods also leave the `-m` output, but only because they changed package.

The probe, so the next reader can reproduce it rather than trust the list. The
inline LIST (function names) reproduces with plain `-m`, but the COST figures
above came from `-m=2`: on go1.27.0, plain `-gcflags=-m` prints neither costs
nor "cannot inline" lines, only bare `can inline <name>` lines for functions
that stayed inlinable.

```
go build -a -gcflags=-m ./internal/reducer 2>&1 \
  | sed -nE 's/.*: can inline //p' | LC_ALL=C sort -u
```
then `comm -23 base head` for the name list; `go build -a -gcflags=-m=2
./internal/reducer` for the cost figures. Whole-module `go build
./...` and `go vet ./...` exit 0 and the reducer tree tests green.

No-Observability-Change: no metric, span, log field, status field, or runtime
setting is added, removed, or renamed. Loader reads stay covered by
`eshu_dp_postgres_query_duration_seconds` and the owning pass by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.

No-Regression Evidence: the reducer factdecode hoist (#6061 PR2) is a
relocation of the decode-failure classification and per-fact quarantine
mechanism, with no logic change. The renames in decode_error.go are case-only,
so lowercasing both sides is an exact equivalence test and its moved bodies are
identical to the base commit under it (positive control: appending one line
turns the check red). The other two regions need more than that test and were
checked separately: quarantine_record.go additionally requalifies `Domain` to
`reducercontract.Domain` — an alias of the same type, declared at
`go/internal/reducer/intent.go:9`, so the signature is unchanged — and
quarantine_writer.go differs only by rewritten file-location comments. `git diff --name-only <base>..HEAD -- testdata/ specs/` is empty, so the
golden-corpus recordings and the end-to-end snapshot cannot move. The repo-wide
reducer test-function inventory is byte-identical across the move, 3232
functions on both sides, so no coverage was lost when the quarantine tests
relocated. Backend and version unchanged (NornicDB, default local profile);
input shape unchanged; terminal queue and row counts unchanged, because no
queue, lease, Cypher, or projection path is touched. Whole-module `go build ./...` and `go vet ./...` exit 0, and the reducer tree
(`reducer`, `contract`, `dsl`, `factdecode`, `payloadcore`, `tags`, `tfstate`)
plus `storage/postgres`, `query` and `projector` test green.

Codegen: this PR adds seven forwarders, and PR1 established that a forwarder
can cost a CALLER its inlinability, so the set difference was measured rather
than assumed. `go build -a -gcflags=-m ./internal/reducer` gives 1329
can-inline entries on the base and 1330 here. NO caller lost inlinability:
the four entries that leave the list are the moved symbols themselves
(`FactDecodeError.FailureClass`, `.Retryable`, `.Unwrap`,
`quarantineWriterFromContext`), now reported under factdecode. The root
forwarders are inlinable and their cross-package targets inline into them, so
the two-hop path collapses.

No-Observability-Change: no metric, span, log field, status field, or runtime
setting is added, removed, or renamed. The quarantine counter
`eshu_dp_reducer_input_invalid_facts_total` is emitted by the same code at its
new path, and the two telemetry-coverage rows that named the moved files are
repointed rather than duplicated.

No-Regression Evidence: the reducer payloadcore hoist (#6061 PR1) is a symbol
eviction, not a logic change. Baseline and after are the same behavior by
construction and that is asserted, not assumed: all 28 moved function bodies are
character-identical to the merge base once every moved identifier the body
references is capitalized, not only the declared one (six bodies call or
reference a sibling that moved with them: PayloadOrderedStrings, PayloadBool,
SupplyChainWorkloadIDsFromPayload, OCIRepositoryID, SourceOrderKey and
PreferMaxSourceOrderKey; the remaining three of the 31 symbols are consts and
have no body) (checked mechanically against `git show origin/main:<file>` for each,
with a seeded-defect positive control — removing `PayloadStr`'s `"<nil>"` guard
turns the check RED naming that line, reverting turns it GREEN), and
`git diff --name-only origin/main..HEAD -- testdata/ specs/` is empty, so every
B-7 cassette and the B-12 snapshot
(`testdata/golden/e2e-20repo-snapshot.json`, sha256
`d6329f7bce08e71319262319fcf435bd4a6770167b081550df691b1a84e76802`) are
byte-identical. Backend and version are unchanged (NornicDB, default local
profile); input shape is unchanged; terminal queue and row counts are unchanged
because no queue, lease, Cypher, or projection path is touched. The diff is
definitions, import blocks, and a mechanical call-site rewrite: 88 call sites
across 31 root files change from `firstNonBlank(...)` to
`payloadcore.FirstNonBlank(...)`, plus a handful of other sites repointed at
payloadcore to keep their callers inlinable. These are behavior-preserving
token swaps, not unchanged lines — the distinction matters for anyone scoping
later regression proof from this record. Equivalence is established by the
body comparison above, not by the call sites being untouched.

Codegen moves in both directions, and the net is positive. Measured with
`go build -a -gcflags=-m ./internal/reducer` on both refs:

- Package-wide inlined call sites rise from 12291 on main to 13985 here, on
  go1.27.0. These counts are toolchain-sensitive — go1.26.6 gives 12231 and
  13925 on the same trees — so quote the toolchain with the number.
- 16 functions GAIN inlinability. The raw set difference is 27 when the probe
  covers `./internal/reducer/...` — the bare `./internal/reducer` above emits no
  payloadcore lines and yields 16. The other 11 are
  new exported payloadcore symbols (`PayloadBool`, `CopyPayload` and the rest)
  that did not exist on main at all, so they are new code rather than a codegen
  change. Only the 16 below were functions before this PR. Not the moved helpers: they are unchanged and
  most were never inlinable (`PayloadStr` costs 148, `ToStringSlice` 188,
  `FormatTally` 233, all against a budget of 80). What became inlinable is the
  16 one-line ROOT FORWARDERS that inherited the old lowercase names —
  `payloadStr`, `payloadString`, `semanticPayloadString`, `anyToString`,
  `compactStringSlice`, `formatTally`, `toStringSlice`, `sourceOrderKey` and
  others — each now a single `return payloadcore.X(...)` whose callee is too
  expensive to inline into it. Go's inline cost is computed over the function
  body; file and package size play no part.
- 3 functions lose inlinability: `cicdWorkflowImageIsInputOnly`,
  `indexSLSAProvenanceEvidence`, `packageRepositoryName`. Their bodies are
  byte-identical to the base; only their inline cost changed, because calling a
  forwarder instead of the original raises the CALLER past budget 80. None is on
  a per-row hot path. `cicdWorkflowImageIsInputOnly` runs once per workflow
  image over two passes (`ci_cd_run_correlation_workflow_image.go:128`, its sole
  call site). `indexSLSAProvenanceEvidence` runs once per decoded
  attestation.slsa_provenance envelope (`sbom_attestation_attachment_slsa_index.go:28`,
  its sole call site). `packageRepositoryName` runs once per dependency-manifest
  fact inside the per-envelope loop (`package_consumption_correlation.go:257`).
  None is the shared-projection retract path

  Four further symbols are inlined at fewer sites — `derefString` -3,
  `payloadBool` -2, `strings.HasPrefix` -3, `trimmedCICDPtr` -1. None of the
  four LOST inlinability: the lost set is exactly the three above plus the
  deleted `firstNonBlank`, so these are changes in how often a still-inlinable
  function was inlined, not new regressions. Three distinct mechanisms produce
  them, and only the first is a codegen change at all. A lost caller takes its
  callee's inlined site with it: `cicdWorkflowImageIsInputOnly` calls
  `trimmedCICDPtr` (-1), and `indexSLSAProvenanceEvidence` calls `derefString`.
  A call MOVED out of the measured package: `payloadcore/identity.go` contains
  exactly three `strings.HasPrefix` calls, which is the whole of that -3 — the
  probe measures `./internal/reducer`, so a call that relocated into the
  subpackage stops being counted, and nothing about it got slower. And a call
  site was deleted: repointing `rowUsesRefreshFence` at `payloadcore.PayloadBool`
  removed its one source call to `payloadBool`. That single deletion accounts
  for the whole -2, because `rowUsesRefreshFence` was itself inlined at one
  site, so `payloadBool` earned two inline reports from it — its own body and
  the inlined copy. Source call sites go 10 to 9; inline reports go 11 to 9. All four are attributed. `payloadBool` -2 is not a lost
  inline at all but this PR's own repoint: `rowUsesRefreshFence` is inlinable on
  BOTH refs, and the two vanished reports are its own body and its single
  inlined copy, which called `payloadBool` on main and call
  `payloadcore.PayloadBool` here. `derefString` -3 splits across two lost
  functions, not one — `indexSLSAProvenanceEvidence` carried two of them and the
  `cicdWorkflowImageIsInputOnly` inline chain the third.

  The probe-scope caveat matters for reading any of this: `-gcflags=-m` reports
  only the package named on the command line, so a call that moved into
  payloadcore leaves the count without anything getting slower. That is the
  whole of `strings.HasPrefix` -3, and the same effect shows up in
  `strings.TrimPrefix` -1, `slices.Sort` -1 and the `time` internals -1 each.

  Five more functions lost inlinability in an earlier revision and were FIXED
  rather than measured, by repointing them at payloadcore directly so the
  forwarder hop disappears: `rowUsesRefreshFence`, `payloadBoolPointer`,
  `collapsedObservabilityValue`, `crossplaneEntityMetadataString` and
  `mapStringValue`. `rowUsesRefreshFence` is the one that mattered — it runs
  once per row on the shared-projection retract path, and an aggregate inline
  count cannot establish no-regression for a per-row loop however favourable the
  total looks. It is inlinable again and inlining at its retract-path call site,
  verified with `-gcflags=-m`.

One forwarder does not exist for the same reason. `firstNonBlank` is inlinable
in the reducer root at cost 78; a forwarder around `payloadcore.FirstNonBlank`
costs 82, over Go's budget of 80, because the callee inlines into its own
forwarder. Keeping it would have dropped ALL 88 of its inlined call sites to 0.
Its 88 call sites across 29 non-test root files call the package directly instead, which
holds the count at 88 on both refs — parity with main, not an improvement.

No-Observability-Change: no metric, span, log field, status field, or runtime
setting is added, removed, or renamed. The four new `payloadcore` files are
pure helpers that emit no signal; reducer execution stays covered by
`eshu_dp_queue_claim_duration_seconds`, `eshu_dp_reducer_queue_wait_seconds`
and `eshu_dp_queue_depth`, and shared projection by
`eshu_dp_shared_projection_cycles_total` and
`eshu_dp_shared_projection_step_seconds`, as the four rows added to
`docs/public/observability/telemetry-coverage.md` record.

No-Regression Evidence: the collector restructure is a file move. Baseline and
after measurement are the same tree by construction, and that is asserted rather
than assumed: `testdata/golden/e2e-20repo-snapshot.json` and every cassette
under `testdata/cassettes/` are byte-identical to the merge base (empty
`git diff`, snapshot sha256 `42e75ccf5a34f69d0f92a2e81cc53c0e281f70c37e4dc444d60dddeaf3c0826c`
on both sides, identical `git ls-tree` digests), so the B-12 contract the B-7
golden-corpus gate diffs against did not move. Backend and version are unchanged
(NornicDB, default local profile); input shape is unchanged (the same 20-repo
corpus); terminal queue and row counts are unchanged because no reducer,
projector, queue, lease, or Cypher path is touched — the diff is `git mv` plus
import-path and qualifier edits, with 211 git-detected renames and every
modified Go line a `collector.X` -> `gitrepo.X` swap. Whole-module `go build
./...` and `go vet ./...` exit 0 and the collector tree plus its `cmd/` callers
run green over 485 packages. A restructure that changed projected truth would
surface as a B-12 diff; there is none.

No-Observability-Change: no metric, span, log field, status field, or runtime
setting is added, removed, or renamed. The `#nosec`, `//nolint:` and `//go:build`
annotation sets are identical before and after (538 `#nosec` with matching
per-code breakdown, 103 `//go:build`), and every telemetry-coverage row that
cited a moved file now cites its new path, with five new rows for the files this
change creates — four of them inert type/helper files, and
`gitrepo/gitmodel/factstream.go` mapped to `eshu_dp_workflow_claim_facts_emitted_total`
because its `Send` increments the counter feeding `CollectedGeneration.FactCount()`.
`scripts/verify-telemetry-coverage.sh` exits 0.

**projector (188) + coordinator (124):** projector's per-provider intent
families are measured clean (zero cross-family calls; all 44 calls fan out
from `scope_generation_intents.go` across 41 family files). Root keeps `canonical*`,
`runtime_*`, `stage_*`, decode helpers, payload readers, the dispatcher,
and failure/retry infra (~70 files). Hazard: canonical Row types are
consumed by 182 external files; family moves need qualifier updates or root
aliases, and the `canonical.go` exact-path gate trigger (#5531) moves in
lockstep. Azure, GCP, Kubernetes, EC2, RDS, S3, security,
workload-cloud-relationship, incident-routing, and AWS-relationship intent
builders now use the neutral
`internal/projector/intent` boundary while root retains assembly, lifecycle,
enqueue, retry, and telemetry. EC2's `USES_PROFILE`
builder is the first extracted family to need a typed-payload decode; it
keeps its own local decode call against `sdk/go/factschema` rather than
importing root's classified decode wrapper, since importing root would create
the same cycle `ReducerIntent` and `FactLookup` route around. S3's LOGS_TO,
external-principal-grant, and internet-exposure intent builders moved into
`internal/projector/s3` the same way; its LOGS_TO builder is the second family
to need a typed-payload decode and follows the same local-decode pattern. All
three S3 builders share the generic `aws_resource_materialization:<scope>`
entity key rather than a family-distinct one, since they gate on the same AWS
`CloudResource` canonical-nodes phase the reducer publishes for every
AWS-provider scope. RDS's single posture-materialization builder moved into
`internal/projector/rds` the same way, sharing that same generic entity key;
unlike EC2's `USES_PROFILE` and S3's `LOGS_TO`, it triggers on
`rds_instance_posture` fact-kind presence alone and needs no typed-payload
decode wrapper, so the move is a one-file, one-caller extraction with no
`factschema_decode_aws.go` companion. The workload-cloud-relationship builder
moved into `internal/projector/workloadcloud` on that same shape: it triggers
on mere `aws_resource` fact presence, decodes no payload, and shares the
generic `aws_resource_materialization:<scope>` entity key with the S3 and RDS
builders. The incident-routing builder moved into
`internal/projector/incidentrouting` on the same presence-only shape: it
triggers on `incident.record` plus the `incident_routing.*` kinds
`internal/facts` registers, anchors with the cross-kind `FirstAcrossKinds`
lookup the security-alert builder also uses, keys on its own
`incident_routing_materialization:<scope>` entity, and decodes no payload.
The AWS relationship builder moved into `internal/projector/awsrelationship`
on the same presence-only shape: it triggers on `aws_relationship` fact
presence, anchors with `FirstOfKind`, decodes no payload, and keeps the shared
`aws_resource_materialization:<scope>` entity key on purpose, because the
reducer's edge handler gates on the canonical-nodes-committed row the AWS node
builders publish under that key. The root `awsCloudRuntimeDriftSourceSystem`
helper it called stays at root for its seven remaining root callers; the child
uses the body-identical `projectorintent.SourceSystem`.
The IAM CAN_ASSUME builder moved into `internal/projector/iamcanassume` as the
third decode-bearing family: it anchors with `FirstOfKindMatching` on the
earliest `aws_iam_permission` fact whose payload decodes with
`policy_source == "trust"`, skipping identity statements and undecodable
facts, and keeps the shared `aws_resource_materialization:<scope>` entity key
for the same node-before-edge readiness reason. Unlike EC2 and S3, whose root
decode wrappers had other root callers, root's `factschema_decode_iam.go` had
this builder as its only caller, so the wrapper moved with it (still named
`factschema_decode_iam.go` so the payload-usage manifest glob finds it) and
root keeps no copy. `awsCloudRuntimeDriftSourceSystem` stays at root for its
five remaining root callers.
The package-source-correlation builder moved into
`internal/projector/packagesource`. It is the first probe in the ordered
fan-out and carries no decode seam: it anchors with `FirstOfKind` on the
earliest `package_registry.source_hint` fact and falls back to the earliest
`package_registry.package` fact, reading only the fact kind. Its private
`packageSourceCorrelationSourceSystem` helper had no other root caller and
was body-identical to `projectorintent.SourceSystem`, so it was dropped
rather than moved. The `packageIdentityEnvelope` test fixture stays at root
because the fan-out and supply-chain-impact tests still build on it.
The service-catalog-correlation builder moved into
`internal/projector/servicecatalog`. It triggers on any fact kind the
`facts.ServiceCatalogSchemaVersion` registry recognizes, anchoring with
`FirstMatchingKindPredicate` on the earliest such fact in input order, and
carries no decode seam. Its private `serviceCatalogCorrelationSourceSystem`
helper was checked body-for-body against `projectorintent.SourceSystem` and
was not identical: it carries a third fallback to the ingestion scope's
`SourceSystem`, so it moved with the family unchanged and the child builder
takes the scope value the way the `kubernetes` builders already do. The root
`firstMatchingKindPredicate` forwarder stays for its three remaining root
callers. The unsupported-schema-version regression test stays at root in
`schema_version_admission_test.go` because it asserts root's
`validateFactSchemaVersion`, not the builder.
Coordinator `_scheduler.go` halves extract cleanly
(they implement a root Planner interface); the `_service.go` halves are
methods on the shared `Service` struct and stay until Service is
decomposed — a design decision, not a file move. Shared plan-key validation now
lives in dependency-neutral `internal/coordinator/plannercontract`. The CI/CD
run scheduler now demonstrates the first provider extraction under
`internal/coordinator/cicdrun`: the child owns its request and planner while
root keeps the structural interface, scheduling order, durable open-target
admission, retry, and telemetry. The provider security-alert scheduler is the
second extraction under `internal/coordinator/securityalert`: the child owns
its request and planner while root keeps the same scheduling, plan-key,
admission, retry, and telemetry responsibilities. The hosted SBOM-attestation
scheduler is the third extraction under `internal/coordinator/sbomattestation`
with the same boundary. The Vault metadata scheduler is the fourth extraction
under `internal/coordinator/vaultlive`; its pure planner moves while root keeps
scheduling, admission, retries, and telemetry. The Grafana Tempo scheduler is
the fifth extraction under `internal/coordinator/tempoplanner`; its
deterministic request validation, target filtering, and workflow-row
construction move while root keeps service scheduling, the plan-key clock,
tenant and egress filtering, durable admission, retries, and telemetry. The
Grafana Loki scheduler is the sixth extraction under
`internal/coordinator/lokiplanner`; its deterministic request validation,
target filtering, and workflow-row construction move under the same ownership
boundary. The scanner-worker scheduler is the seventh extraction under
`internal/coordinator/scannerworker`; the child owns configuration validation,
requested-scope privacy, configured target order, deterministic IDs, and
fairness-key construction. Root keeps the interface, scheduling and plan-key
clock, active and claims gates, tenant-grant and collector-egress gates, durable
admission, retries, queue and lease behavior, and telemetry. The
Prometheus/Mimir scheduler is the eighth extraction under
`internal/coordinator/prometheusmimir`; the child owns all five request fields,
enabled-target validation and filtering, configured order, deterministic IDs,
requested-scope privacy, trigger precedence, and per-target fairness keys. Root
keeps scheduling order, its plan-key clock, tenant and egress filtering,
empty-item admission skips, durable admission, retries, queue and lease
behavior, and telemetry. The Grafana scheduler is the ninth extraction under
`internal/coordinator/grafanaplanner`; the child owns all five request fields,
all-target validation before disabled and scope filtering, configured work-item
order, deterministic IDs, requested-scope privacy, trigger precedence, and the
target-instance-to-scope fairness fallback. Root keeps scheduling order, its
plan-key clock, collector-egress filtering, tenant-grant authorization,
empty-item admission skips, durable admission, retries, queue and lease
behavior, and telemetry. PagerDuty and Jira are the tenth and eleventh
extractions under `internal/coordinator/pagerdutyplanner` and
`internal/coordinator/jiraplanner`. Each child owns all five request fields,
all-target validation before scope filtering, webhook-scope membership,
configured order, deterministic IDs, privacy, and trigger precedence;
PagerDuty partitions fairness by provider and Jira by site. Root keeps
scheduling, clock, policy filtering, empty-item skips, durable admission,
freshness-trigger transitions, retries, queue and lease behavior, and
telemetry. GCP is the twelfth extraction under
`internal/coordinator/gcpplanner`; the child owns request validation,
scope-configuration parsing and defaulting, duplicate and field validation,
requested-scope filtering, requested-scope privacy, and deterministic
work-item construction. Unlike the other eleven, root's own freshness handoff
loop (`service_gcp_freshness.go`, which stays in root: it is a `_service.go`
half, a set of methods on the shared `Service` struct) needed the same scope
parsing the planner owns to match an inbound Cloud Asset Inventory
change-event trigger against configured scopes, and root's config loader
needed the same parsing to validate a claim-enabled GCP instance at startup.
Both call sites now go through two new child exports built for this
purpose — `EnabledScopes` (returning a privacy-scoped `ConfiguredScope`
without content_family or the credential handle) and
`ValidateClaimSchedulerConfiguration` — rather than reaching into the child's
private configuration types, following the same export-a-query-function
precedent `jiraplanner.HasConfiguredScope` and
`pagerdutyplanner.HasConfiguredScope` set for their own freshness call sites.
Root keeps scheduling order, the plan-key clock, tenant-grant authorization,
durable admission, freshness trigger claim/handoff/reap, retries, queue and
lease behavior, and telemetry. These moves do not change scheduler order,
workflow wire values, concurrency, or observability.
The generic component extension scheduler is the thirteenth extraction under
`internal/coordinator/componentextensionplanner`, and the first to hit the
acyclic-boundary problem Part 3's prerequisite section describes for query,
reducer, projector, and mcp. `parseComponentInstanceConfig` — the shared
generic component-activation configuration parse/validate function the
scheduler's core planning function returns and every one of its helper
functions consumes — was not scheduler-owned: `component_activation_config.go`
(root) constructs values of that type when it builds a collector instance's
`Configuration` JSON, and `pagerduty_service.go` and `governance_audit.go`
(root) also read it, for reasons unrelated to component-extension
scheduling. Because root already imports the planner package for the
request type, the planner package cannot import root back, so the type
could not stay in `component_activation_config.go`; and exporting it from
the planner would make two unrelated providers depend on a
scheduler-specific package, the same shape `owned_package_target_helpers.go`
and `target_priority.go` avoid by staying in root. The fix landed as its own
commit, before the scheduler moved: the type and its parser were hoisted
into a new dependency-neutral package, `internal/coordinator/componentactivation`
(`Config`, `RuntimeConfig`, `ParseConfig`) — the same
hoist-to-a-neutral-package pattern `internal/projector/intent` already uses
for the projector families' equivalent problem. `component_activation_config.go`,
`pagerduty_service.go`, `governance_audit.go`, and `componentextensionplanner`
all import `componentactivation`; none of them imports another from this
list, and `componentactivation` imports neither `coordinator` nor
`componentextensionplanner`. Root keeps scheduling order, hosted extension
egress-policy filtering and audit, durable admission, retries, queue and
lease behavior, and telemetry. These moves do not change scheduler order,
workflow wire values, concurrency, or observability.
Terraform-state keeps its separate plan-key validator, and the root
`firstNonBlank` helper remains outside this boundary.

**mcp (338):** two layers. Registration (`tools_<domain>.go`, 43
constructors, zero lateral calls) moves cleanly. Routing is the tangle:
`dispatch.go`'s 490-line switch inlines ~20 families' routing, and
`dispatch_repositories.go` is a hidden second router fanning out to 13
other families. Wave 1: the 11 families whose Route funcs are already
isolated. Wave 2: extract per-family Route funcs out of the two hub
switches, then move. The consumer-existence gates and ~35 package-wide
contract/authz test sweeps stay in root.

The documentation registration family is the first extracted MCP family. Its
six definitions live under `internal/mcp/documentation`, while the root keeps
both existing assembly positions, documentation routing, dispatch,
authorization, and transport ownership. The move uses the dependency-neutral
`internal/mcp/toolcontract` shape and does not combine the two constructor
groups or change the 162-tool order.

The cloud registration family is the second extracted MCP family. Its inventory
and runtime-drift definitions live under `internal/mcp/cloud`, while the root
keeps both assembly positions and all cloud routing, dispatch, authorization,
and transport ownership. The move uses `internal/mcp/toolcontract` and leaves
the 162-tool order unchanged.

The visualization family is the third MCP extraction. Its definition, family
membership, and pure `routecontract` request selection live under
`internal/mcp/visualization`. The root keeps its assembly position between
work-item and freshness tools plus global fanout, its private adapter, dispatch,
authorization, summaries, transport, and all operational behavior. The
162-tool order and HTTP request remain unchanged.

The Ask family is the fourth MCP extraction. Its definition, family membership,
and pure `routecontract` request selection live under `internal/mcp/ask`. The
root keeps its assembly position after reachability and before relationship
edges plus global fanout, its private adapter, dispatch, authorization,
summaries, transport, and all operational behavior. The 162-tool order and HTTP
request remain unchanged.

The query-playbook registration family is the fifth extracted MCP family. Its
two definitions live under `internal/mcp/playbooks`, while the root keeps their
assembly position after documentation tools and before investigation workflows
plus all query-playbook routing, dispatch, authorization, and transport
ownership. The move uses `internal/mcp/toolcontract` and leaves the 162-tool
order unchanged.

The relationship family is the sixth extracted MCP family. Its three
definitions live under `internal/mcp/relationships`: the code story and
analysis definitions remain at zero-based positions 8 and 9 in the codebase
group, and the relationship-edge definition remains after Ask and before
repository files. The same child package owns `CodeRoute` and `EdgeRoute`, pure
selectors that decide family membership and convert decoded arguments into
`internal/mcp/routecontract` requests. Root keeps ordered assembly, global
fanout order, thin route adapters, dispatch, authorization, transport,
timeouts, response budgets, envelopes, and telemetry. `internal/query` keeps
relationship validation, graph reads, bounds, and response shaping. The
extraction leaves the 33-tool codebase group and 162-tool global order
unchanged.

The freshness registration family is the seventh extracted MCP family. Its
four definitions live under `internal/mcp/freshness`, while the root keeps
their assembly position after visualization and before context tools. Routing
also stays in root: `get_repository_freshness` remains in
`dispatch_repositories.go`, and the other three definitions remain in
`dispatch_freshness.go`. The move uses `internal/mcp/toolcontract` and leaves
the 162-tool order unchanged.

The semantic registration family is the eighth extracted MCP family. Its three
definitions live under `internal/mcp/semantic`, while the root keeps the
semantic-evidence and semantic-search assembly positions after investigation
packets and before documentation finding aggregates. Routing also stays in
root: the evidence pair remains in `dispatch_semantic_evidence.go`, and search
remains in `dispatch_semantic_search.go`. The move uses
`internal/mcp/toolcontract` and leaves the 162-tool order unchanged.

The investigation registration family is the ninth extracted MCP family. Its
two workflow and three evidence-packet definitions live under
`internal/mcp/investigation`, while the root keeps both assembly positions
after query playbooks and before semantic evidence. Routing also stays in root:
workflow discovery and resolution remain in
`dispatch_investigation_workflows.go`, and the three packet exports remain in
`dispatch_investigation_packets.go`. The move uses
`internal/mcp/toolcontract` and leaves the 162-tool order unchanged.

The service registration family is the tenth extracted MCP family. Its catalog
definition, three service-context and investigation definitions, and
intelligence-report definition live under `internal/mcp/service`, while the
root keeps all three assembly positions. Routing also stays split in root:
catalog correlations remain in `dispatch_repositories.go` and
`dispatch_service_catalog.go`; service context, story, investigation, and
intelligence-report routes remain in `dispatch.go` and
`dispatch_service_selector.go`. The move uses
`internal/mcp/toolcontract` and leaves the 162-tool order unchanged.

The ecosystem registration family is the eleventh extracted MCP family. Its
23 definitions live under `internal/mcp/ecosystem`, while the root keeps their
single assembly position after repository-language tools and before
infrastructure aggregates. Routing stays split across the existing root
routers: ecosystem summaries and change planning remain in
`dispatch_ecosystem.go`; repository reads remain in
`dispatch_repositories.go`, and package-registry reads moved to
`internal/mcp/packageregistry` in the first Wave 2 extraction below;
infrastructure reads remain in `dispatch.go`, and infrastructure-search
selection moved to `internal/mcp/infrasearch` in the eleventh Wave 2
extraction below; impact reads remain in `dispatch_impact.go`; and
environment comparison remains in `compareRoute`. That move uses
`internal/mcp/routecontract`, not `toolcontract`: route-selection extractions
take the routecontract seam, while `toolcontract` is what an ecosystem tool
*registration* move uses. It leaves the 162-tool order unchanged.

The package-registry route family is the first Wave 2 MCP extraction, and the
first that moves route selection without moving a registration. Its six tools
were answered by arms of the 46-arm `repositoryRoute` switch in
`dispatch_repositories.go`; family membership and pure `routecontract` request
selection now live under `internal/mcp/packageregistry`. Root keeps every tool
definition and its assembly position, global fanout order, the thin
`packageRegistryRoute` adapter, dispatch, authorization, transport, timeouts,
response budgets, envelopes, summaries, and telemetry. The adapter is consulted
at the top of `repositoryRoute` rather than as a new entry in `resolveRoute`, so
the repository router keeps its own position in the global chain and no other
family's resolution order changes. The six tool names are disjoint from the
remaining switch arms, and the 162-tool order, the advertised schemas, and every
selected method, path, and query key remain unchanged.

The CI/CD run-correlation route family is the second Wave 2 MCP extraction and
follows the same shape. Its three tools — the bounded run-correlation listing
and the two run-correlation aggregates — were answered by arms of the same
`repositoryRoute` switch, with their request builders split across
`dispatch_cicd.go` and `dispatch_cicd_aggregates.go`. Family membership, both
aggregate builders, and the private `provider_run_id`-to-`run_id` fallback now
live under `internal/mcp/cicd`, and `dispatch_cicd_aggregates.go` is gone. Root
keeps every tool definition and its assembly position, global fanout order, the
thin `cicdRoute` adapter, dispatch, authorization, transport, timeouts, response
budgets, envelopes, summaries, and telemetry. The adapter is consulted directly
after the package-registry one at the top of `repositoryRoute`, so the
repository router keeps its own position in the global chain and no other
family's resolution order changes. The three tool names are disjoint from the
package-registry family and from the remaining switch arms, and the 162-tool
order, the advertised schemas, the `limit` defaults of 50 and 100, the `offset`
default of 0, the `group_by` fallback to `outcome`, and every selected method,
path, and query key remain unchanged.

The CODEOWNERS ownership route family is the third Wave 2 MCP extraction and
the smallest: one tool, one arm of the same `repositoryRoute` switch. Its
request builder sat in `dispatch_codeowners.go` beside a private
`optionalIntString` helper that nothing else called. Family membership, the
builder, and that helper now live under `internal/mcp/codeowners`, and
`dispatch_codeowners.go` keeps only the thin `codeownersRoute` adapter. Root
keeps the tool definition and its assembly position, global fanout order,
dispatch, authorization, transport, timeouts, response budgets, envelopes,
summaries, and telemetry. The adapter is consulted directly after the CI/CD one
at the top of `repositoryRoute`, so the repository router keeps its own position
in the global chain and no other family's resolution order changes. The one tool
name is disjoint from the package-registry and CI/CD families and from the
remaining switch arms, and the 162-tool order, the advertised schema, the
`limit` default of 50, and every selected method, path, and query key remain
unchanged. The `optionalIntString` semantics move verbatim: `after_order_index`
is the numeric leg of a three-part keyset cursor the handler admits only whole,
so an absent key stays the empty string rather than taking a default, which is
why the child reimplements the helper against `routecontract.Arguments` instead
of collapsing it into `IntOr`.

The secrets/IAM posture route family is the fourth Wave 2 MCP extraction: five
tools, five arms of the same `repositoryRoute` switch -- one fewer than the
package-registry family moved -- with all five request builders sitting together
in `dispatch_secrets_iam.go` and no private helper between them. Family membership and all five builders now live
under `internal/mcp/secretsiam`, and `dispatch_secrets_iam.go` keeps only the
thin `secretsIAMRoute` adapter. Root keeps every tool definition and its
assembly position, global fanout order, dispatch, authorization, transport,
timeouts, response budgets, envelopes, summaries, and telemetry. The adapter is
consulted directly after the CODEOWNERS one at the top of `repositoryRoute`, so
the repository router keeps its own position in the global chain and no other
family's resolution order changes. The five tool names are disjoint from the
package-registry, CI/CD, and CODEOWNERS families and from the remaining switch
arms, and the 162-tool order, the advertised schemas, the `limit` default of 50,
and every selected method, path, and query key remain unchanged.

What makes this family worth reading is that it is not uniform. The four
listings page, so each carries `limit` plus its own keyset cursor and filters.
`count_secrets_iam_posture` is a scope-anchored aggregate over the whole
posture, so it carries `scope_id` and nothing else -- no `limit`, no cursor, no
filter. The tempting edit is to give it a `limit` for symmetry with its four
siblings; that compiles and reads like a consistency fix. It would not cap the
total either -- the handler reads only `scope_id` -- so the key would be inert
and would advertise a bound the endpoint does not honor. Two guards fail on a mutant that adds one and
on one that drops `scope_id`: the child's own summary test and the
dispatch-level `TestSecretsIAMPostureSummaryStaysScopeOnlyThroughDispatch`. The
adapter parity test is not one of them and cannot be -- it builds its expected
value by calling the same child selector, so a mutation moves both sides
together. What it does prove is that the adapter transcribes method, path, body,
and query faithfully, which is a different claim.

The observability-coverage route family is the fifth Wave 2 MCP extraction and
returns to the single-tool shape: one tool, one arm of the same
`repositoryRoute` switch, one request builder in
`dispatch_observability_coverage.go` with no private helper beside it. Family
membership and the builder now live under `internal/mcp/observabilitycoverage`,
and `dispatch_observability_coverage.go` keeps only the thin
`observabilityCoverageRoute` adapter. Root keeps the tool definition and its
assembly position, global fanout order, dispatch, authorization, transport,
timeouts, response budgets, envelopes, summaries, and telemetry. The adapter is
consulted directly after the secrets/IAM one at the top of `repositoryRoute`, so
the repository router keeps its own position in the global chain and no other
family's resolution order changes. The one tool name is disjoint from the
package-registry, CI/CD, CODEOWNERS, and secrets/IAM families and from the
remaining switch arms, and the 162-tool order, the advertised schema, the
`limit` default of 50, and every selected method, path, and query key remain
unchanged.

What makes this family worth reading is the width of a single route.
`list_observability_coverage_correlations` carries twelve query keys -- a
cursor, a limit, and ten filters spanning scope, provider, coverage signal and
status, observability object, source and resource class, outcome, and both
target anchors -- which is more than any other route the repository router
selects. The handler reads each key by name and has no catch-all, and a key
dropped in the move fails two different ways. `limit` is required and a scope
anchor is required, so losing either returns 400. Losing a plain filter returns
200 and widens the caller's page to rows they filtered out, which reads as a gap
the graph does not have. That is why the child tests and the dispatch-level test
assert all twelve keys individually as well as by exact request: a loud failure
and a silent one need the same per-key coverage.

The container-image identity route family is the sixth Wave 2 MCP
extraction, and the only one whose request builders did not come out of a file
named for the family. `containerImageIdentitiesRoute` and
`containerImageTagHistoryRoute` sat in `dispatch_supply_chain.go` beside six
supply-chain builders that stay there, while the count and inventory builders
sat alone in `dispatch_container_image_aggregates.go`. All four now live under
`internal/mcp/containerimage`; the aggregates file is deleted and
`dispatch_container_image.go` takes its place holding only the thin
`containerImageRoute` adapter. Root keeps the four tool definitions and their
assembly positions, global fanout order, dispatch, authorization, transport,
timeouts, response budgets, envelopes, summaries, and telemetry. The adapter is
consulted directly after the observability-coverage one at the top of
`repositoryRoute`, so the repository router keeps its own position in the
global chain and no other family's resolution order changes. The four tool
names are disjoint from the five earlier families and from the remaining
switch arms, which drop from 30 to 26, and the 162-tool order, the advertised
schemas, the `limit` defaults of 50 and 100, and every selected method, path,
and query key remain unchanged.

The root file set is the one thing this family changes that the previous five
did not. Deleting `dispatch_container_image_aggregates.go` and adding
`dispatch_container_image.go` leaves `internal/mcp` at the same 106 non-test Go
files it had before, so the dirgate count still matches while the name set does
not. That is exactly the same-count swap the grandfather digest exists to
catch, and it is why this commit carries a re-pin whose count column is
unchanged and whose digest column moves.

What makes this family worth reading is that its four routes look
interchangeable and are not. Tag history is served from
`/api/v0/images/tag-history`, not the
`/api/v0/supply-chain/container-images/identities` prefix its three siblings
share, because `TagHistoryHandler.Mount` registers it there; folding it onto
the sibling prefix reads like tidying and selects a path the query mux does not
serve. The count route carries no `limit` and no `offset`, because its handler
reads neither: a page size sent there would be inert rather than enforced.
`limit` defaults to 50
on the listing and tag history but 100 on the inventory. And the four routes
fail differently when a key goes missing: the listing 400s without `limit` and
without one of its five scope anchors, tag history 400s without both
`repository_id` and `tag` since the handler composes them into the `image_ref`
it anchors on, and the two aggregates require nothing at all, so a lost filter
there returns 200 over a wider scope and quietly drops that key from the
`scope` block the response echoes back. The `group_by` fallback to `outcome` is
a fourth shape again: `containerImageIdentityInventory` applies the same
default itself, so removing the fallback changes no answer, while changing it
to another dimension changes every ungrouped caller's answer. The child tests
and the dispatch-level test therefore assert each route's keys individually
against literal expectations, not against the child selector, since the
adapter parity test builds both of its sides from that same selector.

The supply-chain-impact route family is the seventh Wave 2 MCP extraction.
Its four tools -- the bounded vulnerability finding listing plus its
whole-scope count, grouped inventory, and single bounded explanation -- were
answered by four arms of the same `repositoryRoute` switch. Two request
builders, `supplyChainImpactFindingsRoute` and
`supplyChainImpactExplanationRoute`, sat in `dispatch_supply_chain.go` beside
four supply-chain builders that stay there; the other two, plus the
eighteen-filter helper they share, sat alone in
`dispatch_supply_chain_aggregates.go`. Family membership and all four
builders now live under `internal/mcp/supplychainimpact`; the aggregates file
is deleted and `dispatch_supply_chain_impact.go` takes its place holding only
the thin `supplyChainImpactRoute` adapter. Root keeps the four tool
definitions and their assembly positions, global fanout order, dispatch,
authorization, transport, timeouts, response budgets, envelopes, summaries,
and telemetry. The adapter is consulted directly after the container-image
one at the top of `repositoryRoute`, so the repository router keeps its own
position in the global chain and no other family's resolution order changes.
The four tool names are disjoint from the six earlier families and from the
remaining switch arms, and the 162-tool order, the advertised schemas, the
`limit` defaults of 50 and 100, the `offset` default of 0, the `group_by`
fallback to `impact_status`, and every selected method, path, and query key
remain unchanged.

As with the container-image extraction, deleting
`dispatch_supply_chain_aggregates.go` and adding
`dispatch_supply_chain_impact.go` is a same-count file swap: `internal/mcp`
holds the same non-test Go file count it had before, so the dirgate re-pin
here is digest-only.

What makes this family worth reading is the `include_suppressed` filter. It
is not a plain string like its neighbors: the handler treats a missing key as
its documented `false` default and only rejects a non-true/false value, so the
route must forward `"true"` or `"false"` when the caller set an explicit bool
and omit the key entirely otherwise. `routecontract.Arguments.BoolOr` cannot
express that three-state contract -- it collapses "the caller never set this"
into the fallback, which is indistinguishable from an explicit `false` -- so
the child package carries its own `boolStr` helper rather than reusing
`BoolOr`, the same shape the root dispatcher used before extraction. The
listing and the two aggregates share this helper and the same eighteen
filters; the explanation carries none of them, since it answers exactly one
finding, one no-evidence explanation, or one ambiguous-scope refusal rather
than a page.

The security-alert reconciliation route family is the eighth Wave 2 MCP
extraction. Its three tools -- the cursor-paged listing of reducer-owned
provider security-alert reconciliations plus its whole-scope count and
grouped inventory -- were answered by three arms of the same
`repositoryRoute` switch. The listing builder,
`securityAlertReconciliationsRoute`, sat in `dispatch_supply_chain.go` beside
three supply-chain builders that stay there; the two aggregate builders sat
alone in `dispatch_security_alert_aggregates.go`. Family membership and all
three builders now live under `internal/mcp/securityalert`; the aggregates
file is deleted and `dispatch_security_alert.go` takes its place holding
only the thin `securityAlertRoute` adapter. Root keeps the three tool
definitions and their assembly positions, global fanout order, dispatch,
authorization, transport, timeouts, response budgets, envelopes, summaries,
and telemetry. The adapter is consulted directly after the
supply-chain-impact one at the top of `repositoryRoute`, so the repository
router keeps its own position in the global chain and no other family's
resolution order changes. The three tool names are disjoint from the seven
earlier families and from the remaining switch arms, which drop from 22 to
19, and the 162-tool order, the advertised schemas, the `limit` defaults of
50 and 100, the `offset` default of 0, the `group_by` fallback to
`reconciliation_status`, and every selected method, path, and query key
remain unchanged.

As with the container-image and supply-chain-impact extractions, deleting
`dispatch_security_alert_aggregates.go` and adding
`dispatch_security_alert.go` is a same-count file swap: `internal/mcp` holds
the same non-test Go file count it had before, so the dirgate re-pin here is
digest-only.

What makes this family worth reading is that the listing is scope-anchored
while its two aggregate siblings are not. `SecurityAlertReconciliationFilter
.hasScope` requires one of `repository_id`, `provider`, `package_id`,
`cve_id`, or `ghsa_id`; `provider_state` and `reconciliation_status` do not
count as anchors on their own, so a caller who sets only those two still
400s on the listing. The count and the inventory require nothing at all, so
a lost filter there returns 200 over a wider scope and quietly drops that
key from the `scope` block the response echoes back. This family carries no
`boolStr`-shaped tri-state filter -- every key is a plain string or a paging
integer -- so, unlike supply-chain-impact, the child needs no local
reimplementation beside `routecontract.Arguments`.

The admission-decisions route family is the ninth Wave 2 MCP extraction and
returns to the single-tool shape: one tool, `list_admission_decisions`, one
arm of the same `repositoryRoute` switch, one request builder alone in
`dispatch_admission_decisions.go` with no private helper beside it. Family
membership and the builder now live under `internal/mcp/admissiondecisions`,
and `dispatch_admission_decisions.go` keeps only the thin
`admissionDecisionsRoute` adapter. Root keeps the tool definition and its
assembly position, global fanout order, dispatch, authorization, transport,
timeouts, response budgets, envelopes, summaries, and telemetry. The adapter
is consulted directly after the security-alert one at the top of
`repositoryRoute`, so the repository router keeps its own position in the
global chain and no other family's resolution order changes. The one name is
disjoint from the eight earlier families and from the remaining switch arms,
which drop from 19 to 18, and the 162-tool order, the advertised schema, the
`limit` default of 50, and every selected method, path, and query key remain
unchanged. The root file set is unchanged too -- the adapter keeps the
builder's file name -- so `internal/mcp` holds its dirgate pin of 106 with no
re-pin.

What makes this family worth reading is that its eight keys fail three
different ways when one is lost, and the handler's own defaults hide two of
them. `domain`, `scope_id`, and `generation_id` are required, so losing any
of them 400s. `anchor_kind` and `anchor_id` must arrive together, so losing
one half 400s while losing both returns 200 over every anchor in scope.
`state`, `include_evidence`, and `limit` each have a handler default, so
losing one returns 200 with a wider state set, no evidence rows, or a 50-row
page -- and because the dispatcher's `limit` default is also 50, the handler
cannot tell an omitted `limit` from a requested one. `include_evidence` is the
key to watch: it is always sent as an explicit `"true"` or `"false"` built
from a Go bool only, so `routecontract.Arguments.BoolOr` is the right seam and
no `boolStr`-shaped tri-state helper is needed, but a client that stringifies
the flag gets a 200 with no evidence rows rather than an error. The child
tests pin each of the six string keys, the `limit` coercion table, and the
bool-only `include_evidence` coercion individually, and the dispatch-level
test asserts the eight keys against literal expectations rather than against
the child selector, which the adapter parity test cannot do for it.

The Kubernetes-correlation route family is the tenth Wave 2 MCP extraction and
keeps the single-tool shape: one tool, `list_kubernetes_correlations`, one arm
of the same `repositoryRoute` switch, one request builder alone in
`dispatch_kubernetes.go` with no private helper beside it. Family membership
and the builder now live under `internal/mcp/kubernetes`, and
`dispatch_kubernetes.go` keeps only the thin `kubernetesCorrelationsRoute`
adapter. Root keeps the tool definition and its assembly position, global
fanout order, dispatch, authorization, transport, timeouts, response budgets,
envelopes, summaries, and telemetry. The adapter is consulted directly after
the admission-decisions one at the top of `repositoryRoute`, so the repository
router keeps its own position in the global chain and no other family's
resolution order changes. The one name is disjoint from the nine earlier
families and from the remaining switch arms, which drop from 18 to 17, and the
162-tool order, the advertised schema, the `limit` default of 50, and every
selected method, path, and query key remain unchanged. The root file set is
unchanged too -- the adapter keeps the builder's file name -- so
`internal/mcp` holds its dirgate pin of 106 with no re-pin.

What makes this family worth reading is that its ten keys fail four different
ways when one is lost, and the loudest failure is the one the dispatcher's
default hides. `limit` is required by the handler and bounded to 1..200 as a
rejection, not a clamp: an absent `limit` 400s with `limit is required`, and
0, -1, or 500 400s with `limit must be between 1 and 200`. The dispatcher's
default of 50 is therefore the only reason an MCP caller who omits `limit`
gets a page at all, and a caller who stringifies it as `"25"` gets a 50-row
page rather than an error because `routecontract.Arguments.IntOr` does not
parse strings. The six anchors -- `scope_id`, `cluster_id`,
`workload_object_id`, `namespace`, `image_ref`, and `source_digest` -- are
required as a group, so losing one 400s only the caller whose sole anchor it
was and silently widens everyone else's page past the anchor they named.
`outcome` and `drift_kind` are `($n = '' OR ...)` equality filters in the
store, so losing one returns 200 over every outcome or drift kind, and
`after_correlation_id` is the `fact_id` keyset cursor, so losing it returns
200 from the first page again. The child tests pin each of the nine string
keys and the `limit` coercion table individually, and the dispatch-level test
asserts the ten keys against literal expectations rather than against the
child selector; dropping `drift_kind` from the builder fails three child
tests and one root test.

The infrastructure-search route family is the eleventh Wave 2 MCP extraction
and lifts an arm out of `resolveRoute`'s own switch rather than out of a split
router. One tool, `find_infra_resources`, one arm under the switch's Infra
group, one request builder alone in `dispatch_infra_search.go` with no private
helper beside it. Family membership and the builder now live under
`internal/mcp/infrasearch`, and `dispatch_infra_search.go` keeps only the thin
`infraResourceSearchRoute` adapter. Root keeps global fanout order, dispatch,
authorization, transport, timeouts, response budgets, envelopes, summaries, and
telemetry; the `ecosystem` child keeps the advertised definition and its
assembly position. Because the arm sat in the switch that runs after every
delegation, the adapter is consulted as the twentieth and last delegation,
directly ahead of the switch, so every earlier family keeps its position and
the one name is claimed at the same point in the chain it was claimed before.
`resolveRoute` goes from 19 delegations and 50 cases to 20 and 49 -- 69
ordered arms on both sides -- and the 162-tool order, the advertised schema,
the `limit` default of 50, and the selected method, path, and body keys remain
unchanged. The root file set is unchanged too, so `internal/mcp` holds its
dirgate pin of 106 with the same digest and no re-pin.

What makes this family worth reading is that its `limit` bound is the opposite
shape from the Kubernetes one above, and calling it a 1..200 bound would
mislead in the same way the admission-decisions wording once did. The handler
substitutes 50 for any `limit` at or below zero and clamps anything above 200
down to 200; nothing is rejected, so 0 and -1 mean a 50-row page, 500 means
200 rows, and the dispatcher's default of 50 is indistinguishable at the
handler from an omitted field. That also changes what a dropped key costs. The
seven scope keys -- `query`, `category`, `kind`, `provider`, `environment`,
`resource_service`, and `resource_category` -- are required as a group, so
losing one 400s only the caller whose sole scope it was with `query or
structured filter is required` and silently widens everyone else's page.
Losing `limit` fails nothing at all: every caller gets 50 rows, so a caller
who asked for 5 or for 200 sees a different page with no error. The body is
JSON, so `limit` travels as a Go `int`, and a stringified `"25"` collapses to
the 50-row default through `routecontract.Arguments.IntOr` rather than
reaching the handler as a string its `int` field would reject with 400. The
child tests pin each of the seven string keys and the `limit` coercion table
individually, and the dispatch-level test asserts the eight keys against
literal expectations rather than against the child selector; dropping
`resource_category` from the builder fails four child tests and one root
test.

**cmd/eshu (233):** `package main` — subdirectories are impossible by
language rule. The lever is extracting business logic to new
`internal/cli/<family>` packages, leaving thin cobra RunE wrappers —
which the package's own AGENTS.md already demands. ~20 families measured;
all clean except the local_host/local_graph supervisor cluster (31 files,
real bidirectional cycle — extract as ONE `localsupervisor` unit or leave
last).

**parser (259 root):** the split already happened — 34 language
subpackages hold 734 files. Root keeps the Engine/Registry/Runtime
dispatcher (by design: languages must not import the parent). The 27
`<lang>_language.go` glue files are Engine methods and CANNOT move without
wiring the already-defined-but-unused LanguageProvider seam — a refactor
to schedule separately, not a file move. What CAN happen now: normalize
straggler filenames and convert per-language root tests into external
`_test` packages inside the existing language dirs, with the two parser
ledgers updated in lockstep. Lowest priority of the seven.

**query (1,903):** the architecture already fits — ~60 handler types, each
with its own Mount(). Phase 0 decision needed first: 284 external files
(86 in mcp dispatch) reference `query.<Type>`; the research recommends
root type aliases (`type SupplyChainHandler = supplychain.Handler`) so
external code compiles unchanged, burned down later. That alias alone does
not compile — see the acyclic-boundary prerequisite below, which has to land
first. Then clean families first: supplychain(~183), code(~172),
contentread(42), packagereg(32).
Tangled families (impact ← repository/service/deployment_trace call its
unexported helpers) need the helper seam exported before their move. Root
keeps: APIRouter/Mount, compatibility aliases and `Write*` wrappers,
capability rows in the existing `contract_*` files, openapi.go assembly + the 101
`openapi_paths_*` constants, and the two cross-cutting test sweeps
(auth_scoped_routes 41 files, graph_read_error 17).

**reducer (1,269):** hub-and-spoke with a small hub: registry.go, the
defaults_additive_domains wiring (11 files), domain.go/intent.go,
shared_projection harness (26), batch-insert helpers. ~30 largest families
symbol-measured: most are clean (containerimage 81, cicdrun 28, secalert,
iamcan, searchdoc, secretsiam, sbomattest, awscloudruntime, tfconfigstate,
the six code-intelligence domains, ~15 per-cloud-resource domains). Three
proven traps: supply_chain_impact ↔ supply_chain_suppression are
bidirectionally type-coupled (one subpackage, or hoist shared types);
code_call_materialization needs code_call_language's unexported resolver
registry exported first; `service_materialization_{docs,vulnerabilities,
incidents}.go` are misnamed ServiceCatalogCorrelationHandler methods —
they move with svccatalog, proving every cluster gets measured before
moved. ~400 external files import reducer; storage/postgres names
family-specific types — whole-module grep before each family move.

### Prerequisite for four packages: an acyclic boundary

A reviewer caught this in query and reducer, and applying the same check to the
rest of Part 3 found it in projector and mcp too. It is the one thing in this
plan that does not compile as written.

The shape is always the same. Root calls into a symbol that is scheduled to move
out, and the moved symbol needs a type that stays in root. Either edge alone is
fine. Both at once is an import cycle, and Go refuses to build it.

| Package | Root reaches into the family | Family needs from root |
|---|---|---|
| query | the alias `type SupplyChainHandler = supplychain.Handler` plus router wiring | `querycontract.GraphQuery`, `ContentStore`, profiles, envelopes, HTTP helpers, and family-local capability registration |
| reducer | 48 `.Handler = <Family>Handler{}` construction sites across 10 of the 11 `defaults_additive_domains*.go` wiring files — e.g. `defaults_additive_domains_correlation.go:66-67`, which calls `containerImageIdentityDomainDefinition()` and builds `ContainerImageIdentityHandler{}` | `Intent`, `Result` and the `Handler` interface (`container_image_identity.go:52,73`) |
| projector | `scope_generation_intents.go` has 44 reducer-intent builder call sites, defined across 41 family files | `ReducerIntent` (`runtime.go:50`) |
| mcp | `types.go` has 42 `append(tools, <domain>Tools()...)` call sites | `ToolDefinition` (`types.go:7`) |

Collector and coordinator are genuinely clear. Collector families are
constructed from external `cmd/` binaries. Coordinator scheduler families use
the dependency-neutral `plannercontract` helper while root retains their
Planner interfaces and service methods, so an extracted scheduler does not
need to import root.
`cmd/eshu` has a different constraint rather than a cycle — it is `package main`
and nothing can import it, so logic extracted to `internal/cli/<family>` cannot
call back into the shared CLI helpers at all. Either those helpers move too, or
the extracted logic must not need them.

Two ways out, and they are not interchangeable:

- **Hoist the shared contracts** — ports, envelopes, `DomainDefinition`,
  `Domain`, `Intent`, `Handler`, `ReducerIntent`, `ToolDefinition` — into a
  dependency-neutral package below both root and the families. This works for
  all four.
- **Invert registration** so root never names a family symbol: the family
  registers itself, and a wiring package above both owns the import edge. This
  works for reducer, projector and mcp, where root's only edge is a call. It
  does NOT work for query: the alias is itself root naming a family symbol, and
  it has to stay in root or the 284 external `query.<Type>` references stop
  compiling, which is the whole reason the alias exists.

Until this lands, read `clean` in the family tables as what it measures: zero
coupling to other families. It does not mean ready to move.

This also breaks Part 4's order below, which moves projector and mcp well before
query and reducer. Either the boundary lands first for every affected package, or
those moves wait.

## Part 4: execution model

- **Order:** gate PR → hardening PR → collector → projector+coordinator →
  mcp → cmd/eshu → query → reducer. Parser last, optional. Small packages
  move while normal lanes run (per-package claim, same as issue claims);
  **query and reducer each get an all-lanes-quiet window** — their moves
  conflict with everything.
- **One owner** for the whole program. Two agents inventing taxonomies
  produce two taxonomies.
- **Per move PR:** `git mv` (history follows); one family per PR; the
  three doc files (doc.go, README.md, AGENTS.md) for every new directory
  (the package-docs gate enforces this); gate/spec path updates in the
  SAME PR; behavior-preserving proof = whole-module build + full package
  tests + golden-corpus gate byte-identical + `go vet` + route/openapi
  parity where applicable. No logic changes ride along with moves, ever.
- **Timing:** after current lanes drain, before Epic M (multi-tenancy),
  feeding directly into #4047/#4398. Epic M then lands on the new layout.

## schemadecode hoist: codegen measurement

No-Regression Evidence: measured, not asserted. `go build -gcflags=-m
./internal/reducer/...` at base `1f0e1e172` and at head `f13adc68b` — the
commit that squash-merged the schemadecode hoist (#6372) and its parent, so
this brackets the merged HOIST itself (`f13adc68b^` is `1f0e1e172`, verify
with `git rev-parse --short f13adc68b^`), not any later branch's diff. Both
are now permanent commits on main's history, so this bracket cannot go stale
the way a moving branch head or a pre-rebase hash can — the previous two
versions of this citation were each wrong in an opposite direction (a
pre-rebase hash that stopped existing, then a moving-branch-head replacement
that would have bracketed no change at all), both from treating the SHA as
bookkeeping rather than as part of the claim. Reported as a set difference and
a per-name call-site difference rather than a net total, because a net count
cannot find a named regression.

**Functions that lost inlinability: zero.** The `can inline` set is 1356 at base
and 1357 at head — UNIQUE function names in the recursive build
(`./internal/reducer/...`), not occurrences: the same build's occurrence count
(a function reported "can inline" more than once across the log) is 1395 at
base and 1396 at head, a constant +39 offset from the unique-name figures at
both SHAs (a counting-method difference, not a code difference). Naming both
here is deliberate — re-deriving this with the other method and finding 1395
instead of 1356 looks like a regression and is not one. The set difference is
one symbol in one direction and it is the rename itself — `FactschemaEnvelope`
gained, nothing lost. No caller's body grew past the inlining budget because of
an added indirection.

Inlined call sites (occurrence count of `inlining call to` in the same
`go build -gcflags=-m ./internal/reducer/...` log) move 14646 to 14568.
**These eleven are every function whose inlined-call-site count changed** — a
full per-name diff of the two logs, not a hand-picked table; that claim is
checkable (re-run the diff) rather than the unfalsifiable "nothing is
unexplained" this section asserted before:

| function | base | head | delta | why |
|---|---|---|---|---|
| `factschemaEnvelope` | 98 | 1 | −97 | renamed |
| `FactschemaEnvelope` | 0 | 97 | +97 | renamed — this and the two rows below are one trio (98 -> 1+97+2=100, net +2, not the "+1" an earlier version of this doc claimed) |
| `schemadecode.FactschemaEnvelope` | 0 | 2 | +2 | renamed |
| `newFactDecodeError` | 101 | 7 | −94 | the 20 moved files now call `factdecode.NewFactDecodeError` directly instead of through the root forwarder |
| `reflect.flag.kind` | 8 | 12 | +4 | not touched by this move's own diff |
| `reflect.flag.mustBe` | 4 | 6 | +2 | not touched by this move's own diff |
| `reflect.flag.mustBeAssignable` | 4 | 6 | +2 | not touched by this move's own diff |
| `reflect.flag.mustBeExported` | 4 | 6 | +2 | not touched by this move's own diff |
| `reflect.flag.ro` | 4 | 6 | +2 | not touched by this move's own diff |
| `factenvelope.FactSchemaFromInternal` | 99 | 100 | +1 | not touched by this move's own diff |
| `factenvelope.sourceRefString` | 99 | 100 | +1 | not touched by this move's own diff |

−191 lost, +113 gained, −78 net, and this total is COMPLETE, not partial: the
four relocations above (the rename trio plus `newFactDecodeError`) account for
all −191 lost and +99 of the +113 gained; the remaining +14 falls across the
seven `reflect.flag.*` / `factenvelope.*` functions in the lower half of the
table. An earlier version of this section named only the four relocations,
said "the arithmetic closes, so nothing is unexplained," and was wrong in a
narrower way than that sounds: the arithmetic WAS already complete and
correct — summing all eleven gives exactly −191/+113/−78, the same total the
four-relocation version claimed. What was false is that the prose described a
smaller set of movers than the arithmetic had already accounted for. Two
independent reviewers and the PR author each verified the totals balanced and
stopped there; the defect was only visible by asking a different question —
not "do these numbers add up" but "is this list of names the whole list."

None of the seven `reflect.flag.*` / `factenvelope.*` functions are touched by
this move's own diff; all seven are called pervasively by the decode path
every seam already goes through (envelope conversion and the JSON-tag
reflection every typed decode uses). The increase is consistent with a
build-ordering/inlining-budget effect of splitting 20 files into a new
compilation unit — not established as the cause, since that has not been
measured directly (e.g. a per-callsite `-m=2` breakdown), only observed as
plausible given what these functions are and where they sit. Do not treat
"consistent with" as "is."

`newFactDecodeError` looks alarming and is not a regression. At base the root
forwarder inlined at 101 sites and then made a real call into `factdecode`; at
head the moved files make that same real call one wrapper shallower.
`factdecode.NewFactDecodeError` is absent from the inlining log at both SHAs — it
was never inlined across the package boundary either way — so the count of real
calls is unchanged and only the wrapper expansions are gone. Less code, same
work.

The one call site that a `var` binding would have cost an inline —
`supply_chain_suppression_decode.go` reaching `factschemaEnvelope` — is why that
single forwarder is a `func` rather than a `var`. The other 99 forwarders bind
decoders far too large to inline in any form, so the binding shape costs them
nothing.

No-Observability-Change: this hoist relocates decode seams and adds
forwarders; it emits no metric, span, or log of its own and changes no signal any
caller emits. Every decode failure still flows through
`factdecode.NewFactDecodeError` to the same quarantine path, so
`eshu_dp_reducer_input_invalid_facts_total` and the reducer pass counters
(`eshu_dp_reducer_executions_total`, `eshu_dp_reducer_run_duration_seconds`)
carry exactly the coverage they did before. The telemetry-coverage rows for every
moved path are updated in this PR.

**Why this is safe.** The move is behaviour-preserving by construction: the seam
bodies are unchanged, and the payload-usage manifest — the derived contract
artifact that depends on these symbol names — is verified to report the same 125
kinds with the same `UsedFields` after the forwarder-resolution fix, which is what
the `scripts/verify-payload-usage-manifest.sh` run in this PR's evidence table
proves. Input shape is one fact envelope per decode call, unchanged; there is no
queue, lease, batch, or row-count dimension to this change.

## gpphase hoist: crossrepo family unblock

The crossrepo family (`cross_repo_resolution.go`, `cross_repo_resolution_retract.go`,
`cross_repo_intent_row.go`, `cross_repo_evidence_type.go`) was graded `clean` in
Part 1's family table but could not move: it references three symbols defined at
the reducer root — `GraphProjectionPhaseKey`, `GraphProjectionReadinessLookup`,
`GraphProjectionReadinessPrefetch`. Measured consumer counts before the move
(`rg -l '\bSYM\b' internal/reducer/*.go | rg -v _test | wc -l`): `GraphProjectionPhaseKey`
12 non-test files (19 test files), `GraphProjectionReadinessLookup` 28 non-test
files (10 test files), `GraphProjectionReadinessPrefetch` 7 non-test files (0 test
files). All three were genuinely DEFINED at the root (`rg '^(type|func|const|var)
SYM\b' internal/reducer/*.go` each matched exactly `graph_projection_phase.go`,
not a forwarder or alias), so none were free.

The brief that opened this hoist additionally named
`GraphProjectionPhaseCanonicalNodesCommitted`, `graphProjectionPhaseStateForIntent`,
`publishIntentGraphPhase`, `ProjectedSourceLedger`, and `PriorGenerationCheck` as
possibly living in `graph_projection_phase.go`. Measured: none of the five do —
they are defined in `generation_check.go`, `graph_projection_phase_publish.go`, and
`projected_source_ledger.go` respectively, and none are referenced by any
crossrepo-family file. This is the same pattern the epic has hit before: the
issue's own framing of a family's blockers is not reliable and must be measured,
not assumed.

The minimal cohesive unit that unblocks crossrepo turned out to be larger than
the three named symbols, because `GraphProjectionPhaseKey.Keyspace` is typed
`GraphProjectionKeyspace` and crossrepo constructs a key with the
`GraphProjectionKeyspaceCrossRepoEvidence` constant, and gates readiness on the
`GraphProjectionPhaseBackwardEvidenceCommitted` constant — both members of the two
enums (`GraphProjectionKeyspace`, `GraphProjectionPhase`) declared earlier in the
same file. Splitting an enum across a package boundary (some constants hoisted,
others left at the root) was rejected as incoherent, so the full identity
vocabulary moved together: `GraphProjectionKeyspace` (13 constants),
`GraphProjectionPhase` (7 constants), `GraphProjectionPhaseKey` plus its
`Validate` method, `GraphProjectionReadinessLookup`, and
`GraphProjectionReadinessPrefetch` — into a new `internal/reducer/gpphase` leaf.

Deliberately left at the root: `GraphProjectionPhaseState` (6 non-test-file
consumers) and the `GraphProjectionPhasePublisher` interface (24 non-test-file
consumers) — the phase-publish/repair machinery's persistence shape, which adds
no identity concept beyond `PhaseKey`/`Phase` and which crossrepo does not
reference. `EndpointPresenceRow`/`Writer`/`Lookup` (4/15/8 non-test-file
consumers) also stay — a distinct uid-exact, cross-scope presence primitive
(issue #1380), unrelated to the same-scope/same-generation readiness gate
crossrepo needs, and confirmed by grep to have zero references from any
crossrepo-family file.

The root keeps every original exported name — `GraphProjectionKeyspace`,
`GraphProjectionPhase`, `GraphProjectionPhaseKey`, `GraphProjectionReadinessLookup`,
`GraphProjectionReadinessPrefetch`, and one alias per constant — as Go type/const
aliases in `graph_projection_phase.go`, so no caller changed and
`GraphProjectionPhaseKey.Validate` needs no forwarder (an alias carries the
method set). This hoist does not itself move the crossrepo family's files into a
`crossrepo` subpackage; it removes a blocker so that move can happen separately.

**Correction after review.** An earlier version of this section, and matching
prose in `gpphase/doc.go`, `gpphase/README.md`, and one
`telemetry-coverage.md` row, asserted these three symbols were crossrepo's
*only* remaining blocker. That overstated what had actually been checked. A
real trial move settles it: copy all five crossrepo-prefixed files —
`cross_repo_resolution.go`, `cross_repo_resolution_retract.go`,
`cross_repo_intent_row.go`, `cross_repo_evidence_type.go`, and
`cross_repo_evidence_artifacts.go` (this fifth file carries the family prefix
but was missed by the four-file set the paragraphs above describe) — into a
throwaway `internal/reducer/crossrepotrial` package and build it
(`go build -gcflags="-e" ./internal/reducer/crossrepotrial/...`, `-e` to see
every error rather than the compiler's default 10-error cutoff). The first
build reports ten undefined names. Five are exactly `GraphProjectionKeyspace*`
constants, `GraphProjectionPhaseKey`, `GraphProjectionReadinessLookup`, and
`GraphProjectionReadinessPrefetch` — this hoist's own symbols, and qualifying
them as `gpphase.*` resolves every one. The other five —
`SharedProjectionIntentRow`, `SharedProjectionIntentInput`,
`BuildSharedProjectionIntent`, `DomainRepoDependency`, `toStringSlice` — are
not gpphase's concern, and each resolves too, by a different route confirmed
individually: `SharedProjectionIntentRow`/`Input` are root type aliases to
`sharedintent.Row`/`Input`, so qualifying by the leaf name is enough;
`BuildSharedProjectionIntent` is a real function at root (not an alias) that
forwards to `sharedintent.Build`, so the call site must name the leaf function
directly; `DomainRepoDependency` is a root const alias to
`contract.DomainRepoDependency`; and `toStringSlice` — which a first pass
believed had no leaf home — is itself a one-line forwarder to
`payloadcore.ToStringSlice` (`internal/reducer/workload_deployment_sources.go:381`),
already an exported leaf function, so it resolves the same way as the other
four. With all ten qualified, the five-file trial package builds clean:
`go build -gcflags="-e" ./internal/reducer/crossrepotrial/...` exits 0, zero
undefined names remain. So the corrected claim is: this hoist supplies the
crossrepo family's only symbols that had **no existing leaf home at all**;
every other name the family needs was already reachable through an
already-hoisted sibling leaf (`sharedintent`, `contract`, `payloadcore`) and
needed only an import or call-site rewrite, not a new hoist. The trial
package was deleted after the build; it is not part of this PR's diff.

No-Regression Evidence: this section was re-derived after a rebase onto
current `origin/main` invalidated the prior baseline/after SHAs and inline
count (rebasing changes what a base-relative figure means even when the
diff's own content is untouched). Baseline `091ec3400` (the `origin/main`
tip immediately before this hoist merged), after `594dc0a3e` (the
squash-merge commit that landed this hoist's code as PR #6411), go1.27.0
darwin/arm64. This crosses a package boundary, so
inlining is measured, not assumed, as a SET in both directions with `comm`,
at whole-module scope (`go build -gcflags=-m ./...`) since the moved types
are referenced from `internal/workflow`, `internal/reducer/dsl`, and
`internal/reducer/tfstate` in addition to the reducer root: unique
`can inline` names 12153 -> 12153 (probe confirmed non-vacuous: both sets
exceed 12000 names before the equal total is believed). **Fifteen** names
moved in each direction (re-derived with `wc -l` after an earlier pass in
this same section under-counted by eye at fourteen) and every one is the
same generic instantiation re-qualified to the new package path (e.g.
`slices.Clone[...reducer.GraphProjectionKeyspace,...]` ->
`slices.Clone[...reducer/gpphase.Keyspace,...]`) — a 1:1 rename set, not a net
count, verified pair by pair by listing all thirty names and matching each
LOST entry to its GAINED counterpart by shape. Zero functions actually lost or
gained inlinability. Correctness: `go build ./...` and `go vet ./...` both exit 0 with
no output; `go test ./internal/reducer/...` passes 13 packages including the new
`gpphase` package, which carries `TestPhaseKeyValidate` and
`TestPhaseKeyValidateRejectsBlankFields` (moved out of
`graph_projection_phase_test.go`, unchanged assertions, package-qualified names).
The reducer root non-test `.go` file count is unchanged (`graph_projection_phase.go`
was edited in place, not deleted) — the dirgate whole-tree gate
(`scripts/dev/precommit-go.sh dirgate-all`) passed with no output and needed no
grandfather-TSV edit. No query, Cypher, batch size, worker count, lease, or queue
behaviour is touched by this change; the moved code is enums, one struct, one
validation method, and two function types.

No-Observability-Change: `GraphProjectionKeyspace`, `GraphProjectionPhase`,
`GraphProjectionPhaseKey`, `GraphProjectionReadinessLookup`, and
`GraphProjectionReadinessPrefetch` are enums, a struct, and function-type
declarations plus one pure validation method (`PhaseKey.Validate`) that computes
no state and performs no I/O; behavior-preserving hoist out of the reducer root
(#6061). The phase-publish and phase-repair machinery that actually writes and
reads readiness (`graph_projection_phase_publish.go`,
`graph_projection_phase_repair.go`, `graph_projection_phase_repair_runner.go`)
stays at the root untouched, so its existing coverage still applies:
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`
bound the handler passes that read and publish these phases, and
`eshu_dp_postgres_query_duration_seconds` times the Postgres reads/writes the
publisher and repair queue issue. This file emits no metric, span, or log of its
own.
No-Regression Evidence: the reducer tfconfigstate move (#6061) relocates
eleven files -- `terraform_config_state_drift*.go`, four non-test and seven
test -- from the reducer root into `go/internal/reducer/tfconfigstate/` with
`git mv` and no logic change beyond qualifying the leaf-owned symbols the
family already referenced (`Intent`/`Result`/`Domain*`/`ResultStatusSucceeded`/
`IntentStatusClaimed`/`DomainWorkloadIdentity` from `reducer/contract`;
`workloadIdentityExecer`/`reducerWriterNow`/`reducerFactVersionedRow`/
`reducerBatchInsertVersionedFacts`/`reducerFactCollectorKind` from
`reducer/factwrite`) with their real package names. This family's only true
blocker was `nonNilMapSlice`, hoisted to `payloadcore.NonNilMapSlice` in a
prerequisite commit the same way `nonNilStrings` already forwarded to
`payloadcore.NonNilStrings`; a trial move (temp package, `go build -gcflags=-e`)
reproduced this exact single-blocker finding before either commit landed.
Baseline `c6a59f7aa` (the immediate pre-move parent), after `830634527` (the
tfconfigstate move, on top of the `9e84c0c6e` nonNilMapSlice hoist), go1.27.0
darwin/arm64, each built in its own throwaway worktree with an isolated
`GOCACHE`. This crosses a package boundary, so inlining can genuinely shift
and is measured rather than assumed: `go build -gcflags=-m ./...` whole-module
reports unique `can inline` names 12153 -> 12154, compared as a SET with `comm`
in both directions rather than by totals, since a matching total is also what a
swap looks like. Zero names lost. One gained -- `NonNilMapSlice`, the new
`payloadcore` export, the same effect the `nonNilStrings` hoist had on
`reducerWriterNow` in the writerprims move above. Correctness: `go build ./...`
and `go vet ./...` both exit 0 across the whole module with no output,
`go test ./internal/reducer/...` passes 13 packages (including the new
`tfconfigstate` leaf and its `payloadcore` sibling), and
`go test ./internal/storage/postgres/... ./internal/query/...` — the two
importers outside the reducer tree that name `DomainConfigStateDrift`/
`TerraformConfigStateDriftHandler`/`PostgresTerraformConfigStateDriftWriter` at
the type level (`cmd/reducer/wiring_handlers.go`,
`internal/reducer/defaults_handlers.go`,
`internal/reducer/defaults_additive_domains_correlation.go`,
`internal/reducer/defaults_config_state_drift_writer_gate_test.go`, all
repointed to `tfconfigstate.*` in this move) — both pass. The reducer root
non-test file count drops from 515 to 511; `scripts/lib/dirgate-grandfather.tsv`
and its generated mirror are re-pinned in this PR
(`bash scripts/dev/precommit-go.sh dirgate-digest internal/reducer`, then
`bash scripts/generate-dirgate-grandfather-go.sh`). Three of the moved test
files (`terraform_config_state_drift_writer_test.go`,
`..._writer_module_resolution_test.go`, `..._writer_retire_test.go`) depended on
shared, root-only batch-insert test doubles
(`fakeWorkloadIdentityExecer`/`fakeWorkloadIdentityExecCall`/
`fakeWorkloadIdentityResult` in `workload_identity_writer_test.go`, and the
`decodeBatchedVersionedFactCall*`/`decodedBatchedVersionedFactRow` helpers in
`reducer_fact_batch_insert_test_helpers_test.go`) still used by 36 files across
17 other families that have not moved out of the root yet (verify with `rg -l
"fakeWorkloadIdentityExecer" go/internal/reducer/ --glob '*.go' | rg -v
tfconfigstate | wc -l`); `go
build ./...` does not surface this because it does not compile test files, only
`go vet ./...` and `go test -c` do. Rather than touch a file several other
concurrent #6061 moves also depend on, `tfconfigstate` keeps a package-scoped
copy of just the versioned-insert shapes it needs
(`terraform_config_state_drift_batch_test_helpers_test.go`), wired to
`factwrite.BatchInsertVersionedQuery`/`factwrite.BatchSize` instead of the
root's compat aliases. The symmetric direction also surfaced: `counterTotal`
was defined inside the moved `terraform_config_state_drift_test.go` but three
other still-in-root test files
(`aws_cloud_runtime_drift_test.go`, `multi_cloud_runtime_drift_test.go`,
`cloud_inventory_admission_test.go`) called it too, so a root-owned copy was
added back (`metrics_counter_total_test_helper_test.go`) rather than moved.
Both directions of this shared-test-fixture problem are noted here because
every remaining #6061 family sharing these root test doubles will hit the same
`go vet`-only-visible break.

No-Observability-Change: this move relocates the `config_state_drift` intent
handler and Postgres writer with no logic change; it emits no new metric,
span, or log and removes none. `eshu_dp_correlation_rule_matches_total`,
`eshu_dp_correlation_drift_detected_total`,
`eshu_dp_drift_unresolved_owner_write_failed_total`, and
`eshu_dp_drift_ambiguous_owner_write_failed_total` are emitted from the same
call sites under the same names; only the package that owns the code moved.
The `docs/public/observability/telemetry-coverage.md` rows for this domain are
updated in this PR to the new `go/internal/reducer/tfconfigstate/` paths (one
line-number citation shifted from :101 to :102 because the new package's
`contract` import adds one line).

## Part 5: what this buys the modularization program

Extraction grades from the research become the repo-split roadmap:

- `clean` families (query/supplychain, query/code, reducer/containerimage,
  collector/gitrepo leaves, projector provider intents, coordinator
  schedulers, most cli families) = future module/repo candidates with
  measured-zero internal coupling.
- `tangled` families = the dependency-inversion backlog, each with its
  named blocker (impact helper seam, Service decomposition, dispatch hub
  extraction, LanguageProvider wiring, supplychain type hoist).
- `shared-core` sets = the de-facto public API of each future module;
  what stays in root today is what a split repo would have to export
  tomorrow. #4047's "extraction readiness gate" can assert exactly this.
