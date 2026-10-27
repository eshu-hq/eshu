# securityalert

## Purpose

Reconciles provider-reported repository security alerts (GitHub Dependabot
and equivalent collectors) against Eshu-owned dependency consumption and
supply-chain-impact evidence, and publishes a durable per-alert comparison
verdict without itself promoting an alert into impact truth. This package
moved out of the flat `internal/reducer` root under issue #6061 and owns the
`security_alert_reconciliation` reducer domain.

## Ownership boundary

**Owns:**

| piece | file | what it does |
|---|---|---|
| `BuildSecurityAlertReconciliations` / `WithQuarantine` | `reconciliation.go` | the entry points that build one decision per decoded provider alert |
| `SecurityAlertReconciliationDecision` | `reconciliation.go` | the family's canonical output row |
| `ManifestConsumptionExtractor` | `reconciliation.go` | the injected manifest-matching seam; see Gotchas / invariants below |
| classification | `reconciliation.go` (`classifyProviderSecurityAlert`) | matched/unmatched/stale/dismissed/fixed/provider_only/unsupported/ambiguous outcome logic |
| triage | `reconciliation_triage.go` | unsupported-ecosystem detection and missing-evidence records |
| observed-version resolution | `reconciliation_observed_version.go` | resolves and validates the observed installed version against manifest evidence |
| typed decode | `reconciliation_decode.go` | decodes `security_alert.repository_alert` through the `schemadecode` seam, strict (quarantining) and lenient variants |
| `SecurityAlertReconciliationHandler` | `reconciliation_handler.go` | the reducer handler: fact loading, dedup, pending-impact deferral, quarantine recording |
| impact fence | `reconciliation_impact_fence.go` | `resolveImpactWait`: the non-counting wait while the same scope generation's `supply_chain_impact` work is in flight, the counting wait below the handler cap, and the visible give-up at it (#8827) |
| `SecurityAlertReconciliationDomainDefinition` | `reconciliation_domain.go` | the additive domain registration |
| `PostgresSecurityAlertReconciliationWriter` | `reconciliation_writer.go` | the batched Postgres fact writer |
| `SecurityAlertReconciliationStatus` and its constants | `reconciliation_status.go` | the comparison outcome enum |
| `ProviderSecurityAlert` / `SecurityAlertConsumption` / `SecurityAlertImpact` | `reconciliation_types.go` | the decoded alert, dependency-consumption, and impact-evidence shapes the matching logic joins |

**Does not own:** matching a provider alert against repository
manifest/lockfile dependency evidence. That decode and
package-identity-normalization logic belongs to the package correlation
family, and a family subpackage may never import the reducer root (nor does
this package import a sibling family directly) — see "The
manifest-consumption seam" under Gotchas / invariants below.

## Exported surface

