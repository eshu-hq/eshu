# Query target tree (#6642)

Proposed destination for the `go/internal/query` restructure, for owner
approval before any file moves. Every count on this page was measured against
`origin/main` `44e5607db` on 2026-09-22. #6597 (`querycontract` over the
directory cap) is mapped here rather than run separately, because its split
axis falls out of this tree.

Nothing has moved. These four pages are the whole of the first PR.

**This plan is four pages.** [The tree](6642-query-target-tree.md) ·
[What the moves cost](6642-query-move-cost.md) ·
[The file mapping](6642-query-file-mapping.md) ·
[Sequencing and open questions](6642-query-move-sequence.md)

## How the numbers were taken

```bash
git ls-tree -r --name-only origin/main go/internal/query/ \
  | rg '^go/internal/query/[^/]+\.go$' | rg -v '_test\.go$' | wc -l   # 277 root non-test
git ls-tree -r --name-only origin/main go/internal/query/ \
  | rg '^go/internal/query/[^/]+_test\.go$' | wc -l                   # 728 root test
```

Family membership was taken by symbol, not by filename. A `go/ast` pass over
all 1,005 root files recorded each file's method receivers, type declarations,
free functions, and exported names; the grouping below follows the receiver
type, because Go pins a method to the package that declares its receiver. The
issue's own warning — "prefixes lie in this package" — held: see
[Where the prefix lies](#where-the-prefix-lies).

## The cap, exactly

`tools/golangci-lint-dirgate/dirgate.go:35` sets `maxDirFiles = 40`. The count
is **per directory, non-recursive**, and `_test.go` files are excluded
(`dirgate.go:152-167`). Nested subdirectories are counted separately, so
`auth/` and `auth/route/` each get their own budget of 40.

`internal/query` is additionally pinned in `scripts/lib/dirgate-grandfather.tsv`:

```
internal/query	277	9c52f08bbe86185263d306745269f3df99b31608126c77a9184125f6b67ee005
```

That row is a **monotonic ratchet**, not a snapshot. It fails when the live
count rises above 277 *and* when it drops below 277 without a re-pin. Every
move PR therefore re-pins the row DOWN and regenerates
`tools/golangci-lint-dirgate/grandfather.go` — see
[Restack rule](6642-query-move-sequence.md#restack-rule-the-dirgate-ledger-trap).

Four existing subpackages sit over the cap today behind a `//nolint:dirgate`
marker on their `doc.go` package line, and the issue's definition of done
requires those markers gone:

| directory | non-test files | marker |
| --- | ---: | --- |
| `querycontract` | 56 | no marker; tracked on #6597 |
| `repository` | 45 | `repository/doc.go:16` |
| `querytestutil` | 42 | `querytestutil/doc.go:57` |
| `codequery` | 41 | `codequery/doc.go:39` |

`codemodel`, `contract` and `impact` each sit at exactly 40 — legal, but with
zero headroom. Any file added to them fails the gate, which is worth knowing
before routing a new seam into one.

## Binding decisions

**1. No retained aliases.** The issue's own rule wins over the
`golang-engineering` skill's general "keep root callers compiling through a
compat surface" guidance. Twenty-one root `*_alias.go` files exist today; each
is listed in [The alias ledger](6642-query-move-cost.md#the-alias-ledger) with a disposition, and the
end state has none of them. A family move migrates its callers in the same
change.

**2. Move and rename together.** `docs/internal/naming.md` rule 5 makes the
rename part of the move. Every path below is de-stuttered: no file repeats its
directory's words, no directory repeats its parent's, and no glued compound
survives. `query/queryauth` fails the directory rule outright (a four-or-more
character parent matches as a prefix); `query/auth` passes.

**3. Receiver types are atomic.** A method cannot live in a different package
from its receiver. This is what makes Part B a big-bang and it is the single
hardest constraint in the tree — see
[The ContentReader problem](#the-contentreader-problem).

**4. Exported surface is preserved.** Per the issue's Scope section, every
exported symbol and route registration stays identical. Nothing below splits a
type, changes a signature, or renames an exported identifier.

## The tree

Files live in the leaves. A parent that holds no package of its own holds
nothing at all: `query/graph/` and `query/package/` are already empty
directories containing only their children, and `scripts/verify-package-docs.sh`
agrees — it requires the `doc.go`/`README.md`/`AGENTS.md` trio only of
directories that contain package source. The issue's definition of done says
"every new directory carries `doc.go`/`README.md`/`AGENTS.md`"; read against
the gate and the existing tree, that means every new directory **with Go code
in it**.

| parent | leaves (← current name) |
| --- | --- |
| `auth/` | ← `queryauth`, plus `route`, `session`, `signin`, `setup`, `acl` from root |
| `content/` | ← `contentread` (the handler), `read` (the `ContentReader` unit), `relationship` |
| `code/` | ← `codequery`, `model` ← `codemodel`, `shaping` ← `codeshaping`, `owners` ← `codeowners`, `divergence` ← `codedivergence`, `seam`, and `codequery`'s nine existing children |
| `contract/` | ← `querycontract`, split into `answer`, `code`, `entity`, `evidence`, `kubernetes`, `language`, `visualization`, `rowvalue` |
| `capability/` | the root capability handler, plus `capability_matrix.go` and `registry.go` from today's `contract/` |
| `infra/` | `aggregate`, `relationship`, `summary` |
| `cloud/` | `drift` |
| `investigation/` | `packet`, `workflow` |
| `supply/chain/` | `sbom` (new), `advisory`, `alerts`, `impact` (unchanged) |
| `image/` | `tag` ← `taghistory` |
| `graph/` | `read`, `entity`, `rows` (unchanged) |
| `repository/` | `artifacts` ← `repositoryartifacts`, `readmodel`, `seam` |
| `impact/` | `trace` ← `impacttrace`, `seam` |
| `entity/` | `semantics` ← `entitysemantics` |
| `semantic/` | `search` ← `semanticsearch`, `evidence` |
| `observability/` | `coverage` |
| `terraform/` | `drift` |
| new top-level leaves | `ask`, `cicd`, `collector`, `compare`, `dependency`, `documentation`, `evidence`, `kubernetes`, `metrics`, `status`, `workload` |
| renamed in place | `testutil` ← `querytestutil`, `selector` ← `queryselector`, `span` ← `tracing` |
| unchanged | `admin`, `decode`, `freshness`, `iac`, `incident`, `language`, `local`, `openapi`, `package/registry`, `playbook`, `secrets`, `service`, `visualization`, `workitem` |

### Part D renames, settled by the issue

| current | new | why |
| --- | --- | --- |
| `queryauth` | `auth` | naming.md rule 4: a four-or-more character parent matches as a prefix, so `query/queryauth` fails |
| `querycontract` | `contract` | same rule; the name frees itself as today's `contract/` empties — see [#6597](6642-query-move-cost.md#6597-and-the-contract-name-collision) |
| `querytestutil` | `testutil` | same rule |
| `queryselector` | `selector` | same rule |
| `codequery` | `code/` | reads as `query/codequery`; the issue folds it into `code/` |
| `repositoryartifacts` | `repository/artifacts` | rule 3: nest the compound, do not glue it |
| `codemodel`, `codeshaping`, `codeowners`, `codedivergence` | `code/{model,shaping,owners,divergence}` | rule 3 |
| `impacttrace`, `entitysemantics`, `semanticsearch`, `contentread` | `impact/trace`, `entity/semantics`, `semantic/search`, `content/` | rule 3 |
| `tracing` | `span` | the issue's Part D names `queryspan -> span`; `queryspan` has since been renamed `tracing`, which collides with the `handler_tracing.go` concern and with `go.opentelemetry.io` vocabulary |

`queryspan` no longer exists under that name. Treating `tracing` as its
successor is an inference, not a measurement — flagged in
[UNDECIDED](6642-query-move-sequence.md#undecided).

## The ContentReader problem

This is the hard constraint in the tree and it decides Part B.

Thirty-nine root non-test files declare at least one method on `ContentReader`.
Go requires a method's receiver type to be declared in the same package, so
those files are one indivisible unit. Filename prefixes badly understate it:
25 are named `content_reader_*`, and the other 14 arrive from six different
families — `cloud`, `documentation`, `evidence`, `repository`, `semantic` and
`service`. `cloud_inventory_read_model.go`, `documentation_read_model.go`,
`repository_relationship_read_model.go`, `evidence_read_model.go` and
`service_story_target_support.go` all hang methods off the same receiver.

The unit as measured is 41 files in one directory against a cap of 40:

| component | files |
| --- | ---: |
| root files declaring a `ContentReader` method | 39 |
| less `semantic_evidence.go`, which splits in two (below) | −1 |
| plus the `ContentReader` half of that split, as its own file | +1 |
| plus `content_entity_access_batch.go` and `content_reader_index_readiness.go` — no methods of their own, but helpers only this unit calls | +2 |
| **total in `content/read/`** | **41** |

It cannot be reduced by relocating, and splitting `ContentReader` into several
types would change the exported surface, which decision 4 forbids. The
remaining lever is merging files that already cover one concern. Four merges
take the leaf to 37 with real headroom, and every result stays well inside the
500-line file cap:

| merge | merged size | result |
| --- | ---: | --- |
| `entities_by_ids.go` + `entities_by_paths.go` | 139 | `content/read/entities.go` |
| `entity_search.go` + `entity_search_page.go` | 267 | `content/read/entity_search.go` |
| `repository_refs.go` + `repository_catalog.go` | 249 | `content/read/repository_catalog.go` |
| `index_readiness.go` + `coverage.go` | 134 | `content/read/coverage.go` |

Those are measured sizes after merging, not the sum of the inputs — deduplicated
headers and import blocks take roughly 14 lines off each pair. The largest
result is 267 lines against a 500-line cap.

**These merges preserve the exported surface exactly, and that is measured, not
argued.** In a throwaway worktree, `go doc -all ./internal/query` was captured
before and after performing all four merges:

```
diff doc-before.txt doc-after.txt        exit 0, zero lines of difference
go build ./internal/query/               exit 0
go test ./internal/query/ -run 'ContentReader|Entit|RepositoryCatalog|Coverage|IndexReadiness' -count=1
                                         ok  0.946s
```

The issue's Scope section requires that "every exported symbol and route
registration stays identical". An empty `go doc -all` diff is that requirement
satisfied, so the merges sit inside the stated scope rather than stretching it.
Which file a declaration lives in is not part of the surface Go exposes.

The minimum to clear the cap is one merge; four is the recommendation, because
landing a leaf at exactly 40 means the next file added to it fails CI. None of
the eight files carries a `//go:build` tag, so the merges are plain
concatenation with no tag hazard. It remains the one place in the tree where a
move is not a pure `git mv`, which is why it is called out here rather than
discovered in review.

### The one file that must be split before it moves

`semantic_evidence.go` (363 lines) declares methods on three receivers:
`ContentReader`, `SemanticEvidenceHandler`, and `semanticEvidenceFilter`. The
first belongs to `content/read/`, the other two to `semantic/evidence/`. It is
the only root file that straddles two destinations, and it is split in place —
into `semantic_evidence.go` and a new `semantic_evidence_read.go` — as the
first commit of whichever move PR reaches it first, so each half then moves as
an ordinary `git mv`.

## Where the prefix lies

The issue warned that filename prefixes misstate membership in this package.
The `go/ast` census found 20 root files whose prefix points at one family while
their declared methods pin them to another. Every one of these would have been
routed wrong by a `rg`-on-filename move.

| file | prefix suggests | receiver says | destination |
| --- | --- | --- | --- |
| `cloud_resources.go` | cloud | `InfraHandler` | `infra/` |
| `cloud_resources_metrics.go` | cloud | (helpers for `InfraHandler`) | `infra/` |
| `cloud_resources_params.go` | cloud | (helpers for `InfraHandler`) | `infra/` |
| `cloud_inventory_read_model.go` | cloud | `ContentReader` | `content/read/` |
| `cloud_inventory_rollout_signal.go` | cloud | `ContentReader` | `content/read/` |
| `documentation_read_model.go` | documentation | `ContentReader` | `content/read/` |
| `documentation_packet_read_model.go` | documentation | `ContentReader` | `content/read/` |
| `documentation_target_read_model.go` | documentation | `ContentReader` | `content/read/` |
| `documentation_source_only.go` | documentation | `ContentReader` | `content/read/` |
| `evidence_read_model.go` | evidence | `ContentReader` | `content/read/` |
| `repository_read_model_summary.go` | repository | `ContentReader` | `content/read/` |
| `repository_relationship_read_model.go` | repository | `ContentReader` | `content/read/` |
| `repository_deployment_evidence_read_model.go` | repository | `ContentReader` | `content/read/` |
| `repository_entry_points.go` | repository | `ContentReader` | `content/read/` |
| `service_story_target_support.go` | service | `ContentReader` | `content/read/` |
| `service_story_target_support_source_only.go` | service | `ContentReader` | `content/read/` |
| `admission_decisions.go` | admission | `EvidenceHandler` | `evidence/` |
| `investigation_packet_api_deployable.go` | investigation | `EvidenceHandler` | `evidence/` |
| `investigation_packet_api_drift.go` | investigation | `CloudRuntimeDriftHandler` | `cloud/drift/` |
| `investigation_packet_supply_chain.go` | investigation | `supplyChainImpactPacketResponder` | `supply/chain/` |

Two more are worth naming even though the prefix is not wrong, only uninformative:
`relationships_catalog.go` declares `InfraHandler` methods (so it goes to
`infra/relationship/`, not a top-level relationships package), and
`operator_control_plane.go` declares `StatusHandler` methods.

Four of the existing `//nolint:dirgate` markers at root already say this out
loud — `repository_relationship_read_model.go:4`,
`repository_entry_points.go:4`, `repository_deployment_evidence_read_model.go:4`
and `service_story_target_support.go:4` each record "methods on the root
`ContentReader` must live in package query". The `content/read/` leaf is what
retires all four.

## How much of the mapping the receiver test actually settles

The receiver test is decisive where it applies, and it applies to just over half
the tree. Of the 250 mapped files, **135 (54%) declare at least one method**;
the other 115 are free functions, types and constants that no receiver pins
anywhere. Those were placed from the cross-reference graph — which destination's
files actually call them — and, where that was also silent, from the name.

Twenty-nine destinations are settled mainly by receiver. Thirteen are not, and
those are where a move PR's own census is most likely to disagree with this
page:

| destination | files | declare a method | placed from |
| --- | ---: | ---: | --- |
| `auth/route` | 24 | 1 | the scoped-route registration cluster; free functions throughout |
| `content/relationship` | 9 | 1 | reference graph |
| `investigation/packet` | 8 | 0 | reference graph |
| `auth` | 8 | 2 | reference graph |
| `ask` | 6 | 2 | reference graph |
| `capability`, `compare` | 4 each | 1 each | reference graph |
| `repository/seam` | 3 | 0 | reference graph |
| `supply/chain` | 3 | 1 | reference graph |
| `code/seam`, `semantic` | 2 each | 0 | reference graph |
| `decode`, `semantic/evidence` | 1 each | 0 | reference graph |

`auth/route` is the one to watch: 24 files, one receiver between them. They are
a registration cluster, and the grouping is as good as the reference graph makes
it, not better. Like the reducer plan's triage roll, these rows are
**proposed here and confirmed by the move PR's own `go/types` census**; where
the census disagrees, the census wins and this page is amended.

## Build-tagged files

One non-test root file is behind a build tag: `entity_alias_live.go`
(`//go:build` plus a `//nolint:dirgate` marker saying the staying root live
comparison test is its only caller). Twenty-four root `*_test.go` files are
tagged, across eleven distinct constraints:

```
integration (9), live_nornicdb_language_imports_grant (5),
live_infra_scope_shape (2), queryplan_profile_live, live_postgres_language_zero_match_plan,
live_postgres_language_grant_plan, live_nornicdb_label_predicates,
live_nornicdb_label_predicate_timing, live_nornicdb_answer_truth,
live_global_name_comparison, graph_entity_inventory_slo_live
```

`go build`, `go vet` and `go test ./...` all skip these silently. Any move PR
touching a tagged file greps its constraint name across `.github/workflows`,
`Makefile` and `scripts/` before trusting a green default run — the incident
this rule comes from is recorded in
`go/internal/query/cloud_inventory_account_alias_live_test.go`.

## Per-directory arithmetic

Every directory after the plan, with its non-test file count. Three land at
exactly 40 and have zero headroom — the next file added to any of them fails
CI. `repository` is the one directory this plan does not bring under the cap;
the measurement and the reason are in
[what the moves cost](6642-query-move-cost.md#the-other-three-over-cap-directories),
and the decision is [UNDECIDED](6642-query-move-sequence.md#undecided).

| directory | non-test files |
| --- | ---: |
| `query/ (root)` | 5 |
| `query/admin` | 9 |
| `query/admin/audit` | 3 |
| `query/admin/identity` | 6 |
| `query/admin/provider/config` | 7 |
| `query/admin/store` | 6 |
| `query/ask` | 7 |
| `query/auth` | 16 |
| `query/auth/acl` | 2 |
| `query/auth/route` | 24 |
| `query/auth/session` | 4 |
| `query/auth/setup` | 5 |
| `query/auth/signin` | 9 |
| `query/capability` | 10 |
| `query/cicd` | 6 |
| `query/cloud` | 6 |
| `query/cloud/drift` | 7 |
| `query/code` | 40 **← at cap** |
| `query/code/chain` | 5 |
| `query/code/deadcode` | 13 |
| `query/code/divergence` | 10 |
| `query/code/imports` | 4 |
| `query/code/metrics` | 3 |
| `query/code/model` | 40 **← at cap** |
| `query/code/owners` | 7 |
| `query/code/quality` | 4 |
| `query/code/relationships` | 5 |
| `query/code/relationships/story` | 6 |
| `query/code/routes` | 4 |
| `query/code/seam` | 2 |
| `query/code/search` | 3 |
| `query/code/shaping` | 4 |
| `query/code/visualization` | 2 |
| `query/collector` | 11 |
| `query/compare` | 4 |
| `query/content` | 6 |
| `query/content/read` | 37 |
| `query/content/relationship` | 9 |
| `query/contract` | 37 |
| `query/contract/answer` | 3 |
| `query/contract/code` | 2 |
| `query/contract/entity` | 3 |
| `query/contract/evidence` | 3 |
| `query/contract/kubernetes` | 2 |
| `query/contract/language` | 4 |
| `query/contract/rowvalue` | 2 |
| `query/contract/visualization` | 2 |
| `query/decode` | 3 |
| `query/dependency` | 2 |
| `query/documentation` | 8 |
| `query/entity` | 29 |
| `query/entity/semantics` | 6 |
| `query/evidence` | 11 |
| `query/freshness` | 11 |
| `query/graph/entity` | 2 |
| `query/graph/read` | 3 |
| `query/graph/rows` | 2 |
| `query/iac` | 36 |
| `query/image` | 6 |
| `query/image/tag` | 7 |
| `query/impact` | 40 **← at cap** |
| `query/impact/seam` | 5 |
| `query/impact/trace` | 28 |
| `query/incident` | 5 |
| `query/incident/model` | 3 |
| `query/incident/sql` | 5 |
| `query/incident/store` | 14 |
| `query/infra` | 10 |
| `query/infra/aggregate` | 5 |
| `query/infra/relationship` | 5 |
| `query/infra/summary` | 3 |
| `query/investigation/packet` | 8 |
| `query/investigation/workflow` | 3 |
| `query/kubernetes` | 4 |
| `query/language` | 11 |
| `query/local` | 10 |
| `query/metrics` | 4 |
| `query/observability/coverage` | 3 |
| `query/openapi` | 10 |
| `query/openapi/paths/auth` | 9 |
| `query/openapi/paths/catalog` | 5 |
| `query/openapi/paths/cicd` | 3 |
| `query/openapi/paths/cloud` | 5 |
| `query/openapi/paths/code` | 10 |
| `query/openapi/paths/code/dead` | 4 |
| `query/openapi/paths/evidence` | 9 |
| `query/openapi/paths/freshness` | 5 |
| `query/openapi/paths/iac` | 8 |
| `query/openapi/paths/impact` | 7 |
| `query/openapi/paths/infrastructure` | 6 |
| `query/openapi/paths/repository` | 8 |
| `query/openapi/paths/search` | 7 |
| `query/openapi/paths/service` | 3 |
| `query/openapi/paths/status` | 14 |
| `query/openapi/paths/supply` | 1 |
| `query/openapi/paths/supply/chain` | 18 |
| `query/openapi/schema` | 3 |
| `query/package/registry` | 19 |
| `query/playbook` | 9 |
| `query/repository` | 45 **← OVER, see UNDECIDED** |
| `query/repository/artifacts` | 20 |
| `query/repository/readmodel` | 5 |
| `query/repository/seam` | 3 |
| `query/secrets` | 11 |
| `query/selector` | 3 |
| `query/semantic` | 2 |
| `query/semantic/evidence` | 3 |
| `query/semantic/search` | 17 |
| `query/service` | 27 |
| `query/service/evidence` | 2 |
| `query/span` | 3 |
| `query/status` | 18 |
| `query/supply` | 1 |
| `query/supply/chain` | 38 |
| `query/supply/chain/advisory` | 10 |
| `query/supply/chain/alerts` | 6 |
| `query/supply/chain/impact` | 37 |
| `query/supply/chain/sbom` | 4 |
| `query/terraform/drift` | 2 |
| `query/testutil` | 24 |
| `query/testutil/content` | 10 |
| `query/testutil/graph` | 8 |
| `query/visualization` | 7 |
| `query/workitem` | 13 |
| `query/workload` | 1 |

125 directories, 1184 non-test files.
At the cap with zero headroom: `query/code`, `query/code/model`, `query/impact`.
Over the cap: `query/repository` (45).
