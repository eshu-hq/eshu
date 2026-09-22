# Query target tree: sequencing and open questions (#6642)

**This plan is four pages.** [The tree](6642-query-target-tree.md) ·
[What the moves cost](6642-query-move-cost.md) ·
[The file mapping](6642-query-file-mapping.md) ·
[Sequencing and open questions](6642-query-move-sequence.md)

## Sequencing

One destination directory per PR, in this order. The dependency that fixes the
order is the shared spine: nothing moves before PR 1.

| # | PR | files | why here |
| ---: | --- | ---: | --- |
| 1 | **Spine repoint.** Delete the five unexported root forwarders; point every call site at the `querycontract` / `tracing` twins that already exist. | 0 moved | 192 cross-boundary symbols, 5 of which block every later PR; also takes the dependency component from 38 destinations to 23 |
| 2 | `capability/` — the root capability handler, plus `capability_matrix.go` and `registry.go` from today's `contract/` | 7 | small, and it starts draining the `contract/` name |
| 3 | `querycontract`'s seven leaf extractions, in place, without the rename | 19 | closes the #6597 split; the rename waits for the name |
| 4 | `testutil/` ← `querytestutil`, nested | 42 | test helpers, no production risk; unblocks moved tests |
| 5 | `span/` ← `tracing` | 2 | tiny; completes the Part D rename set |
| 6 | `auth/` ← `queryauth` + the 52 root auth files, nested five ways | 60 | the largest root family; `auth/route/` alone is 24 files |
| 7–30 | **one parent per PR, smallest first. Each is a hoist-then-move**, not a move — see [the cycle analysis](6642-query-move-cost.md#the-tree-is-not-reachable-by-moving-files-alone) for the symbols each owes.** 24 PRs covering 33 leaves and 145 files: `decode` (1), `workload` (1), `dependency` (2), `observability/coverage` (2), `terraform/drift` (2), `kubernetes` (3), `metrics` (3), `compare` (4), `cicd` (5), `ask` (6), `collector` (8), `documentation` (8), `evidence` (10), `status` (14), the three seam leaves `code/seam` (2), `repository/seam` (3) and `impact/seam` (5), and the parent-grouped `semantic` (3), `graph` (5), `image` (8), `investigation` (11), `supply/chain` (7), `cloud` (11), `infra` (21) | 145 | order within the block is free; each PR carries its own hoist |
| 31 | `code/` ← `codequery` + the four `code*` siblings | 102 | large but mechanical; `CodeHandler` is **not** renamed here |
| 32 | `repository/artifacts` ← `repositoryartifacts` (rename only); `repository/` itself stays at 45 pending the UNDECIDED below | 20 | queryplan pins regenerate |
| 33 | **Part B.** `content/` ← `contentread`, `content/read/` (the `ContentReader` unit, with the four merges and the `semantic_evidence.go` split), `content/relationship/` | 56 moved, 52 after merges | the issue puts it last; it is the only big-bang |
| 34 | The alias sweep: delete all 21 root `*_alias.go` and migrate 1,420 external references | −21 | each family's aliases can only die after that family has moved |
| 35 | `querycontract` → `contract/` once today's `contract/` is down to `doc.go`; root reduction to five files; re-pin the dirgate row; retire the `internal/query` ledger row | — | definition of done |

`CodeHandler` is deliberately not renamed anywhere in this plan. The issue
requires inspecting the current query-plan entries and recording affected
digests first, and #6649 tracks whether those degree measurements represent
real code. That is a separate change on top of PR 31.

## Proof bar, every move PR

- `git mv` for every relocated file so history follows the move.
- Census by symbol before the move, not by filename prefix. The 20 files in
  [Where the prefix lies](6642-query-target-tree.md#where-the-prefix-lies) are why.
- Run the cycle check before writing any code: build the file-level reference
  graph, assign files to the proposed destination, and confirm references cross
  the new boundary in **one direction only**. A two-way crossing is an import
  cycle the moment the leaf becomes a package. This check rejected five of the
  seven leaves proposed in the first draft of this plan, and it is the reason
  every family PR is a hoist-then-move.
- All Go commands from `go/`: `go build ./... && go vet ./...`, then
  `go test ./internal/query/... -count=1` **recursive** — a bare package path
  silently skips the subpackages a move touched.
- Prove tests still *run*, not just compile:
  `go test ./internal/query/<newpkg>/... -list '.*' -count=1 | rg -q <MovedTestName>`.
  `-list` exits 0 on an empty match, so the `rg -q` is the assertion.
- For any build-tagged file, grep the constraint name across
  `.github/workflows`, `Makefile` and `scripts/` before trusting a green
  default run.
- Golden corpus (B-7) and e2e snapshot (B-12) byte-identical, or stop.
- Every new directory that contains Go code carries `doc.go`, `README.md` and
  `AGENTS.md` with real content; `scripts/verify-package-docs.sh` passes. A
  namespace parent with no package of its own carries nothing, matching
  `query/graph/` and `query/package/` today.
- Dirgate row re-pinned DOWN in the same PR, `grandfather.go` regenerated —
  see [Restack rule](#restack-rule-the-dirgate-ledger-trap).
- Citation sweep. **222 path citations across 101 tracked `.md`/`.yaml`/`.yml`
  files name 87 of the 277 moving files** (`envelope_aliases.go` 12 times,
  `relationships_catalog_cypher.go` and `content_reader_repository_catalog.go`
  9 each). Reproduce with `git ls-files '*.md' '*.yaml' '*.yml'` and the pattern
  `go/internal/query/<basename>.go`, excluding these four pages; a bare
  directory walk gives a different answer because it picks up the untracked
  `docs/site/` build output. Thirty-one query `.go` paths are pinned in `specs/`, one of them —
  `relationships_catalog_cypher.go` — a non-test root file that moves. Prior
  moves have already left stale citations behind: `specs/capability-matrix.v1.yaml:312`
  still names `go/internal/query/service_story_seam.go`, which has lived at
  `go/internal/query/entity/service_story_seam.go` since an earlier move. It
  sits inside a YAML comment, which is how it evaded the citation gate.
- Only the orchestrator runs the promotion gate: `make pre-push` once, and
  `make pre-pr-full` for a package move (build tags can hide files from
  `./...`, and only the whole-module race lane exercises them).

## Restack rule (the dirgate ledger trap)

`scripts/lib/dirgate-grandfather.tsv` row `internal/query` and its generated
mirror `tools/golangci-lint-dirgate/grandfather.go` conflict on every sibling
merge, and a clean merge is the dangerous case. Every move PR: take
`origin/main`'s copy of both, re-derive count and digest for the real tree with
`scripts/dev/precommit-go.sh dirgate-digest internal/query`, regenerate the
mirror with `scripts/generate-dirgate-grandfather-go.sh`, check the sums. Never
carry a ledger resolution across a rebase without re-deriving it.

A second session (`eshu-14`) is driving the same shape of change on
`go/internal/storage/postgres` under #6693 and re-pins its own row in the same
two files. Neither lane touches the other's row, but both touch both files.

## UNDECIDED

Two items that were listed here have since been settled by experiment rather
than left to you — the `content/read` merges (an empty `go doc -all` diff proves
they keep the exported surface identical, so they sit inside the issue's stated
Scope) and the `contract/` displacement (37 of its 40 files are per-family rows
that travel with their families, as seven already-moved families demonstrate).
What remains are genuine owner calls.

1. **`tracing` is treated as the issue's `queryspan`.** No package named
   `queryspan` exists today. `tracing/`'s `doc.go` describes exactly the
   per-route span role Part D names, so the identification is near-certain —
   but it is an inference from the doc comment, not from rename history, which
   this drive did not reconstruct.

2. **The root test rule.** 403 of 728 root test files match no moving root
   file, and the largest cluster (63 `openapi_*`) belongs to a family that
   moved in Part C. The proposed rule — single-destination tests move,
   router-level tests stay and convert to `package query_test` — changes how
   about 400 files are treated and should be agreed before PR 6, not argued
   per PR.
3. **`repository` does not reach the cap and this plan will not guess how.**
   Fourteen of its 45 files pin to `Handler`, and of the other 31 exactly one
   has no inbound reference. Three cohesive leaves — `story` (5),
   `semantics` (4), `deployment` (4) — were each tested and each crosses the
   boundary in both directions, so each would be an import cycle; `story` and
   `deployment` also cross each other both ways. Getting under 40 needs a real
   seam: hoist the shared row helpers into `contract/`, or split the `Handler`
   type. Both are design changes rather than moves, and both are outside what
   this issue's Scope section authorizes ("Move and rename"). The options are
   (a) do that design work as its own issue, (b) accept `repository` keeping
   its `//nolint:dirgate` marker, or (c) widen this issue's scope. This is the
   one place the definition of done is not met by the plan as written.

4. **`repository/seam`, `code/seam` and `impact/seam`.** Three small leaves
   invented here for root files that adapt one family to another
   (`repository_authz.go`, `code_seam.go`, `family_impact_*.go`). After PR 1
   deletes the `repositoryAccessFilter` forwarders, `repository/seam` may
   collapse to two files and be worth folding into `contract/` instead. Left
   as leaves here so the mapping is complete; revisit at PR 32.
5. **`CodeHandler`.** Not renamed by this plan, per the issue's own
   precondition and #6649. Whether it is renamed at all is still open.

## Checklist

Updated as each PR lands.

- [ ] PR 1 — spine repoint
- [ ] PR 2 — `capability/`
- [ ] PR 3 — `querycontract` seven-leaf split (closes #6597's split question)
- [ ] PR 4 — `testutil/` ← `querytestutil`
- [ ] PR 5 — `span/` ← `tracing`
- [ ] PR 6 — `auth/`
- [ ] PR 7–30 — 24 leaf PRs, smallest first
- [ ] PR 31 — `code/`
- [ ] PR 32 — `repository/artifacts` rename (`repository/` itself blocked on UNDECIDED 3)
- [ ] PR 33 — Part B, `content/`
- [ ] PR 34 — alias sweep
- [ ] PR 35 — `querycontract` → `contract/`; root at 5 files; dirgate row retired
