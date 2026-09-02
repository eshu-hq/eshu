# Agent instructions: internal/reducer/incident

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The PagerDuty incident-routing reducer family: the
`incident_routing_materialization` graph-evidence handler and the
`incident_repository_correlation` durable correlation handler + Postgres
writer (issue #2161). Moved out of the reducer root in issue #6061; every
outside symbol the family needed was already a leaf-package forwarder
(`payloadcore`, `contract`, `factdecode`, `schemadecode`, `factwrite`) before
this move.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/incident/README.md`
- `docs/internal/design/package-restructure.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it for `IncidentRoutingHandlers`
  wiring and handler construction, never the reverse.
- **The two `DomainDefinition` builders live in the reducer root, not here.**
  `incidentRoutingMaterializationDomainDefinition` and
  `incidentRepositoryCorrelationDomainDefinition` stayed in
  `go/internal/reducer/registry_additive_domains.go` (every additive
  family's builder lives there regardless of where the handler moved — see
  `configStateDriftDomainDefinition` for the precedent). Do not re-add a
  domain-definition builder in this package.
- **The graph contract only materializes exact convergence.** Only full
  declared/applied/observed exact agreement, or exact live-only no-IaC
  evidence, becomes a graph row (`incidentRoutingEvidenceIsGraphEligible`).
  Every weaker outcome (drifted, stale, permission-hidden, ambiguous,
  unresolved, rejected, derived, missing) must stay provenance-only — do not
  widen the eligibility check without an ADR.
- **Correlation identity excludes the resolved repository id on purpose.**
  `incidentRepositoryCorrelationIdentity` keys on (scope, generation,
  provider, provider_service_id) so an outcome flip updates the same row
  instead of appending a duplicate. Do not add the repository id to the
  identity.
- **The backend resolver is memoized per distinct (backend_kind,
  locator_hash).** `resolveDistinctBackends` must consult
  `BackendRepositoryResolver` at most once per distinct locator regardless of
  how many provider services or rows share it.
- **Do not import the reducer root's shared batch-insert test doubles.**
  This package keeps its own scoped copy
  (`incident_repository_correlation_writer_batch_test_helpers_test.go`) of
  the `fakeWorkloadIdentityExecer` / `decodeBatchedFactCalls` shapes instead
  of the root's `workload_identity_writer_test.go` /
  `reducer_fact_batch_insert_test_helpers_test.go` versions — those are still
  shared by other families that have not moved out of the root
  (verify with `rg -l "fakeWorkloadIdentityExecer" go/internal/reducer/
  --glob '*.go' | rg -v incident | wc -l`). Keep the two copies structurally
  identical if you change the wire shape (`factwrite.BatchInsertQuery`'s
  argument order/count); do not let them silently drift apart.
- **A fact-payload decode error routes through `factdecode.PartitionDecodeFailures`,
  never a silent empty-string read.** A missing required field quarantines
  that one fact as an input_invalid dead-letter and skips it while every
  valid sibling still projects; any other decode error is fatal and aborts
  the whole intent. Do not add a raw `map[string]any` field read as a
  workaround for a decode-seam gap.

## Common changes

Adding a new incident-routing evidence slot (beyond
declared/applied/observed): extend `incidentRoutingSlotDecision` and the
three `incidentRouting*Decision` builders in
`incident_routing_evidence_rows.go` together, and update
`incidentRoutingEvidenceIsGraphEligible`'s convergence rule to say whether the
new slot participates in exact eligibility.

Adding a new correlation outcome: extend
`IncidentRepositoryCorrelationOutcome`'s const block and
`incidentRepositoryCorrelationOutcomes()` in
`incident_repository_correlation.go` together, and decide in
`classifyProviderServiceCandidate` (`incident_repository_correlation_build.go`)
which classification path produces it.

## Failure modes to avoid

- Adding a `DomainDefinition` builder in this package instead of the reducer
  root's `registry_additive_domains.go` — see Invariants above.
- Widening `incidentRoutingEvidenceIsGraphEligible` to materialize a
  non-exact outcome as a graph row.
- Adding the resolved `RepositoryID` to `incidentRepositoryCorrelationIdentity`.
- Adding a batch-insert-shaped test helper to this package's
  `incident_repository_correlation_writer_batch_test_helpers_test.go` that
  silently diverges from the root's copies other families still use. If the
  shared shape changes, change both.

## Do not change without ADR review

- The exact-convergence-only graph eligibility rule
  (`incidentRoutingEvidenceIsGraphEligible`).
- The identity key excluding `RepositoryID` for correlation facts.
