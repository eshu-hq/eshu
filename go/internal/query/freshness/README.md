# Freshness

## Purpose

The bounded freshness-drilldown routes: the generation lifecycle drilldown
(`GET /api/v0/freshness/generations`), the changed-since delta summary
(`GET /api/v0/freshness/changed-since`), and the service-scope changed-since
delta summary (`GET /api/v0/freshness/services/changed-since`, #1943). It also
owns the freshness-causality read model's closed cause enumeration and the
generation/pending-projection/transition projection types the root
`GET /api/v0/status/freshness-causality` route renders.

## Ownership boundary

Owns the `Handler` struct, its three routes' HTTP dispatch, the bounded
changed-since/generations/service-changed-since reader ports, and the
causality cause/projection types and helpers. Does not own the capability
registry or truth-envelope contract (`querycontract`), the caller grant
resolution (`querycontract.RepositoryAccessFilterFromContext`), the
service-catalog correlation read model the service-changed-since route's
grant binds against (`service`), or the Postgres status store that implements
every reader port (`internal/status`) -- those are separate homes this
package calls into. Does not own `StatusHandler.getFreshnessCausality`
(package query, `status_freshness_causality.go`): its receiver is declared in
`status.go` and `scopedFreshnessCausalityRoute` is read by
`auth_scoped_routes.go`, so it moves with the status family in a later #6642
lane; it calls into this package through `freshness_alias.go`'s
`freshnessCausalityFromRawAndReport` and `freshnessNextCheckAsRecommendedCall`
forwarders.

## Layout

- `generations.go` -- `Handler`, `Mount`, `profile`, `listGenerationLifecycle`
  and its parsing/not-found helpers, the `GenerationLifecycleReader` port, and
  `generationLifecycleRoute`; `Mount` registers all three routes here
  because `Handler` is declared in this file, and each route constant sits
  beside its own handler file (`changedSinceRoute` in `changed_since.go`,
  `serviceChangedSinceRoute` in `service_changed_since.go`).
- `changed_since.go` -- `listChangedSince` and its parsing/truth-envelope/
  not-found helpers, the `ChangedSinceReader` port.
- `service_changed_since.go` -- `listServiceChangedSince`, the #5167 grant
  binding (`serviceChangedSinceGrantAdmits`, `refuseServiceChangedSinceGrant`),
  and the `ServiceChangedSinceReader` port.
- `causality.go` -- `Cause`, `NextCheck`, the eight `Cause*` constants,
  `ValidCause`, `CauseNextCheck`, `WithCause`, and
  `NextCheckAsRecommendedCall` -- all thin aliases/forwarders to
  `querycontract`'s closed enumeration (see AGENTS.md for why this layer
  exists instead of a direct `querycontract` import everywhere).
- `causality_report.go` -- `Causality`, `CauseStatus`, `Generations`,
  `PendingProjection`, `Transition`, and the pure-projection builders
  (`CausalityFromRawAndReport`, `freshnessCausalityFromReport`,
  `freshnessState`, `buildFreshnessCauseStatuses`, ...) that turn a
  `status.Report`/`status.RawSnapshot` into the causality read model with no
  I/O.
- `capabilities.go` -- `ChangedSinceCapability`, `GenerationLifecycleCapability`,
  `ServiceChangedSinceCapability` and their `*Support()` constructors (see
  AGENTS.md).
- `handler_tracing.go` -- the route's own span seam (`freshnessHandlerTracer`,
  `startQueryHandlerSpan`), the same shape the other leaves keep in their
  `handler_tracing.go`.
- `main_test.go` -- `TestMain`, registering this family's three capabilities
  before any test runs (see AGENTS.md).
- Test files -- this package's own tests, several moved in verbatim from
  root (see Move evidence); `changed_since_two_tenant_test.go` and
  `generations_two_tenant_test.go` construct fixtures hoisted to
  `querytestutil` (see AGENTS.md).

## Move evidence

