# Freshness — Agent Instructions

Scope: `go/internal/query/freshness/` (package `freshness`).

## Ownership

This leaf owns the three freshness-drilldown routes (#6642): `generations.go`
(`Handler`, `Mount`, `listGenerationLifecycle`), `changed_since.go`
(`listChangedSince`), `service_changed_since.go` (`listServiceChangedSince`
and its #5167 grant binding), `causality.go` and `causality_report.go` (the
closed cause enumeration and the pure-projection causality read model),
`capabilities.go` (the three capability constants and their `*Support()`
constructors), `handler_tracing.go` (the span seam), `main_test.go` (the
capability-registration `TestMain`), plus this package's own tests, several
moved in verbatim from root -- see README.md's Move evidence.

## Invariants

- MUST NOT import root package `query` -- root would import this package
  back for the compatibility aliases in `freshness_alias.go`, cycling. Reach
  root-only helpers through `querycontract` (profiles, envelopes,
  capability registration, HTTP helpers, `RepositoryAccessFilterFromContext`),
  `queryauth` (`AuthContext`, tests only), `service`
  (`CatalogCorrelationStore`/`Filter`/`Row`), `querytestutil` (shared
  two-tenant test fixtures), or `queryspan` (the shared handler-span seam);
  if none of those has what you need, it does not belong here -- ask before
  adding a new shared home.
- `ChangedSinceCapability`/`GenerationLifecycleCapability`/
  `ServiceChangedSinceCapability` MUST stay `"freshness.changed_since"` /
  `"freshness.generation_lifecycle"` / `"freshness.service_changed_since"` --
  byte-identical to root's `contract_changed_since.go` /
  `contract_freshness.go` / `contract_service_changed_since.go` constants.
  `capability_lockstep_freshness_test.go` (root, package query) pins the
  string equality and every `*Support()` ceiling field by field; it is the
  only thing that would catch a one-sided edit today, since root's three
  `init()` rows are still a literal copy, not a call into these constructors.
- `main_test.go`'s `TestMain` registering the three capabilities through
  `ChangedSinceSupport()`/`GenerationLifecycleSupport()`/
  `ServiceChangedSinceSupport()` is NOT redundant with root's `init()`
  registrations: this package's own test binary never links root (an import
  would cycle through `freshness_alias.go`), so root's `init()` functions
  never run here. Delete `TestMain` and every handler test in this package
  fails the capability gate's `unsupported_capability` 501 -- not because the
  handler is broken, but because no capability was ever registered for it to
  check against. Follow the `codeowners`/`supplychain` precedent, not the
  `packagereg` one: call the `*Support()` constructors, never copy their
  field values into a second literal.
