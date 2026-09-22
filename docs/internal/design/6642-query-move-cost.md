# Query target tree: what the moves cost (#6642)

**This plan is four pages.** [The tree](6642-query-target-tree.md) ·
[What the moves cost](6642-query-move-cost.md) ·
[The file mapping](6642-query-file-mapping.md) ·
[Sequencing and open questions](6642-query-move-sequence.md)

Companion to [the tree](6642-query-target-tree.md). Every count here was
measured at `origin/main` `44e5607db` on 2026-09-22; the commands are in that
page's "How the numbers were taken".

## What a move actually costs: the shared spine

File counts do not predict the difficulty of a move. What predicts it is the
number of **unexported** symbols that cross the new package boundary, because
each one has to be exported into a shared leaf or carried along.

A second `go/ast` pass resolved every cross-file identifier reference in the
root package against its declaring file, then mapped both ends onto the
destinations in this document. **192 distinct unexported symbols cross at
least one proposed boundary.** Five of them dominate:

| symbol | consuming destinations | declared in | proposed home |
| --- | ---: | --- | --- |
| `requiredProfile` | 19 | `envelope_aliases.go` | `contract.RequiredProfile` |
| `capabilityUnsupported` | 18 | `handler.go` | `contract.CapabilityUnsupported` |
| `startQueryHandlerSpan` | 17 | `handler_tracing.go` | `span.StartHandler` |
| `repositoryAccessFilterFromContext` | 16 | `repository_authz.go` | `contract.RepositoryAccessFilterFromContext` |
| `repositoryAccessFilter` | 8 | `repository_authz.go` | `contract.RepositoryAccessFilter` |

Four of those five need no new API at all. The exported twins already exist in
`querycontract` (`contract/` after the rename) and the root declarations are
unexported forwarders onto them:

```
querycontract/http.go:132                func CapabilityUnsupported(...)
querycontract/http.go:137                func RequiredProfile(...)
querycontract/http.go:18                 func AcceptsEnvelope(...)
querycontract/repository_authz.go:21     type RepositoryAccessFilter
querycontract/repository_authz_context.go:24  func RepositoryAccessFilterFromContext(...)
```

`repository_authz.go`'s own `//nolint:dirgate` marker says exactly that — "the
unexported alias and forwarder names are used across root call sites while the
type lives in querycontract".

The fifth already has a home too. `startQueryHandlerSpan` is a three-line wrapper
over `tracing.StartHandlerSpanWith` (`tracing/handler.go:48`), and `tracing/` is
what the issue's Part D calls `queryspan`. So **PR 1 adds no new API** — it
repoints call sites at five functions that already exist and deletes the
forwarders.

One wrinkle is worth stating before it surprises a reviewer.
`handler_tracing.go` exists for a package-local `var queryHandlerTracer`, and
its own comment says why: three root span tests swap that var for a recording
provider, "so it must stay a package-local var … keeping the swap private to
this package rather than mutating what every other importer reads". Each
destination that starts a span therefore carries its own one-line tracer var
seeded from `tracing.HandlerTracer()`. That is a replicated seam, not a shared
one, and it is deliberate.

**This sets the order.** No family can move until those five are reachable,
because a family that leaves root loses access to all five on the way out. PR 1
in the sequence below does that repointing and moves no family.

Below the top five, the coupling is local rather than global. The heaviest
remaining edges are `content/read` needing 16 unexported helpers from
`documentation`, 9 from `cloud`, 6 each from `evidence` and `auth/acl`, and 5
from `repository/seam` — which is the same statement as "the `ContentReader`
read models were written next to the families they read for". Each of those is
resolved inside the `content/read` move by exporting the helper from its owning
package, and the count is the honest size of that PR.

## The tree is not reachable by moving files alone

This is the finding that changes the plan, and it was missed on the first pass.

Build the destination graph — 45 nodes, being the 42 destinations in
[the mapping](6642-query-file-mapping.md) plus the root spine, the alias files,
and `semantic_evidence.go` which splits across two; one edge wherever a file
references a symbol declared in a file bound for a different node — and run a
strongly-connected-component pass over it. **Thirty-eight of the 45 nodes fall
into a single component.** Every one of them mutually
depends on another, so none of them can become a Go package while the others
stand still.

Most of that is an artifact of today's root, not of the tree. Retire the two
things PR 1 and the alias sweep remove — the five spine symbols, and every edge
into a `*_alias.go` or into root — and the picture improves but does not clear:
**23 destinations remain in one component, across 18 mutually-importing pairs
and 85 distinct unexported symbols.**

