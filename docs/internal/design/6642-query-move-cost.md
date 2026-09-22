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

The fifth already has a home too. `startQueryHandlerSpan` is a nine-line wrapper
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

## The alias ledger

Twenty-one `*_alias.go` files sit at root. The issue forbids retaining them.
Each row below is measured: exported symbol count, the number of `query.X`
references to those symbols from outside `go/internal/query`, and the number of
unexported symbols the file also declares.

**1,509 external `query.X` references resolve through these files.** That, not
the file count, is the caller-migration bill for Part A's endgame. Repo-wide,
336 files outside `go/internal/query` (128 of them non-test) name 585 distinct
`query.X` symbols — the issue's "313 external files name 535 distinct symbols"
has grown to 336/585 since it was written.

| file | exported | external refs | unexported | disposition |
| --- | ---: | ---: | ---: | --- |
| `envelope_aliases.go` | 52 | **1026** | 4 | SPLIT FIRST: hoist `requiredProfile`/`acceptsEnvelope` (PR 1), then delete with the largest caller migration in the issue |
| `admin_alias.go` | 63 | 130 | 0 | DELETE with caller migration to `admin/` |
| `local_identity_alias.go` | 32 | 67 | 4 | DELETE with caller migration to `local/` |
| `iac_alias.go` | 62 | 42 | 36 | DELETE; 36 unexported symbols must land in `iac/` first |
| `freshness_alias.go` | 22 | 42 | 2 | DELETE with caller migration to `freshness/` |
| `semantic_search_alias.go` | 21 | 35 | 0 | DELETE with caller migration to `semantic/search/` |
| `query_playbook_alias.go` | 22 | 34 | 6 | DELETE with caller migration to `playbook/` |
| `incident_alias.go` | 48 | 25 | 0 | DELETE with caller migration to `incident/` |
| `service_alias.go` | 14 | 17 | 17 | DELETE; 17 unexported symbols, 15 of them test-only |
| `secrets_alias.go` | 7 | 16 | 6 | DELETE with caller migration to `secrets/` |
| `work_item_alias.go` | 14 | 14 | 1 | DELETE with caller migration to `workitem/` |
| `content_read_alias.go` | 8 | 12 | 0 | DELETE in the Part B change, as the issue directs |
| `visualization_alias.go` | 10 | 11 | 0 | DELETE with caller migration to `visualization/` |
| `entity_alias.go` | 2 | 10 | 13 | DELETE; all 13 unexported symbols are test-only |
| `code_alias.go` | 1 | 8 | 0 | DELETE; one symbol, `CodeHandler`, 8 external refs |
| `repository_alias.go` | 3 | 8 | 0 | DELETE with caller migration to `repository/` |
| `package_registry_alias.go` | 6 | 7 | 0 | DELETE with caller migration to `package/registry/` |
| `language_alias.go` | 3 | 4 | 3 | DELETE with caller migration to `language/` |
| `answer_metadata_alias.go` | 3 | 1 | 1 | DELETE with caller migration to `ask/` |
| `k8s_match_alias.go` | 0 | 0 | 8 | DELETE outright: no exported surface, no external caller |
| `entity_alias_live.go` | 0 | 0 | 1 | DELETE outright, but it is build-tag gated — see below |

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
`repository_compat.go`, `compat_supply_chain.go` (495 lines of real logic) and
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
Merging them is not an option (96 files, and the concepts differ), so the name
goes to the one the issue named:

- `querycontract` → `contract/` (shared vocabulary; matches the reducer
  precedent, where `contract/` is "top-level shared vocabulary").
- `contract/` → `capability/matrix/` (the support rows), beside the root
  capability handler at `capability/`.

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

**56 − 19 = 37 files** left in `contract/`, four under the cap, and the
`//nolint:dirgate` tracker on #6597 retires.

What stays is genuinely one unit. The remaining groups — the core vocabulary,
graph/truth, HTTP envelope, story helpers, content ports, repository authz and
infra scope — are mutually dependent in both directions: `core ↔ graph`,
`core ↔ http`, `core ↔ story`, `core ↔ content`, `core ↔ repository`, and
`infra ↔ repository`. Splitting any of those pairs into siblings creates an
import cycle. `contract/code` is the cleanest extraction in the package: two
files, zero references in either direction.

`contract/rowvalue` (2 files) already exists nested and is unchanged.

## The other three over-cap directories

| directory | now | action | after |
| --- | ---: | --- | ---: |
| `repository` | 45 | nest `story` (5), `semantics` (4), `deployment` (4) | 32 |
| `testutil` ← `querytestutil` | 42 | nest `content` (10), `graph` (8) | 24 |
| `code` ← `codequery` | 41 | delete `aliases.go`; `responses.go` → `code/shaping`; `handler_tracing.go` → `span` | 38 |

`repository`: 14 of its 45 files declare methods on `Handler` and pin
together; the three proposed leaves are drawn only from the other 31.
`story.go`, `story_counts.go`, `story_deployment_evidence.go`,
`deployment_overview_story.go` and `narrative_enrichment.go` are free;
`story_coverage.go` declares `Handler` methods and stays. The existing
`//nolint:dirgate` warns that "splitting a subpackage rechurns the queryplan
file pins" — that is a regeneration obligation on the move PR, not a reason
not to split.

`testutil`: the de-stutter pass forces this one on its own. Nineteen of its
files are glued compounds that naming.md rule 3 rejects —
`contentreaderargs.go`, `deadcodecontentstore.go`,
`patternconsumersearchcontentstore.go`, `evaluatingrepositorygraph.go`,
`fakegraphreaderwithsingle.go` and their siblings. Nesting them as
`testutil/content/reader_args.go`, `testutil/content/dead_code_store.go`,
`testutil/graph/reader.go` and so on both fixes the names and clears the cap.

`code`: 33 of 41 files declare `CodeHandler` methods and cannot separate.
The three changes above come from the eight that do not, and none of them
touches the `divergenceStore` seam that `codequery/doc.go:39` protects — that
marker explicitly refuses a divergence subpackage, so this plan does not
propose one.

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

The rule this document proposes:

- A test that names symbols from exactly one destination moves with it and
  becomes an internal test there.
- A test that drives the composed router, or spans two or more destinations,
  stays at root and becomes `package query_test`, importing what it needs.
- The conversion happens in the same PR as its family's move, not as a sweep
  afterwards.

Twenty-four root test files are behind `//go:build` tags and need the
constraint-name check described under [Build-tagged files](6642-query-target-tree.md#build-tagged-files)
before their move is trusted.

## Two open questions the issue asked about, now answered

The issue's 2026-09-18 update flagged `security` and `replatforming` as
families with "no distinct file cluster found under those names — verify
whether they landed under a different name or were folded into another family
before treating them as done". Both are done:

- **`replatforming`** has no non-test root file left. Its types live in
  `iac_alias.go`'s 36 unexported forwarders (which delete with `iac/`), its
  capability rows in `contract/replatforming*.go` (three files, moving to
  `capability/matrix/`), and its OpenAPI fragment in
  `openapi/components_replatforming.go`. Only tests remain at root.
- **`security`** was never a family. Its one root non-test file,
  `content_reader_security_secrets.go`, is a `ContentReader` method and goes to
  `content/read/`. The rest of the name is spread across `supply/chain/`
  (security-alert reconciliation) and `codequery/security_secrets.go`.