- `freshnessHandlerTracer` is this package's own tracer var (the same seam
  `languageHandlerTracer` in `go/internal/query/language/handler_tracing.go`
  and `incidentHandlerTracer` in
  `go/internal/query/incident/handler.go` use): package-local so a recording-
  provider swap in a test stays private to this package. Do not promote it
  to an exported var or move the span helper back to root. A test outside
  this package (root's old `freshness_service_changed_since_telemetry_test.go`) that
  swapped root's `queryHandlerTracer` expecting it to observe this package's
  spans silently measured zero spans -- see README.md's Move evidence for
  why that test now lives here instead.
- `Cause`/`NextCheck`/the eight `Cause*` constants/`ValidCause`/
  `CauseNextCheck`/`WithCause` in `causality.go` are themselves thin
  aliases/forwarders to `querycontract`'s closed enumeration
  (`querycontract/freshness.go` is the one canonical declaration of the
  cause set and the cause-to-next-check mapping). This layer exists only
  because it moved here verbatim from root's own alias file of the same
  shape (#6060 predates #6642); do not add new behavior here that belongs in
  `querycontract` instead.
- `NextCheckAsRecommendedCall` and `CausalityFromRawAndReport` are exported
  at their declaration (dropping what would otherwise be unexported,
  root-package-private names) because root's staying
  `status_freshness_causality.go`
  (`StatusHandler.getFreshnessCausality`) is the caller that needs them,
  through the `freshnessNextCheckAsRecommendedCall` /
  `freshnessCausalityFromRawAndReport` forwarders in `freshness_alias.go`.
  `freshnessCausalityFromReport` has no such caller and stays unexported.
- `Handler.ServiceOwnership` (`service.CatalogCorrelationStore`) is
  the only thing binding `listServiceChangedSince`'s grant: that route's
  tables carry only `service_id`, so the grant cannot live in its own SQL
  the way the two repository-scope readers bind theirs. A nil
  `ServiceOwnership` fails every scoped caller closed (#5167); do not special
  -case nil into an unscoped-shaped answer.

## Test fixtures hoisted to querytestutil (#6608 rule)

A fixture this package's tests share with root's staying
`auth_all_scope_bearer_two_tenant_test.go` moved to `querytestutil` as an
exported helper (one definition; this package's own tests call it directly,
and root needs no forwarder because its call sites were edited in the same
move), rather than being duplicated. Do not re-duplicate any of these back
into a `_test.go` copy here or in root:

- `querytestutil.TwoTenantChangedSinceScope` / `TwoTenantChangedSinceScopes`
  / `ChangedSinceTwoTenantPriorGeneration` / `GrantMirroringChangedSince` --
  this package's `changed_since_two_tenant_test.go` and root's
  `auth_all_scope_bearer_two_tenant_test.go` both construct it.
- `querytestutil.TwoTenantGenerationRow` / `TwoTenantGenerationRows` /
  `GrantMirroringGenerations` -- this package's
  `generations_two_tenant_test.go` and root's
  `auth_all_scope_bearer_two_tenant_test.go` both construct it.
- `querytestutil.ScopedChangedSinceTenantA` -- the grant-bearing caller
  (tenant-a, repo-a, scope-a) all three of the above plus root's
  `auth_all_scope_bearer_two_tenant_test.go`'s restricted-bearer case share.
- `querytestutil.DecodeChangedSinceEnvelope` -- the response-envelope
  decoder this package's `changed_since_two_tenant_test.go` and root's
  `auth_all_scope_bearer_two_tenant_test.go` both call.

These fixtures live in `querytestutil/freshnessreader.go` beside the
repository freshness double rather than in a file of their own: a new file
would have put `querytestutil` one over the 40-non-test-file dirgate cap,
and a cap exemption is not worth a second freshness fixture file. Do not add
an exemption row to `scripts/lib/dirgate-naming-exempt.tsv` for this
package's own files -- `freshness/` has none and must stay that way.

`service_changed_since_telemetry_test.go`'s fixtures
(`fakeServiceChangedSinceLineageReader`, `fakeServiceOwnershipProbeResult`)
deliberately did NOT move to `querytestutil`: they are minimal single-caller
doubles sufficient only to land on each of the four closed grant-refusal
reasons, not a reusable SQL-mirroring fixture. Root's
`service_changed_since_grant_test.go` keeps its own richer
`grantMirroringServiceOwnership` (which also depends on root-unexported
`containsAuthString`, `errServiceCatalogOutsideGrantNeedsAGrant`, and
`serviceCatalogCorrelationMaxLimit`, which is why that whole file stays in
root) for the grant-boundary correctness proof; do not try to unify the two.

## Naming

`docs/internal/naming.md` is law: no `freshness_` file prefixes, no
`freshness/freshness_causality.go`, exported identifiers lose the family
stutter except where the forwarder rule above names a specific root caller.
The root `freshness_alias.go` keeps every old exported spelling for staying
callers. `causality_report.go`'s `Transition.FreshnessHint` field is an
exception the naming audit flags and this file explains: it is a struct
field (out of scope for the rule-4 exported-identifier check per the move
brief), and it keeps the exact name of the `status.GenerationTransitionSnapshot
.FreshnessHint` field it copies in `freshnessTransitions` -- renaming it
would diverge from that source field's name for no benefit.