| pair | a→b | b→a | unexported symbols to resolve |
| --- | ---: | ---: | ---: |
| `content/read` ↔ `documentation` | 16 | 1 | 17 |
| `content/read` ↔ `cloud` | 9 | 2 | 11 |
| `infra` ↔ `infra/aggregate` | 1 | 10 | 9 |
| `content/read` ↔ `evidence` | 6 | 2 | 7 |
| `auth` ↔ `auth/route` | 5 | 7 | 6 |
| `supply/chain` ↔ `supply/chain/sbom` | 2 | 20 | 5 |
| `supply/chain` ↔ `investigation/packet` | 13 | 2 | 5 |
| `evidence` ↔ `investigation/packet` | 6 | 4 | 5 |
| `auth/session` ↔ `auth/signin` | 2 | 13 | 4 |
| `auth` ↔ `status` | 2 | 4 | 4 |
| `auth` ↔ `auth/signin` | 1 | 9 | 4 |
| `auth` ↔ `auth/session` | 1 | 9 | 4 |
| `investigation/packet` ↔ `cloud/drift` | 1 | 6 | 3 |
| `semantic_evidence.go`'s two halves | 2 | 1 | 3 |
| `supply/chain` ↔ `kubernetes` | 1 | 4 | 2 |
| `cloud` ↔ `supply/chain` | 3 | 1 | 2 |
| `supply/chain` ↔ `image` | 2 | 11 | 1 |
| `content/read` ↔ `semantic_evidence.go`'s read half | 1 | 1 | 1 |

A mutual import is a cycle whatever the export status of the symbols, so
`supply/chain → supply/chain/sbom` being exported-only does not save that pair.

Three shapes account for all eighteen.

**A family is cyclic with its own leaves.** `auth` is mutually dependent with
`auth/route`, `auth/session` and `auth/signin`; `infra` with `infra/aggregate`
and `infra/relationship`; `supply/chain` with `supply/chain/sbom`. Collapsing
each family back into one package would fix it and is not available — `auth`
would be 60 files and `infra` 21. So the split stands and the shared symbols
must move somewhere both sides can import.

**Shared vocabulary was written into whichever family needed it first.**
`packetBoundsFromRequest`, `refusalPacketForAPI` and `writeInvestigationPacket`
are needed by `evidence`, `cloud/drift` and `supply/chain` as well as
`investigation/packet`; `normalizeAuthContext` and `unauthorizedResponse` by
three auth leaves. These are not family internals, they are contract, and they
belong in `contract/` below every consumer.

