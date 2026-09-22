# Query target tree: sequencing and open questions (#6642)

**This plan is four pages.** [The tree](6642-query-target-tree.md) ·
[What the moves cost](6642-query-move-cost.md) ·
[The file mapping](6642-query-file-mapping.md) ·
[Sequencing and open questions](6642-query-move-sequence.md)

## Two open questions the issue asked about, now answered

The issue's 2026-09-18 update flagged `security` and `replatforming` as
families with "no distinct file cluster found under those names — verify
whether they landed under a different name or were folded into another family
before treating them as done". Both are done:

- **`replatforming`** has no non-test root file left. Its types live in
  `iac_alias.go`'s 36 unexported forwarders (which delete with `iac/`), its
  capability rows in `contract/replatforming*.go` (three files, travelling to `iac/`), and its
OpenAPI fragment in
  `openapi/components_replatforming.go`. Only tests remain at root.
- **`security`** was never a family. Its one root non-test file,
  `content_reader_security_secrets.go`, is a `ContentReader` method and goes to
  `content/read/`. The rest of the name is spread across `supply/chain/`
  (security-alert reconciliation) and `codequery/security_secrets.go`.

## Sequencing

One destination directory per PR, in this order. The dependency that fixes the
order is the shared spine: nothing moves before PR 1.