The family moved here from the query root (`freshness_changed_since.go`,
`freshness_generations.go`, `freshness_service_changed_since.go`,
`freshness_causality.go`, and `freshness_causality_report.go`, #6642, split
off the #6060 lane A restructure); the non-test files are destuttered
(`freshness_changed_since.go` -> `changed_since.go`, etc.) and
`FreshnessHandler` renamed to `Handler` at its declaration, with every
`Freshness`-prefixed exported identifier losing the stutter (`FreshnessCause`
-> `Cause`, `FreshnessCauseReducerBacklog` -> `CauseReducerBacklog`,
`WithFreshnessCause` -> `WithCause`, `FreshnessCausality` -> `Causality`, and
so on) and every method/field/unexported-helper name otherwise unchanged.
Root keeps every pre-move exported spelling through a stanza in the new
`freshness_alias.go`: the `FreshnessHandler`/`FreshnessCause`/
`FreshnessNextCheck`/`FreshnessCausality`/`FreshnessCauseStatus`/
`FreshnessGenerations`/`FreshnessPendingProjection`/`FreshnessTransition`
type aliases, the `GenerationLifecycleReader`/`ChangedSinceReader`/
`ServiceChangedSinceReader` port aliases cmd/api's and cmd/mcp-server's
wiring need, all eight `FreshnessCause*` const aliases, and the
`WithFreshnessCause`/`ValidFreshnessCause`/`FreshnessCauseNextCheck`/
`freshnessCausalityFromRawAndReport`/`freshnessNextCheckAsRecommendedCall`
forwarders.

`freshness_causality_handler.go` stayed in root (renamed
`status_freshness_causality.go` to clear the new `freshness/` directory's
naming-rule sibling-prefix check): `getFreshnessCausality` is a
`StatusHandler` method whose receiver is declared in `status.go`, and
`scopedFreshnessCausalityRoute` is read by `auth_scoped_routes.go`; it moves
with the status family in a later lane. Its helper functions
(`freshnessCausalityToMap`, `freshnessCausesToSlice`,
`freshnessGenerationsToMap`, `freshnessPendingProjectionToMap`,
`freshnessTransitionsToSlice`, `freshnessCausalityTruth`) already lived in
that file and stayed with it unchanged.

Capability registration: root's `contract_changed_since.go`,
`contract_freshness.go`, and `contract_service_changed_since.go` (#6642 Part
C, not moved by this lane) define the three capability constants this
family's handlers read and register them into root's `capabilityMatrix` via
literal `init()` blocks. This leaf declares its own exported copies with the
same string values in `capabilities.go` plus three `*Support()`
constructors, registered by `main_test.go`'s `TestMain` so this package's own
test binary (which cannot import root without an import cycle) exercises the
same capability gate production does. Root's
`capability_lockstep_freshness_test.go` (package query) asserts, field by
field, that root's three literal rows equal this leaf's three constructors,
so the two copies of the same contract cannot drift apart silently; a
follow-up lane should point root's rows at these constructors directly, the
way root's hardcoded-secret registration already calls
`querycontract.HardcodedSecretSupport`.

Two fixtures this package's two-tenant grant-boundary tests share with
package query's #6450 residual all-scope-bearer boundary test
(`auth_all_scope_bearer_two_tenant_test.go`) moved to `querytestutil` instead
of being duplicated (#6608 rule): `querytestutil.GrantMirroringChangedSince`/
`TwoTenantChangedSinceScopes`/`ChangedSinceTwoTenantPriorGeneration` and
`querytestutil.GrantMirroringGenerations`/`TwoTenantGenerationRows`, plus
`querytestutil.ScopedChangedSinceTenantA` and
`querytestutil.DecodeChangedSinceEnvelope`. See AGENTS.md.

One test moved rather than staying, correcting the original move brief: the
grant-refusal span-attribute proof
(`TestServiceChangedSinceGrantRefusalIsRecordedOnTheSpan`,
`TestServiceChangedSinceGrantRefusalReasonsAreAClosedVocabulary`) was
expected to stay in root's `service_changed_since_telemetry_test.go` because
it swapped `queryHandlerTracer`, a root-unexported symbol. But
`handler_tracing.go`'s tracer seam is deliberately package-local (see
AGENTS.md) -- once `listServiceChangedSince` moved here, swapping root's var
no longer observed any span this route emits, and the test failed with
"ended spans = 0, want 1". It moved to this package's own
`service_changed_since_telemetry_test.go`, swapping `freshnessHandlerTracer`
instead, with its own minimal fakes rather than reusing
`querytestutil`'s SQL-mirroring two-tenant correlation fixture (that
correctness proof stays where it was, in root's
`service_changed_since_grant_test.go` -- this proof only needs to land on
each of the four closed refusal reasons, not re-derive the grant
intersection).

No-Regression Evidence: the `go test ./internal/query/...` and
`go test ./internal/query/freshness/` test-name union (`-list '.*'`) equals
main's 2777 root names (`origin/main` 43c0606ca) plus exactly
`TestFreshnessCapabilityLockstep`, nothing dropped or duplicated (2778
total). The `internal/query` dirgate ledger row
(`scripts/lib/dirgate-grandfather.tsv`) moved from 479 non-test files to 475
(five files left root, one alias file arrived); `bash
scripts/verify-dirgate.sh --all` passes with no exemption row for this new
leaf directory.

No-Observability-Change: every route keeps its span name
(`telemetry.SpanQueryFreshnessChangedSince`,
`telemetry.SpanQueryFreshnessGenerationLifecycle`,
`telemetry.SpanQueryFreshnessServiceChangedSince`) and
`http.route`/`eshu.capability` attributes. `freshnessHandlerTracer` is this
package's own package-local tracer var (mirroring `languageHandlerTracer` in
`go/internal/query/language/handler_tracing.go`), seeded from
`queryspan.HandlerTracer()`; `service_changed_since_telemetry_test.go` (moved
here for the reason above) proves the handler still emits exactly one span
per request with the documented attributes, including the
`eshu.service_changed_since.grant_refused`/`grant_refused_reason` pair.

## Related docs

- [docs/public/reference/incremental-freshness-model.md](../../../../docs/public/reference/incremental-freshness-model.md)
- [docs/public/reference/truth-label-protocol.md](../../../../docs/public/reference/truth-label-protocol.md)
- [go/internal/query/read-models.md](../read-models.md)