**A read model sits on the wrong side.** `content/read` ↔ `documentation` (17
symbols) and ↔ `cloud` (11) are the same statement as
[where the prefix lies](6642-query-target-tree.md#where-the-prefix-lies): the
`ContentReader` read models were written next to the families they read for,
and the SQL-building helpers stayed behind when the methods left.

**What this means for the plan.** Every family move is a *hoist and move*, not a
move: the family's PR first lifts the symbols on its row above into `contract/`
(or into the leaf both sides can import), proves the direction is one-way, and
only then relocates the files. The 85 symbols are the complete bill, measured,
and each row names its own share. The sequencing table reflects this.

No family PR is "independent once PR 1 lands" — that claim was wrong in the
first draft of this page and is retracted here. What PR 1 buys is the removal
of the root and alias edges, which is what takes the component from 38 nodes to
23.

## The three seam leaves dissolve

An earlier draft invented `repository/seam`, `code/seam` and `impact/seam` for
root files that adapt one family to another, and left their placement
UNDECIDED. Counting who actually consumes each file's symbols settles all three:

| file | symbols | consuming destinations | goes to |
| --- | ---: | ---: | --- |
| `repository_authz.go` | 4 (2 retire in PR 1) | **18** | `contract/` — shared vocabulary by any measure |
| `repository_compat.go` | 7 | **5** — `content/read`, `content/relationship`, `entity/`, `contract/*`, `repository/` | `contract/` |
| `repository_selector.go` | 1 | 1 (`cicd`) | `cicd/` |
| `code_seam.go` | 33 | **8** — `code/`, `content/read`, `impact/trace`, `language/`, `entity/`, `contract/*`, `auth/route`, `infra/summary` | `contract/code` |
| `family_codeowners_shim.go` | 1 | 1 root + 2 external | `code/owners`; deletes in the alias sweep once `handler.go`'s field and the `cmd/api` and `cmd/mcp-server` call sites migrate to `codeowners.Handler` |
| `family_impact_*.go`, `deployment_trace_support_helpers.go` | 31 | **0** for four of the five files | `impact/trace` |

The impact files are the interesting case: four of the five have no consumer
outside the leaf at all, which means they were never a seam — they are impact's
own backends, and they belong with `impact/trace` (28 files, 33 after). The
`repository_authz.go` row is the opposite extreme at 18 consuming destinations,
which is the definition of shared vocabulary.

No file needs a `seam` directory. The word was doing work the measurement does
better.

Two corrections a reviewer caught in the fan-out column, both worth stating
because they change the price rather than the placement. The consumer counts
above were first computed over `go/internal/query` **root files only**, which
undercounts any symbol used from a subpackage: `code_seam.go` reaches 8
destinations, not 3, and `repository_compat.go` 5, not 3. Broader sharing
strengthens the case for putting both in `contract/`, but it widens the PR 31
and contract-hoist fan-out, so the corrected lists are above. `CodeownersOwnershipHandler`
is likewise referenced from `go/cmd/api/wiring_router.go` and
`go/cmd/mcp-server/wiring_router.go`, which sit outside the 1,420-reference alias
sweep entirely.

## The alias ledger

Twenty-one `*_alias.go` files sit at root. The issue forbids retaining them.
Each row below is measured: exported symbol count, the number of `query.X`
references to those symbols from outside `go/internal/query`, and the number of
unexported symbols the file also declares.

**1,420 external `query.X` references resolve through these files.** That, not
the file count, is the caller-migration bill for Part A's endgame.

The counting rule matters, because a looser one inflates it. These figures count
only **tracked `.go` files outside `go/internal/query` that import the package
*and* name a `query.X` symbol** — not every file whose text happens to contain
`query.Something`. On that rule, 314 files (120 of them non-test) name 474
distinct `query.X` symbols, and 184 of the 393 symbols the alias files export are
actually referenced from outside. The issue's "313 external files name 535
distinct symbols" is the same order of magnitude measured differently; 314/474
is what this rule gives today.

| file | exported | external refs | unexported | disposition |
| --- | ---: | ---: | ---: | --- |
| `envelope_aliases.go` | 52 | **1022** | 4 | SPLIT FIRST: hoist `requiredProfile`/`acceptsEnvelope` (PR 1), then delete with the largest caller migration in the issue |
| `admin_alias.go` | 63 | 115 | 0 | DELETE with caller migration to `admin/` |
| `local_identity_alias.go` | 32 | 60 | 4 | DELETE with caller migration to `local/` |
| `semantic_search_alias.go` | 21 | 35 | 0 | DELETE with caller migration to `semantic/search/` |
| `freshness_alias.go` | 22 | 34 | 2 | DELETE with caller migration to `freshness/` |
| `query_playbook_alias.go` | 22 | 22 | 6 | DELETE with caller migration to `playbook/` |
| `iac_alias.go` | 62 | 20 | 36 | DELETE; 36 unexported symbols must land in `iac/` first |
| `incident_alias.go` | 48 | 17 | 0 | DELETE with caller migration to `incident/` |
| `secrets_alias.go` | 7 | 15 | 6 | DELETE with caller migration to `secrets/` |
| `service_alias.go` | 14 | 14 | 17 | DELETE; 17 unexported symbols, 13 of them test-only |
| `content_read_alias.go` | 8 | 12 | 0 | DELETE in the Part B change, as the issue directs |
| `work_item_alias.go` | 14 | 9 | 1 | DELETE with caller migration to `workitem/` |
| `visualization_alias.go` | 10 | 9 | 0 | DELETE with caller migration to `visualization/` |
| `entity_alias.go` | 2 | 9 | 13 | DELETE; all 13 unexported symbols are test-only |
| `repository_alias.go` | 3 | 8 | 0 | DELETE with caller migration to `repository/` |
| `code_alias.go` | 1 | 8 | 0 | DELETE; one symbol, `CodeHandler` |
| `package_registry_alias.go` | 6 | 7 | 0 | DELETE with caller migration to `package/registry/` |
| `language_alias.go` | 3 | 3 | 3 | DELETE with caller migration to `language/` |
| `answer_metadata_alias.go` | 3 | 1 | 1 | DELETE with caller migration to `ask/` |
| `k8s_match_alias.go` | 0 | 0 | 8 | DELETE outright: no exported surface, no external caller |
| `entity_alias_live.go` | 0 | 0 | 1 | DELETE outright, but it is build-tag gated — see below |

A large `exported` count with a small `external refs` count is the interesting
shape: `iac_alias.go` exports 62 symbols and 20 references reach them, and
`incident_alias.go` exports 48 for 17. Most of each file is already dead weight
that the family move can simply drop.

Two qualifications the raw counts hide:

**`envelope_aliases.go` is not an alias file.** It carries 52 real type aliases
to `querycontract`, *and* it declares `requiredProfile` and `acceptsEnvelope`,
the two most widely shared unexported symbols in the package. Deleting it
without hoisting those first breaks 19 destinations at once.

**"Zero non-test consumers" does not mean dead.** The reference count above
counts test files separately, and several alias files exist almost entirely for
root tests: all 13 of `entity_alias.go`'s unexported symbols have zero
production callers and between 1 and 5 test callers each, and 13 of
`service_alias.go`'s 17 are the same shape. Those forwarders disappear when
their tests move to the owning package or start calling it package-qualified —
not before. `entity_alias_live.go` is the sharpest case: one symbol, one test
caller, behind a `//go:build` tag, so a default `go test ./...` will not notice
if it breaks.

Three more files carry `alias`-adjacent names without being aliases:
`repository_compat.go`, `compat_supply_chain.go` (494 lines of real logic) and
`cloud_resource_forwarders.go`. They are mapped as ordinary family files, not
as deletions.

## #6597 and the `contract` name collision

#6597 asks for a split axis for `querycontract` before more seams land there.
The axis falls out of this tree, so #6597 is resolved here rather than run
separately.

First, the collision. Two packages under `query/` want the name `contract`:

- `querycontract` (56 files) is the **shared vocabulary** — request and
  response types, ports, envelope and error codes, read models, the truth
  level. It is imported by 24 packages: 117 references from root, 87 from
  `repository`, 85 from `codequery`, 77 from `impact`, and so on down.
- `contract` (40 files) is the **capability support matrix** — one row per
  capability, each registering itself through
  `querycontract.RegisterCapabilities` in an `init()`. Its own `doc.go` says
  so.

They are different things and the dependency runs one way: four `contract/`
files import `querycontract`, and no `querycontract` file imports `contract`.

**But the collision dissolves on its own, and that changes the order.** Of
`contract/`'s 40 files, **37 are a single `init()` registering one capability
row** — `contract/kubernetes.go` is nine lines of code registering
`kubernetesCorrelationsCapability`, and its 36 siblings have the same shape.
Only `capability_matrix.go`, `registry.go` and `doc.go` are shared.

Those 37 rows are not a package. They are the residue of families that have not
moved yet, and the repository has already been migrating them the other way: of
the families that have moved, `playbook`, `secrets`, `incident`,
`package/registry` each hold one registration inside their own package, and
`impact` holds three, `service` two, `semanticsearch` two. A family's capability
row travels with the family; that is established practice here, not a proposal.

So today's `contract/` empties itself completely as the family moves land: 34 of
its rows go to the families listed in the arithmetic table, 3 more
(`capabilities.go`, `capability_matrix_ext.go`, `capability_matrix_terraform.go`)
go to `capability/`, and `capability_matrix.go` and `registry.go` follow them
there as `capability/matrix.go` and `capability/registry.go`. Its `doc.go` is
deleted with the directory. 34 + 5 + 1 = 40, the whole package.

To keep `capability/registry.go` unambiguous, root's `capability_registry.go`
lands as `capability/lookup.go` rather than `capability/registry.go` — the
mapping is updated to match, and the collision a reviewer caught here does not
arise. `capability/` totals 4 root files + 5 from `contract/` = 9.
The consequence for sequencing is that the `querycontract` rename goes **late**,
after the families have drained the name, rather than second.

Second, the split. A third `go/ast` pass resolved every cross-file reference
inside `querycontract` and grouped the 56 files by consumer family. Seven
groups are true leaves: they reference other groups, and **nothing references
them back**, so extracting each is acyclic.

| leaf | files | depends on | depended on by |
| --- | ---: | --- | --- |
| `contract/answer` | 3 | graph, story, core, evidence | nothing |
| `contract/code` | 2 | nothing | nothing |
| `contract/entity` | 3 | content, core, repository | nothing |
| `contract/evidence` | 3 | nothing | answer, visualization |
| `contract/kubernetes` | 2 | content, story | nothing |
| `contract/language` | 4 | story, content | nothing |
| `contract/visualization` | 2 | graph, evidence | nothing |

All seven pass the same cycle test that rejected the `repository` leaves below:
`stay → leaf` is **zero** for every one of them, so each extraction is one-way
by construction rather than by argument. The only leaf-to-leaf edges are
`answer → evidence` (1 symbol) and `visualization → evidence` (2), both one-way,
which is why `evidence` is listed as a base rather than a consumer.

**56 − 19 = 37 files** left in `contract/`, three under the cap, and the
`//nolint:dirgate` tracker on #6597 retires.

What stays is genuinely one unit. The remaining groups — the core vocabulary,
graph/truth, HTTP envelope, story helpers, content ports, repository authz and
infra scope — are mutually dependent in both directions: `core ↔ graph`,
`core ↔ http`, `core ↔ story`, `core ↔ content`, `core ↔ repository`, and
`infra ↔ repository`. Splitting any of those pairs into siblings creates an
import cycle. `contract/code` is the cleanest extraction in the package: two
files, zero references in either direction.

`contract/rowvalue` (2 files) already exists nested and is unchanged.

## The three over-cap directories, all resolved

Each proposed split was tested, not assumed. The test: build the file-level
reference graph inside the package, assign files to the proposed leaf, and check
whether references cross the boundary in **both** directions. A two-way crossing
is an import cycle once the leaf becomes its own package, and no amount of
exporting fixes it.

Two of the three hold. One does not.

The test itself was checked against the compiler before being trusted, on both
polarities, in a throwaway worktree off `origin/main` `44e5607db`:

```
# GREEN: a leaf the test calls acyclic
  querycontract/{cross_repo_dead_code_reads,dead_code_incoming_edge}.go
  -> querycontract/code/, package code
  go build ./internal/query/querycontract/...        exit 0

# RED: a leaf the test calls cyclic
  repository/{story,story_counts,story_deployment_evidence,
              deployment_overview_story,narrative_enrichment}.go
  -> repository/story/, package story
  go build ./internal/query/repository/...           fails BOTH ways:
    story/core.go:76     undefined: buildRepositorySemanticStory
    story/counts.go:73   undefined: queryRepositoryFileCount
    story/deployment_overview.go:160  undefined: intValue
    repository/deployment_overview.go:54  undefined: buildOverviewDeliveryFamilyStory
    repository/deployment_overview.go:59  undefined: buildOverviewTopologyStory
```

The `repository` failure is the two-way crossing made concrete: the leaf cannot
see what stays, and what stays cannot see the leaf. The probe worktree was
removed; nothing of it is in this branch.

| directory | now | action | after |
| --- | ---: | --- | ---: |
| `testutil` ← `querytestutil` | 42 | nest `content` (10) and `graph` (8) | 24 |
| `code` ← `codequery` | 41 | delete `aliases.go`; `handler_tracing.go` → `span` | 39 |
| `repository` | 45 | **no relocation-only split exists** — see below | 45 |

**`testutil` is clean.** Both proposed leaves are fully isolated: zero
references cross the boundary in either direction. The de-stutter pass forces
this split on its own anyway — nineteen of its files are glued compounds that
naming.md rule 3 rejects (`contentreaderargs.go`, `deadcodecontentstore.go`,
`patternconsumersearchcontentstore.go`, `evaluatingrepositorygraph.go`,
`fakegraphreaderwithsingle.go` and siblings). Nesting them as
`testutil/content/reader_args.go`, `testutil/content/dead_code_store.go` and
`testutil/graph/reader.go` fixes the names and clears the cap in one move.

**`code` clears by two files, and only two.** Thirty-three of its 41 files
declare `CodeHandler` methods and cannot separate. Of the eight that remain,
three candidates were tested and two failed:

| candidate | references out | references back | verdict |
| --- | ---: | ---: | --- |
| `handler_tracing.go` → `span/` | 1 | 0 | one-way, safe |
| `responses.go` → `code/shaping/` | 4 | 9 | **cycle** |
| `wrapper_bypass.go` + `_cypher.go` → `code/bypass/` | 12 | 2 | **cycle** |

So the action is `handler_tracing.go` to `span/` plus deleting
`codequery/aliases.go`, which the no-alias rule condemns anyway: it is a file of
"plain type alias or thin forwarder onto querycontract", by its own header
comment. Deleting it means qualifying its call sites with `querycontract.`
inside the package — mechanical, but the widest edit in that PR. 41 − 2 = 39.
This plan does not propose a divergence subpackage; `codequery/doc.go:39`
explicitly refuses one, and the tested candidates above are the alternatives.

**`repository` splits by direction, not by topic.** My first three attempts —
`story` (5 files), `semantics` (4), `deployment` (4) — were grouped by subject
matter, and all three failed the two-direction test; the compiler confirmed it
in both directions. That is recorded above because the failure is instructive,
not because the package is unsplittable.

Grouping by dependency direction instead finds the answer immediately. Of the 31
non-`Handler` files, **12 have zero outbound references into `repository`** —
they pull nothing back, so each can move *down* into a child with the import
running one way only:

```
capability.go              context_counts.go        families.go
api_surface.go             context_degrade.go       groups.go
dependency_edge_cap.go     infrastructure.go        relationship_overview.go
javascript_semantics.go    infrastructure_degrade.go
python_semantics_promotion.go
```

Compiler-proven in a throwaway worktree off `origin/main`: moving all twelve
into the existing `repository/readmodel/` as `package readmodel` gives

```
go build ./internal/query/repository/readmodel/   exit 0          # the moved package is clean
go build ./internal/query/repository/             34 undefined symbols, ALL in the parent
```

Every error is on one side. Compare the `story` attempt, where they appeared on
both — that is the whole difference between a split and a cycle. The 34 are
unexported names the parent still calls, so the cost is 34 exports in the child,
not a 73-symbol hoist into `contract/`.

**`repository` 45 → 33, `readmodel` 5 → 17, both under the cap, no
`//nolint:dirgate` left, no widened Scope, no separate seam issue.** The marker's
own justification — "splitting a subpackage rechurns the queryplan file pins" —
still applies as a regeneration obligation on that PR, and `repository` has 15
`file:` keys in `query-source-coverage.yaml` to re-key.

## What stays at root

Five non-test files, and nothing else:

| file | new name | why it stays |
| --- | --- | --- |
| `doc.go` | `doc.go` | the package contract |
| `handler.go` | `router.go` | `APIRouter` — the composition root that mounts every family |
| `handlers.go` | `router_helpers.go` | router construction helpers |
| `handler_tracing.go` | `tracing.go` | until PR 1 moves `startQueryHandlerSpan` to `span/`; after that it is deleted |
| `ports.go` | `ports.go` | the interfaces the router accepts |

That is 277 → 5. `handler.go` is the only one that genuinely cannot move: it
is what every family is mounted onto.

## The 728 root test files

The cap excludes test files, so the 728 root `*_test.go` files do not block the
definition of done. They do need a stated rule, because 726 of them are
`package query` internal tests and only 2 are `package query_test`.

Assigning each root test to the non-test file whose stem is its longest
matching prefix: **325 match a root file that moves, and 403 do not.** The 403
break down by leading token as `openapi_*` (63), `supply_*` (42),
`repository_*` (34), `language_*` (30), `code_*` (26), `entity_*` (23),
`service_*` (21), `graph_*` (20), `impact_*` (20), and a long tail.

Every one of those families already moved. `openapi/` took all 114 of its
non-test files out of root in Part C (#6648) and left 63 tests behind. The
tests stayed because they exercise the composed router over HTTP rather than
the moved package's API — `supply_chain_impact_findings_test.go` is
`package query`, imports `query/supply/chain/impact`, and drives
`httptest` against the mounted route.

The rule, and it is measured rather than proposed. Resolve every identifier each
test references to its declaring file, map that file to its destination, and
assign the test to the destination the plurality of its references point at:

| | tests | |
| ---: | --- | --- |
| **560** | reference at most three destinations | move to the winning one |
| **30** | reference four or more | stay at root as `package query_test` |
| **138** | reference no moving destination | stay; they test the root spine or already-moved subpackages |

The signal is strong, not marginal: for the 560 movers the **median share of
references pointing at the winning destination is 100%**, the mean is 82%, and
57% sit at 80% or above. Only one test in 728 references eight or more
destinations.

The destinations that inherit the most are the ones the mapping already says are
biggest — `content/read` 118, `auth` 112, `supply/chain` 61, `graph/read` 44,
`infra` 21. Each family's move PR carries its own share; no separate test sweep
is needed, and no test is stranded.

Twenty-four root test files are behind `//go:build` tags and need the
constraint-name check described under [Build-tagged files](6642-query-target-tree.md#build-tagged-files)
before their move is trusted.