| symbol | what it is |
|---|---|
| `SecurityAlertReconciliationDecision` | the decision record |
| `BuildSecurityAlertReconciliations` / `BuildSecurityAlertReconciliationsWithQuarantine` | the pure decision builders |
| `ManifestConsumptionExtractor` | the injected manifest-matching function type |
| `SecurityAlertReconciliationHandler` | the reducer handler |
| `SecurityAlertReconciliationDomainDefinition` | the additive domain registration |
| `ImpactProducerReadiness` / `ImpactProducerState` | the injected port reporting the same scope generation's `supply_chain_impact` work by status; the Postgres store is `storage/postgres/readiness/producer.ImpactStore` |
| `ImpactNotReadyFailureClass` | the non-counting failure class of the in-flight wait, enrolled in `nonCountingReducerRetryFailureClasses` |
| `ImpactWaitDeferredInFlight` / `ImpactWaitGaveUpUnmatched` / `ImpactWaitGaveUpBoundExceeded` / `ImpactWaitProducerSettled` | the closed outcome set of `eshu_dp_security_alert_reconciliation_impact_wait_total` |
| `PersistedImpactWaitOutcomes` | the closed set a reconciliation row may carry as `impact_wait_outcome` (#8956): the two give-up outcomes; the query OpenAPI enum is pinned to it |
| `SecurityAlertReconciliationWriter` / `SecurityAlertReconciliationWrite` / `WriteResult` | the writer interface and its publication I/O |
| `PostgresSecurityAlertReconciliationWriter` | the Postgres writer implementation |
| `SecurityAlertReconciliationStatus` and its constants | the comparison outcome enum |
| `SecurityAlertReconciliationMissingEvidence` | the structured evidence-gap record |
| `SecurityAlertReconciliationFactFilter` | bounds active-evidence loading for one intent |
| `ProviderSecurityAlert` / `SecurityAlertConsumption` / `SecurityAlertImpact` | decoded evidence shapes exported for the reducer root's manifest-consumption bridge and supply_chain_impact's finding seeding |
| `ExtractProviderSecurityAlerts` / `ExtractProviderSecurityAlertsWithQuarantine` | the lenient and strict decode entry points, exported for the reducer root's evidence-scoping fence and finding seeding |
| `ExtractSecurityAlertConsumptions` | the non-manifest consumption extractor, exported for `supply_chain_impact`'s finding seeding |
| `MatchSecurityAlertConsumption` / `SecurityAlertRepositoryScopeMatches` / `SecurityAlertPackageNameCandidates` / `SecurityAlertIDMatches` | matching primitives exported for the reducer root's manifest-consumption bridge and `supply_chain_impact` |

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/facts` — the fact envelope and fact-kind types the decode and
  extraction seams read
- `internal/packageidentity` — package-identity normalization used by triage
  and observed-version resolution
- `internal/reducer/contract` (aliased `reducercontract`) — the
  `Intent`/`Result`/`Domain` shapes `SecurityAlertReconciliationHandler.Handle`
  implements
- `internal/reducer/factdecode` — quarantine handling
  (`QuarantinedFact`, `PartitionDecodeFailures`, `RecordQuarantinedFacts`)
- `internal/reducer/factload` — the scoped fact loader the handler uses to
  pull active evidence
- `internal/reducer/factwrite` — the batched fact-row writer
  `PostgresSecurityAlertReconciliationWriter` is built on
- `internal/reducer/payloadcore` — payload accessor and string-normalization
  helpers
- `internal/reducer/schemadecode` — the typed
  `security_alert.repository_alert` decode seam
  (`DecodeSecurityAlertRepositoryAlert`)
- `internal/telemetry` — the `Instruments` the handler records quarantine and
  execution metrics through
- `internal/truth` — the `TruthContract`/`Layer` types the domain
  registration declares (comparison state only; provider alert state is
  never impact truth)
- `sdk/go/factschema` — the `FactKindReducerPackageConsumptionCorrelation`
  constant used to identify consumption evidence
- `sdk/go/factschema/securityalert/v1` (aliased `securityalertv1`) — the
  typed `RepositoryAlert` payload shape the decode seam returns

No dependency on the reducer root, and none of the root's other family
subpackages.

## Telemetry

Facts rejected for a malformed payload feed the shared
`eshu_dp_reducer_input_invalid_facts_total` counter through
`factdecode.RecordQuarantinedFacts` instead of a family-specific one, and
the reducer executions that run this handler stay covered by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.
The package emits one counter registered in `internal/telemetry`,
`eshu_dp_security_alert_reconciliation_impact_wait_total{domain,outcome}`, from
`reconciliation_impact_fence.go` (#8827): `deferred_in_flight`,
`gave_up_unmatched`, `gave_up_bound_exceeded`, `producer_settled` (#8956). The
defer and the settled write log at INFO and the give-up at WARN with
`scope_id`, `generation_id`, and `impact_wait_outcome`; a give-up also sets
`sub_signal_impact_wait_gave_up` on the success line. See
[Security Alert Reconciliation Impact Wait](../../../../docs/public/reference/telemetry/security-alert-reconciliation-impact-wait.md).

## Gotchas / invariants

### The impact fence (#8827)

`supply_chain_impact` and `security_alert_reconciliation` are enqueued from the
same scope generation and share the default `(scope, scope id)` conflict key.
That key fences them only while one is claimed or running. While the producer
is pending or retrying the reconciliation is claimable, so it used to run, spend
a counting attempt, and give up at attempt 3 before impact existed.
`resolveImpactWait` asks `ImpactProducerReadiness` for the producer's state:

- in flight and under `crossscope.ProducerReadinessMaxWait` (30 minutes, from
  the newest row's last-workable time): return `ImpactNotReadyFailureClass`,
  which the queue exempts from the retry budget;
- otherwise, below the handler cap of 3 attempts: the counting
  `security_alert_reconciliation_waiting_for_impact` wait;
- at the cap with a settled producer (`ImpactProducerState.Settled`: at least
  one impact row and every one succeeded): write `unmatched` with no give-up,
  record `producer_settled`, log INFO. That is a genuine no-match (#8956);
- at the cap otherwise: write `unmatched`, record `gave_up_unmatched` or
  `gave_up_bound_exceeded`, log WARN, set `sub_signal_impact_wait_gave_up`, and
  pass the outcome on `SecurityAlertReconciliationWrite.ImpactWaitOutcome`. The
  writer stamps it as the `impact_wait_outcome` payload key only on decisions
  that waited on impact evidence and refuses any value outside
  `PersistedImpactWaitOutcomes`; with no give-up the payload bytes are the
  pre-#8956 bytes (`TestSecurityAlertReconciliationWriterKeepsTheGenuineNoMatchPayload`).

On the cap attempt `probeImpactProducerBeforeLoad` reads the producer state
before any evidence loads, and `resolveImpactWait` decides with that one
reading. Read after the load, a finding that committed and acked in between
would be missing from the decisions while the probe read settled
(`TestSecurityAlertReconciliationReadsSettlementBeforeTheEvidenceLoad`).

The non-counting class freezes `attempt_count`, so the in-flight wait is
bounded by ELAPSED TIME only; never compare the attempt count to bound it. A
probe error is returned as a counting error and is never turned into the
non-counting class, which would retry forever without surfacing. A nil port
disables the fence; `TestSecurityAlertReconciliationRegistrationCarriesTheImpactProducerReadinessSeam`
and the `cmd/reducer` wiring test guard the wire. The producer probe is keyed on
the intent's own scope and generation, so impact evidence produced by another
scope's impact run is not waited on.

### The manifest-consumption seam

Matching a provider alert against repository manifest/lockfile dependency
evidence depends on `ExtractPackageManifestDependencies` and
`PackageConsumptionKeys` -- package-identity decode and normalization logic
owned by the package correlation family
(`packages/correlation/security_alert_manifest_dependency_match.go`, moved out
of the reducer root with the family in #6061 and exported). A family
subpackage may never import the reducer root, and this package never imports
a sibling family directly, so the bridge stays behind the injected
`ManifestConsumptionExtractor` seam: the reducer root wires
`correlation.ExtractSecurityAlertManifestConsumptions` at the one
construction site instead of this package calling the family.

This package exposes `ManifestConsumptionExtractor`, a
`func(alerts []ProviderSecurityAlert, envelopes []facts.Envelope) []SecurityAlertConsumption`
type. `BuildSecurityAlertReconciliations`, `BuildSecurityAlertReconciliationsWithQuarantine`,
and `SecurityAlertReconciliationHandler.ExtractManifestConsumptions` all take
one as an injected dependency. A `nil` extractor is valid for the two
builders and simply skips the manifest-consumption half of the evidence set.
`SecurityAlertReconciliationHandler.Handle` rejects `nil` alongside its
`FactLoader`/`Writer` checks: on the reducer intent path an absent bridge is
a forgotten registration, and failing open there commits every lockfile-only
alert as `provider_only` with no error and no counter. The handler has exactly
one production construction site,
`defaults_additive_domains_supply_chain.go:66`, and the reducer root wires its
own concrete implementation there. `security_alert.go` is
not a construction site: it calls the same bridge function directly, without
going through a builder. Root keeps its own tests for the
real matching behavior
(`security_alert_reconciliation_lockfile_test.go`,
`security_alert_scoped_npm_test.go`) because this package cannot build a
working extractor without importing root.

### Why some helpers are declared locally instead of imported

A handful of small, pure functions this package's own logic touches are
declared here rather than imported, because importing the reducer root from a
family subpackage is forbidden (issue #6061) and each is either not yet
extracted into its own shared subpackage or genuinely root-scoped shared
state. Nine functions are copied in this way. Four are byte-identical to their
root originals; five are not: three were renamed (`securityAlertDependencyScope`,
`securityAlertPayloadBoolPointer`, `securityAlertHasPackageSourceRepositoryFact`) --
two of those also name a leaf package where root named its own forwarder --
`exactConsumptionDependencyVersion` was re-parameterised, and
`securityAlertConsumptionEvidenceKind` returns the `factschema` constant
directly where root returns its `packageConsumptionCorrelationFactKind` alias
for the same value. Each bullet below says which:

- `activeRepositoryFactLoader` / `activePackageManifestDependencyFactLoader`
  (`reconciliation_handler.go`) mirror the reducer root's
  identically-named interfaces (`package_source_correlation_handler.go`),
  shared by several families that have not moved yet. Go interfaces are
  satisfied structurally, so the same concrete `FactLoader` implementation
  root wires into other families' handlers also satisfies these local
  declarations without duplicating any logic -- the pattern
  `internal/reducer/code/taint/graph_ports.go` established.
- `packageNameFromPURL` (`reconciliation.go`) mirrors
  `manifest_dependency.go`: pure purl string parsing with no further
  dependency. `packageNameFromPackageID` forwards to
  `payloadcore.PackageNameFromPackageID`, so the alert side and the
  supply-chain impact side agree on a Maven package ID's `group:artifact`
  name (#9286).
- `securityAlertDependencyScope` / `securityAlertPayloadBoolPointer`
  (`reconciliation.go`) are `match.go`'s
  former `supplyChainDependencyScope` / `payloadBoolPointer`: short payload
  fallbacks this family was the only caller of, so the root copies are deleted
  in the same change rather than left dead.
- `securityAlertHasPackageSourceRepositoryFact`
  (`reconciliation_handler.go`) is
  `package_source_correlation_handler.go`'s `hasPackageSourceRepositoryFact`
  under a family-scoped name: a short envelope-kind scan. Root keeps its
  own copy: two root callers still use it
  (`package_source_correlation_handler.go`, `repository.go`).
- `securityAlertConsumptionEvidenceKind`, `exactConsumptionDependencyVersion`,
  `exactManifestDependencyVersion`, and `nonVersionDependencyPrefix`
  (`reconciliation_observed_version.go`) mirror
  `security_alert.go` and `ranges.go`
  (which holds all three version helpers): pure version-string and
  evidence-kind-fallback logic with no reducer-root state. Only
  `exactConsumptionDependencyVersion` is re-parameterised, to the four
  `SecurityAlertConsumption` fields the logic reads instead of the full
  `supplychainmodel.PackageConsumption` value type; the Evidence section below
  has the detail.

### Tests

No test in this package exercises manifest-dependency matching: they either
pass `nil` to a builder or, on the `Handle` path that rejects `nil`, an
explicit no-op extractor that returns no consumptions -- see "The
manifest-consumption seam" above for why the tests that DO need real
manifest matching live in the reducer root instead
(`security_alert_reconciliation_lockfile_test.go`,
`security_alert_scoped_npm_test.go`), and why
`reconciliation_batch_insert_test_helpers_test.go` is a local
copy of the reducer root's generic batched-writer test infrastructure rather
than an import (Go test files never export across packages regardless).

### Evidence

No-Regression Evidence: #6061 relocates this family's production logic
without changing its behavior. Most hunks inside the moved production files
are package-clause and import requalification: symbols the reducer root
used to supply as one-line forwarders or aliases -- the reducer-contract
types, the fact-load and quarantine helpers, the batch-insert family, and the
payload accessors -- are now imported from the leaf package that already owned
them. The rule holds for every such symbol rather than for a list of them:
where root declared a forwarder, the call now names the forwarder's target
directly, so the same function runs. `git diff` against the base file is the
authority, not an enumeration here.

The decode seam is the one exception and did not survive as an
import. The root's unexported `decodeSecurityAlertRepositoryAlert` was
deleted outright rather than requalified; call sites now use the exported
[`schemadecode.DecodeSecurityAlertRepositoryAlert`]. Grouping it with the
unchanged imports above would suggest a forwarder that no longer exists.

`activeRepositoryFactLoader`/
`activePackageManifestDependencyFactLoader` are locally redeclared, not
imported, for the reason above, and the nine functions listed under "Why some
helpers are declared locally instead of imported" are locally copied from their
reducer root originals for the same reason -- that bullet list is the single
enumeration of the copied functions, so it does not disagree with a second copy
here. Two of
the three version helpers are byte-identical;
`exactConsumptionDependencyVersion` is re-parameterised to the four
`SecurityAlertConsumption` fields it reads instead of the full
`supplychainmodel.PackageConsumption` value type, with a behaviour-equivalent
body
(root's one-line `normalizedSupplyChainVersionEcosystem` inlined). The one real signature change is
`BuildSecurityAlertReconciliations`/`WithQuarantine` and
`SecurityAlertReconciliationHandler` gaining the injected
`ManifestConsumptionExtractor` (see above) in place of a direct call to
`correlation.ExtractSecurityAlertManifestConsumptions`/
`securityAlertManifestConsumptionMatches`, which moved to the package correlation
family with their logic unchanged and only their alert/consumption types
requalified to this package's exported ones — the reducer root wires that
same function as the extractor at the handler's one construction site, so the
composed behavior for every
caller (the reducer handler, `supply_chain_impact`'s finding seeding, and
every existing test) is unchanged; only the seam between "decide" and
"look up manifest evidence" became an explicit parameter instead of an
implicit same-package call. Measured on this branch: `go build ./...` and
`go vet ./...` exit 0 at the branch head, and `go test ./... -count=1` passes
across the whole module -- including this package, the reducer root,
`internal/storage/postgres`, `internal/replay/costcounting`, `cmd/reducer`,
and `internal/query`. The full-module run is taken at whichever head last
changed a `.go` file; the PR body's evidence table names that commit and shows
the command that proves every Go change since it is comment-only.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span,
or log field. The counter and executions pair above are the same before and
after the move; only the file paths the telemetry-coverage rows point at
changed.

## Related docs

- `docs/internal/design/package-restructure.md` (#6061 package restructure)
- `docs/public/reference/http-api/evidence-and-supply-chain.md` (the
  reconciliation read surface: `security_alert_reconciliation_aggregate`)
- `docs/public/reference/security-intelligence-provider-alert-parity.md`
  (the provider-alert parity gate this domain's comparison rows feed)
