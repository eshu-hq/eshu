# Read-Surface Consumer-Existence Gate

Blocking, credential-free, Docker-free CI gate for issue
[#5335](https://github.com/eshu-hq/eshu/issues/5335). It targets the epic's
dominant defect class: a ledger row or a query claiming a capability with no
real consumer behind it — a typo'd label, a renamed tool, or a UNION branch
naming a relationship type nobody ever writes. All three halves ride the
existing Go package test floor (`go test ./internal/mcp ./internal/query
./internal/replaycoverage ./cmd/api`); there is no separate workflow.

## GATE 1 — read-surface consumer existence

Two ledgers claim read surfaces. Historically both only checked
non-emptiness, not that the claim resolves to something real.

### Language-parity ledger

`specs/language-feature-parity-ledger.v1.yaml` `read_surfaces` lists abstract
labels (`execute_language_query`, `entity_context`, `content_relationships`,
`find_dead_code`, `get_code_relationship_story`, `list_relationship_edges`,
`trace_deployment_chain`, `trace_resource_to_code`, `trace_route_callers`).
`go/internal/replaycoverage/languageledger.go`'s `LoadLanguageLedger` parses
the field into `LanguageLedgerEntry.ReadSurfaces`.

`go/internal/mcp/read_surface_consumer_existence.go` defines a closed,
hand-maintained backing map (`languageParityReadSurfaceBacking`) from each
label to a live artifact:

- **`mcp_tool`** — the label (or its alias) must be a `tool.Name` in
  `ReadOnlyTools()`. Six of the nine labels are literal MCP dispatch case
  strings (`dispatch.go`/`dispatch_impact.go`) and are also registered tool
  names, so the ref equals the label. `list_relationship_edges` is a seventh
  label that equals its tool name directly, routed through its own dispatch
  function (`dispatch_relationship_edges.go`) rather than the shared
  case-string switch. `entity_context` aliases to the `get_entity_context`
  tool.
- **`go_symbol`** — `content_relationships` aliases to the unexported
  `query.buildContentRelationshipSet` symbol. `query.ReadSurfaceGoSymbolBackings`
  (`go/internal/query/content_relationships_read_surface_backing.go`) holds a
  direct compile-time reference to that symbol, so a rename or removal fails
  `go build` package-wide, not just a test.
- **`api_route`** — reserved for a future label; no language-parity label
  uses it today.

`TestLanguageParityReadSurfacesResolveToRealConsumers` fails for any label
not in the map, and for any label whose ref does not resolve. It also checks
the reverse direction: a backing-map entry no ledger row's `read_surfaces`
uses anymore is stale and fails
(`assertLanguageParityBackingNotStale`) — scoped to the nine-label backing
map, not the full universe of `ReadOnlyTools()`/served routes.

### Fact-kind registry

`specs/fact-kind-registry.v1.yaml` `read_surface` (family-level field only —
`read_surface_overrides` is out of scope for v1) names 17 distinct literal
`"METHOD /path"` routes. `TestFactKindRegistryReadSurfacesResolveToLiveRoutes`
matches each against the live route inventory
(`capabilitycatalog.LoadSurfaceInventory`, category `api_route`, readiness
`implemented`) with positional `{param}` wildcard matching: method, segment
count, and every literal segment must match; a `{param}` segment on either
side matches positionally regardless of its name (so
`GET /api/v0/incidents/{id}/context` and
`GET /api/v0/incidents/{incident_id}/context` are the same route).

### Mounted-route parity (fact-kind registry)

`TestFactKindRegistryReadSurfacesResolveToLiveRoutes` (above) only proves a
fact-kind read_surface literal is *documented* — it matches
`capabilitycatalog.LoadSurfaceInventory`, the OpenAPI-derived inventory
generated from the served spec (`query.OpenAPISpec()` by way of
`cmd/capability-inventory`'s `enumerateAPIRoutes`). `verify-openapi.sh` keeps
that spec in parity with `HandleFunc` *declarations* in `openapi_paths_*.go`
source files, not with what production wiring actually mounts on the API
router's `*http.ServeMux` — a route can be declared (and so documented) while
the handler that would serve it is never assigned onto `query.APIRouter`, in
which case `APIRouter.Mount` silently skips it and a caller following the
documented route gets a live 404. That gap was a #5359 codex-review P1 finding
against this gate's own domain: an advertised-but-unservable route would pass
GATE 1 as long as it stayed in the spec.

`go/cmd/api/fact_kind_mounted_route_gate.go` closes it with a second,
independent check: `TestFactKindReadSurfacesAreActuallyMountedOnRealRouter`
(`go/cmd/api/fact_kind_mounted_route_gate_test.go`) builds the real production
router (`newFullyWiredTestRouter`, the same construction
`TestNewRouterWiresEveryFieldOrDocumentsWhyNot` uses), mounts it onto a real
`*http.ServeMux`, and for every fact-kind read_surface literal asks the mux
itself — via the stdlib's own `(*http.ServeMux).Handler(req)` — whether a
synthetic request for that route resolves to a registered pattern. An empty
returned pattern is conclusive: the route is not being served, regardless of
what the spec says. `TestFactKindMountedRouteGateCatchesDocumentedButUnmountedRoute`
is the regression test that proves this check actually discriminates
documented-but-unmounted routes from genuinely live ones, by deliberately
nil-ing `router.CICD` and confirming the gate fails for
its one owned route while the documented inventory would still call it
implemented.

This test rides `go test ./cmd/api/...`, the same credential-free floor as
every other Go package test — no separate workflow.

**Residual scope limit:** `newFullyWiredTestRouter` wires everything
`newRouter` wires, but not the two routes `wireAPI`
(`cmd/api/wiring.go`) mounts directly onto the outer `apiMux` outside
`APIRouter.Mount` (`POST /api/v0/ask`, the `serviceintelhttp.ReportHandler`
report route) — see `routerFieldsNotWiredByNewRouter`'s `"Ask"` entry. No
fact-kind read_surface names either route today, so this does not currently
narrow the gate's coverage; a future read_surface pointed at one of those two
routes would need the test's router construction extended to mount them too.

### Grandfathering

`go/internal/mcp/read_surface_grandfather.go` mirrors
`go/internal/queryplan/grandfathered_non_hot.go`'s landing mechanism: a
closed map from `"<language>:<label>"` (or family name) to a digest of the
row's exact claim. Editing the row changes the digest and un-grandfathers it.
Both maps are empty — every claim in both ledgers resolves to a real
consumer today.

## GATE 2 — scoped edge-materialization gate

`go/internal/query/impact_edge_materialization_gate.go` audits the
target_type-scoped blast-radius Cypher constants in
`go/internal/query/impact_blast_radius.go` (the six queries feeding
`blastRadiusAffected`'s switch, plus the shared tier-lookup query).

For each, `extractRelationshipTypeTokens` tokenizes every relationship-type
name the Cypher's bracket patterns name: `-[:A]->`, `<-[:A]-`, `-[r:A]->`
(bound variable), `-[:A|B|C]->` (pipe union, split into `A`, `B`, `C`), and
`-[:A*1..3]->` (variable-length; the quantifier is outside the identifier
character class, so it never pollutes the captured type). Node labels
(`(n:Label)`, parenthesized) are out of scope for v1 — only relationship
types are extracted.

Each extracted token must be disclosed rather than silent:

1. present in that query's own coverage-edge-type list
   (`sqlTableBlastRadiusEdgeTypes`, `crossplaneXrdBlastRadiusEdgeTypes` —
   disclosed via the API response's `coverage`/`complete` fields either way),
2. or genuinely materialized per `EdgeMaterializationCoverage`,
3. or explicitly annotated in `unmaterializedAnnotatedImpactEdgeTypes`
   (empty today).

The invariant is **"no edge is traversed silently,"** not "every edge has a
writer" — an annotated, disclosed, unwritten edge (`SATISFIED_BY` today)
passes. `DEPENDS_ON` and `REPO_CONTAINS`, traversed by queries with no
per-query coverage list at all (`repository`, `terraform_module`), are
registered in `EdgeMaterializationCoverage`'s registry
(`structuralEdgeTypes` in `edge_materialization_coverage.go`) with citations
to their real writers, since there is no coverage/complete field to disclose
a gap for those target types.

Bidirectional: `TestImpactBlastRadiusCoverageEdgeTypesAreStillTraversed`
fails a coverage-edge-type list entry that is neither traversed by any
audited query nor a registered relationship type
(`internal/graph/edgetype.IsRegistered`) — distinguishing the deliberate
`sql_table` honesty pattern (`REFERENCES_TABLE`, `MAPS_TO_TABLE` are listed as
conceptually covered even though no UNION branch queries them, so the
response can disclose the gap) from a genuinely stale or fake entry. `MIGRATES`
left this list in #5346: it now has its own UNION branch and a real writer.

Two anti-false-green mitigations:

- **Positive-extraction floor** — each query has a `MinDistinctEdgeTypes`
  seeded from its current known token count (repository: 1,
  terraform-source-repos: 2, dependents-by-id: 1, crossplane: 3, sql_table:
  7, tier-lookup: 1), so a tokenizer regression that silently drops tokens
  fails the floor instead of vacuously passing.
- **Literal-only discipline** — `TestImpactBlastRadiusGateQueriesAreLiteralConstants`
  AST-parses `impact_blast_radius.go` and requires every audited query to be
  declared as a single string-literal `const` (Go's own const semantics
  already forbid a non-constant expression like `fmt.Sprintf` there). A
  tracked name missing from a literal const decl fails with "restructure or
  extend the gate" instead of silently tokenizing stale or composed Cypher.

### Scope limits

- Only the Cypher constants in `impact_blast_radius.go` that feed
  `blastRadiusAffected`'s switch are audited — not every "impact"-named
  query in the package (`impact.go`'s dependency-path explainer,
  `exposure_path.go`, and similar are out of v1 scope).
- Node labels are not extracted or checked, only relationship types.

## Related gates

- [C-1/C-8/C-9/C-10 replay coverage manifest](local-testing.md) — checks the
  same two ledgers for non-emptiness and replay-scenario coverage; this gate
  is the consumer-existence check the replay-coverage gate does not perform.
- [Cypher Performance](cypher-performance.md) — hot-path Cypher discipline.
- `go/internal/queryplan/grandfathered_non_hot.go` — the grandfather landing
  mechanism this gate mirrors.

## GATE 3 — route-serves-data consistency (#5474 D1)

`go/internal/mcp/read_surface_route_serves_data.go` adds a new gate that
closes the #5480 defect class: a registry family's `read_surface` route can be
live and mounted (so the #5359 gate stays green) while serving data from an
entirely different reducer domain — the `kubernetes_live` → `cloud/resources`
mis-mapping is the canonical example.

### Backing map

`routeServesDataBackingMap` is a closed, hand-maintained, digest-pinned map
from each distinct `read_surface` route literal to the `reducer_domain`
values whose data that route surfaces. It covers all 17 distinct route
literals the registry uses today. A route missing from the map fails closed.

Entry discipline:
- **Add an entry** for every distinct `read_surface` the registry uses.
- **ServedDomains** lists every `reducer_domain` whose produced data is
  surfaced through that route. When a new family shares an existing route,
  add its `reducer_domain` here.
- **read_surface_overrides** (per-kind route substitutions) are excluded from
  v1 scope. A family that uses overrides passes as long as its family-level
  route is consistent.

### Consistency rule

For each family in `specs/fact-kind-registry.v1.yaml`, the gate asserts:

> The family's `reducer_domain` must be in the route's `ServedDomains`.

A failure names BOTH fix paths:

1. Fix `specs/fact-kind-registry.v1.yaml`'s `read_surface` (point the family
   at the correct route).
2. Fix `routeServesDataBackingMap` (add the missing `reducer_domain` to the
   route's `ServedDomains`).

### BITES proof

`TestRouteServesDataBITES_KubernetesLiveCloudResourcesMismatch`
(`go/internal/mcp/read_surface_route_serves_data_test.go`) reproduces the
#5480 defect class on purpose:

1. **Baseline-green:** `kubernetes_live`'s real route
   (`GET /api/v0/kubernetes/correlations`, serves `kubernetes_correlation`)
   passes.
2. **Seeded-RED:** Re-point `kubernetes_live` at `GET /api/v0/cloud/resources`
   — a live, mounted route that serves `CloudResource` nodes — and assert the
   gate goes RED with a message naming both fix paths.
3. **Production stays GREEN:** The backing map's actual entry for
   `kubernetes/correlations` still includes `kubernetes_correlation`.

Follows the baseline-green-then-break pattern from the #5359 BITES precedent.

### Mount

Rides `go test ./internal/mcp`, the same credential-free floor as every other
package test. No separate workflow.

## GATE 4 — per-kind consumer existence (#5474 D2)

`go/internal/mcp/kind_disclosure_ledger.go` and
`go/internal/mcp/kind_consumer_existence_test.go` add a new gate walking every
fact kind in the generated registry (`facts.FactKindRegistry()`) and asserting
each kind either has a detectable consumer or an explicit disclosure entry.

### Consumer taxonomy (v1)

A kind passes if it has at least one of these signals:

1. **PayloadSchema non-empty** — the kind has a typed decode seam (a
   `sdk/go/factschema` struct + decode wrapper, consumed at the reducer
   level).
2. **AdmissionExempt** — legacy code kinds (`file`, `repository`) are
   deliberately outside the versioned-admission regime but still consumed.
3. **Pipeline consumer** — the kind has a non-empty `ReducerDomain`,
   `ProjectionHook`, and a real (non-`"none"`) `AdmissionHook`, meaning it
   flows through the full admission→projection→read pipeline and is consumed
   at the pre-typing raw-payload level.
4. **Disclosure ledger** — the kind is pinned in
   `grandfatheredUnconsumedKinds` with a code-anchor citation. This covers
   kinds that are legitimately registered but intentionally unconsumed (e.g.
   `terraform_state_candidate`, consumed by the projector but not by the
   reducer's decode seam; `package_registry.vulnerability_hint`, join-key-only
   with no decode-side consumer).

### Disclosure ledger

`kind_disclosure_ledger.go` mirrors `read_surface_grandfather.go`: a closed
map from exact fact kind to a SHA-256 digest of `family:kind:reason`. Changing
the kind, family, or disclosure reason changes the digest and un-grandfathers
the entry, forcing re-evaluation.

Seed entries (from #5475):
- `terraform_state_candidate`, `_provider_binding`, `_warning` — intentionally
  not consumed by the reducer decode seam
  (`projector/tfstate_canonical.go:104-106`).
- `package_registry.vulnerability_hint` — join-key-only evidence; no reducer
  decode seam or query read-model consumer.
- Five `service_catalog` kinds (`api_link`, `dependency`, `scorecard_definition`,
  `scorecard_result`, `warning`) — no decode-side consumer today.
- `vulnerability.warning` — no reducer decode, no query reader (#5462 owns).
- Three `ci_cd_run` kinds (`ci.job`, `ci.pipeline_definition`, `ci.warning`) —
  emitted by collector, no reducer decode call (Wave 4d deferred).

### Fail-closed enumeration

`TestEveryRegistryKindHasConsumerOrDisclosure` walks all 176 fact kinds in the
generated registry. A NEW kind with no consumer and no disclosure entry fails
the gate — this is the point. The RED message names the kind, the family
(`reducer_domain`), and the three legal exits:

1. Add a consumer (typed decode seam, reducer handler, or query read model).
2. Add the kind to `grandfatheredUnconsumedKinds` with code-anchor evidence.
3. Remove the kind from `specs/fact-kind-registry.v1.yaml`.

### BITES proof

`TestKindConsumerExistenceBITES_UnconsumedKindMustFail` constructs a synthetic
kind with no `PayloadSchema`, not `AdmissionExempt`, no pipeline consumer, and
not in the disclosure ledger → the gate goes RED with a message naming all
three exits. A disclosed kind (`terraform_state_candidate`) is proven to pass
via the disclosure path.

### Mount

Rides `go test ./internal/mcp`, the same credential-free floor as every other
package test. No separate workflow.

## Related gates

- [C-1/C-8/C-9/C-10 replay coverage manifest](local-testing.md) — checks the
  same two ledgers for non-emptiness and replay-scenario coverage; this gate
  is the consumer-existence check the replay-coverage gate does not perform.
- [Cypher Performance](cypher-performance.md) — hot-path Cypher discipline.
- `go/internal/queryplan/grandfathered_non_hot.go` — the grandfather landing
  mechanism this gate mirrors.