| # | PR | files | why here |
| ---: | --- | ---: | --- |
| 1a | **Envelope spine repoint** ([#6977](https://github.com/eshu-hq/eshu/pull/6977)). Delete `requiredProfile` and `acceptsEnvelope`; repoint 31 files. | 0 moved | clears 1 of the 5 dominant spine symbols |
| 1b | **Authz/capability spine repoint.** Delete `capabilityUnsupported`, `repositoryAccessFilterFromContext` and the `repositoryAccessFilter` type; repoint 83 files. | 0 moved | clears 3 more. `startQueryHandlerSpan` is the 5th and belongs to [#6818](https://github.com/eshu-hq/eshu/issues/6818), not here — so this series clears 4 of 5, and the spine is clear for every move that follows |
| 2 | `capability/` — 4 root files, plus `capabilities.go`, `capability_matrix.go`, `capability_matrix_ext.go`, `capability_matrix_terraform.go` and `registry.go` from today's `contract/` | 9 | starts draining the `contract/` name. **Not small**: see the note below |
| 3 | `querycontract`'s seven leaf extractions, in place, without the rename | 19 | closes the #6597 split; the rename waits for the name |
| 4 | `testutil/` nesting — the `content` and `graph` leaves only. The `querytestutil` → `testutil` **rename itself belongs to [#6818](https://github.com/eshu-hq/eshu/issues/6818)** | 42 | test helpers, no production risk |
| 6 | the 52 root auth files, nested five ways under whatever `queryauth` is called by then | 52 | the largest root family; `auth/route/` alone is 24 files |
| 7–30 | **one parent per PR, smallest first. Each is a hoist-then-move**, not a move — see [the cycle analysis](6642-query-move-cost.md#the-tree-is-not-reachable-by-moving-files-alone) for the symbols each owes.** 24 PRs covering 33 leaves and 145 files: `decode` (1), `workload` (1), `dependency` (2), `observability/coverage` (2), `terraform/drift` (2), `kubernetes` (3), `metrics` (3), `compare` (4), `cicd` (5), `ask` (6), `collector` (8), `documentation` (8), `evidence` (10), `status` (14), the three seam leaves `code/seam` (2), `repository/seam` (3) and `impact/seam` (5), and the parent-grouped `semantic` (3), `graph` (5), `image` (8), `investigation` (11), `supply/chain` (7), `cloud` (11), `infra` (21) | 145 | order within the block is free; each PR carries its own hoist |
| 31 | `code/` ← `codequery` + the four `code*` siblings | 102 | large but mechanical; `CodeHandler` is **not** renamed here |
| 32 | `repository/`: move the 12 zero-outbound files into `repository/readmodel`, export the 34 names the parent calls; plus `repository/artifacts` ← `repositoryartifacts` | 65 | 15 queryplan `file:` keys re-key |
| 33 | **Part B.** `content/` ← `contentread`, `content/read/` (the `ContentReader` unit, with the four merges and the `semantic_evidence.go` split), `content/relationship/` | 56 moved, 52 after merges | the issue puts it last; it is the only big-bang |
| 34 | The alias sweep: delete all 21 root `*_alias.go` and migrate 1,420 external references | −21 | each family's aliases can only die after that family has moved |
| 35 | hand the free `contract/` name to #6818 once today's `contract/` is down to `doc.go`; root reduction to five files; re-pin the dirgate row; retire the `internal/query` ledger row | — | definition of done |

### PR 2 is a hoist, not a move (measured)

The earlier "7 files / small" estimate was wrong in both halves. Measured on
2026-09-22 by performing the move in a throwaway worktree and compiling with
`go build -gcflags=-e ./internal/query/...`:

- It is **9 files**, not 7 — 4 from root and 5 from `contract/`. The cost page's
  "4 root files + 5 from `contract/` = 9" was right; this table was not.
- The move breaks **42 files with 57 distinct undefined symbols, across two
  packages** — root `query` and `contract`. `registry.go` holds `register`,
  `capabilitySupport`, `truthExact` and `truthDerived`, which all 36 remaining
  `contract/` capability rows call.
- There is **no import cycle**, which the first draft of this note assumed there
  would be. The ~45 capability-id constants are *duplicated* per package: root's
  `capability_keys.go` and each `contract/` row each declare the same name with
  the same string value. Symbols needed in the reverse direction, measured: **0**.
- The one real back-edge is 5 root-facing helpers **defined inside the moving
  files**, each itself a pure forwarder: `writePermissionDeniedEnvelope`,
  `authContextAllowsPermissionFeature` and `permissionFeatureIdentityAdmin` onto
  `queryauth`, and `parseOffset` and `parseBoundedLimit` onto `querycontract`.

So PR 2 is a spine repoint of those 5 in the shape of PR 1a/1b, then the move.

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

## The query-plan pins, inspected

The issue asks for this before any `CodeHandler` rename: "inspect the current
query-plan entries and record affected digests and proof". Done — and it turns
out to bind every move PR, not just that rename.

`go/internal/queryplan` pins query code **by path**, in two places:

| manifest | keys | sha256 |
| --- | ---: | --- |
| `testdata/query-source-coverage.yaml` | 100 `file:` keys | `38fe4386acd7b595a74e9f18cf791509143182b90d3cbf25ea88380d54f9e562` |
| `testdata/hot-cypher.yaml` | 1 `CodeHandler` entry | `7a0092f6bccc0281edcf5a0ec1443d9b920ef638b3715cff183c5f7da5c39131` |
| `testdata/handler-hot-cypher.yaml` | 0 `CodeHandler` entries | `fc4d4294b63737fbdd4d30d7676679f48b7eda9f2a786752b783d8224e2a8c97` |
| `grandfathered_non_hot.go` | 16 entries, each `path:(*Type).method` → content digest | generated |

The 100 source-coverage keys sit under the directories this plan touches —
`repository` 15, `codequery` 15, `impact` 14, `entity` 8, `impacttrace` 6,
`package/registry` 4, `taghistory`, `service`, `queryselector` 2 each, plus
root-file keys. `grandfathered_non_hot.go` splits 7 under `codequery/` and the
rest on root files that move:

```
compare.go:(*CompareHandler).environmentSnapshot                     -> compare/handler.go
compare.go:(*CompareHandler).fetchWorkload                           -> compare/handler.go
infra_graph_summary_packet.go:(*InfraHandler).graphSummaryRelationshipCounts  -> infra/summary/packet.go
infra_graph_summary_packet.go:(*InfraHandler).graphSummaryRepoEcosystemMap    -> infra/summary/packet.go
infra_graph_summary_packet.go:(*InfraHandler).graphSummaryRepoLanguages       -> infra/summary/packet.go
infra_relationship_filter.go:(*InfraHandler).getRelationships        -> infra/relationship/filter.go
status.go:(*StatusHandler).getIndexStatus                            -> status/handler.go
```

Every one of those keys changes when its file moves. The digests are content
hashes, so a pure `git mv` leaves the hash valid and only the key stale — which
is the dangerous shape, because a stale key silently stops matching rather than
failing loudly. **Each move PR re-keys its own pins and proves the count is
unchanged**, and the `codequery` → `code/` PR re-keys 7 grandfathered entries
and 15 source-coverage keys in one go.

Thirty distinct `CodeHandler` methods are pinned across these manifests. That is
the inspection the `CodeHandler` rename owes, and it is now recorded; whether
the rename happens at all remains open under #6649.

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

This list started at seven. Six were settled by evidence rather than left to
you:

- **The root-test rule** — a measured partition rather than a proposal: 560 of
  the 728 root tests move to a single destination at a median 100% reference
  share, 30 stay at root as `package query_test`, and 138 reference nothing
  that moves.
- **The `tracing`/`queryspan` identification** — `git log --all
  --grep=queryspan` finds PR #6846, "move query/queryspan to query/tracing",
  merged. Not an inference from a doc comment.
- **The `content/read` merges** — `go doc -all ./internal/query` is byte-identical
  before and after all four, so they keep the exported surface the issue's Scope
  requires.
- **The `contract/` displacement** — 37 of its 40 files are per-family
  capability rows that travel with their families, as seven already-moved
  families demonstrate. No displacement PR is needed.
- **The three `seam` leaves** — counting consumers dissolves all three.
  `repository_authz.go` is consumed by 18 destinations and is plainly
  `contract/`; four of the five `impact/seam` files have no consumer outside
  the leaf and belong with `impact/trace`. No `seam` directory survives.

- **`repository`** — the split failed when grouped by topic and succeeds when
  grouped by dependency direction. Twelve files have zero outbound references
  and move down into `repository/readmodel` one-way, compiler-proven; 45 → 33,
  17 in the child, marker retired.

What remains is one item, and the issue itself defers it.

1. **`CodeHandler`.** Not renamed by this plan, per the issue's own
   precondition and #6649. Whether it is renamed at all is still open.

## Checklist

Updated as each PR lands.

- [ ] PR 1 — spine repoint
- [ ] PR 2 — `capability/`
- [ ] PR 3 — `querycontract` seven-leaf split (closes #6597's split question)
- [ ] PR 4 — `testutil/` ← `querytestutil`
- [ ] PR 6 — `auth/`
- [ ] PR 7–30 — 24 leaf PRs, smallest first
- [ ] PR 31 — `code/`
- [ ] PR 32 — `repository/artifacts` rename (`repository/` itself blocked on UNDECIDED 1)
- [ ] PR 33 — Part B, `content/`
- [ ] PR 34 — alias sweep
- [ ] PR 35 — `contract/` name handed to #6818; root at 5 files; dirgate row retired
