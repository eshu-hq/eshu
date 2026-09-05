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
| `BuildSecurityAlertReconciliations` / `WithQuarantine` | `security_alert_reconciliation.go` | the entry points that build one decision per decoded provider alert |
| `SecurityAlertReconciliationDecision` | `security_alert_reconciliation.go` | the family's canonical output row |
| `ManifestConsumptionExtractor` | `security_alert_reconciliation.go` | the injected manifest-matching seam; see Gotchas / invariants below |
| classification | `security_alert_reconciliation.go` (`classifyProviderSecurityAlert`) | matched/unmatched/stale/dismissed/fixed/provider_only/unsupported/ambiguous outcome logic |
| triage | `security_alert_reconciliation_triage.go` | unsupported-ecosystem detection and missing-evidence records |
| observed-version resolution | `security_alert_reconciliation_observed_version.go` | resolves and validates the observed installed version against manifest evidence |
| typed decode | `security_alert_reconciliation_decode.go` | decodes `security_alert.repository_alert` through the `schemadecode` seam, strict (quarantining) and lenient variants |
| `SecurityAlertReconciliationHandler` | `security_alert_reconciliation_handler.go` | the reducer handler: fact loading, dedup, pending-impact deferral, quarantine recording |
| `SecurityAlertReconciliationDomainDefinition` | `security_alert_reconciliation_domain.go` | the additive domain registration |
| `PostgresSecurityAlertReconciliationWriter` | `security_alert_reconciliation_writer.go` | the batched Postgres fact writer |
| `SecurityAlertReconciliationStatus` and its constants | `security_alert_reconciliation_status.go` | the comparison outcome enum |
| `ProviderSecurityAlert` / `SecurityAlertConsumption` / `SecurityAlertImpact` | `security_alert_reconciliation_types.go` | the decoded alert, dependency-consumption, and impact-evidence shapes the matching logic joins |

**Does not own:** matching a provider alert against repository
manifest/lockfile dependency evidence. That decode and
package-identity-normalization logic belongs to the still-in-root
package-consumption-correlation family, and a family subpackage may never
import the reducer root — see "The manifest-consumption seam" under
Gotchas / invariants below.

## Exported surface

| symbol | what it is |
|---|---|
| `SecurityAlertReconciliationDecision` | the decision record |
| `BuildSecurityAlertReconciliations` / `BuildSecurityAlertReconciliationsWithQuarantine` | the pure decision builders |
| `ManifestConsumptionExtractor` | the injected manifest-matching function type |
| `SecurityAlertReconciliationHandler` | the reducer handler |
| `SecurityAlertReconciliationDomainDefinition` | the additive domain registration |
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
This package registers no instrument of its own.

## Gotchas / invariants

### The manifest-consumption seam

Matching a provider alert against repository manifest/lockfile dependency
evidence depends on `extractPackageManifestDependencies` and
`packageConsumptionKeys` -- package-identity decode and normalization logic
owned by the reducer root's still-in-root package-consumption-correlation
family. A family subpackage may never import the reducer root, so that one
piece of behavior stayed in the reducer root
(`security_alert_manifest_dependency_match.go`) instead of moving here.

This package exposes `ManifestConsumptionExtractor`, a
`func(alerts []ProviderSecurityAlert, envelopes []facts.Envelope) []SecurityAlertConsumption`
type. `BuildSecurityAlertReconciliations`, `BuildSecurityAlertReconciliationsWithQuarantine`,
and `SecurityAlertReconciliationHandler.ExtractManifestConsumptions` all take
one as an injected dependency. A `nil` extractor is valid for the two
builders and simply skips the manifest-consumption half of the evidence set.
`SecurityAlertReconciliationHandler.Handle` rejects `nil` alongside its
`FactLoader`/`Writer` checks: on the reducer intent path an absent bridge is
a forgotten registration, and failing open there commits every lockfile-only
alert as `provider_only` with no error and no counter. The reducer root wires
its own concrete implementation at every construction site
(`defaults_additive_domains_supply_chain.go`,
`supply_chain_impact_security_alert.go`), and keeps its own tests for the
real matching behavior
(`security_alert_reconciliation_lockfile_test.go`,
`security_alert_scoped_npm_test.go`) because this package cannot build a
working extractor without importing root.

### Why some helpers are declared locally instead of imported

A handful of small, pure, reducer-root-owned functions this package's own
logic touches are copied here verbatim rather than imported, because
importing the reducer root from a family subpackage is forbidden (issue
#6061) and each is either not yet extracted into its own shared subpackage
or genuinely root-scoped shared state:

- `activeRepositoryFactLoader` / `activePackageManifestDependencyFactLoader`
  (`security_alert_reconciliation_handler.go`) mirror the reducer root's
  identically-named interfaces (`package_source_correlation_handler.go`),
  shared by several families that have not moved yet. Go interfaces are
  satisfied structurally, so the same concrete `FactLoader` implementation
  root wires into other families' handlers also satisfies these local
  declarations without duplicating any logic -- the pattern
  `internal/reducer/codetaint/graph_ports.go` established.
- `packageNameFromPURL` / `packageNameFromPackageID`
  (`security_alert_reconciliation.go`) mirror
  `supply_chain_impact_manifest_dependency.go`: pure purl/package-ID string
  parsing with no further dependency.
- `securityAlertDependencyScope` / `securityAlertPayloadBoolPointer`
  (`security_alert_reconciliation.go`) mirror
  `supply_chain_impact_match.go`'s `supplyChainDependencyScope` /
  `payloadBoolPointer`: four-line payload fallbacks.
- `securityAlertConsumptionEvidenceKind`, `exactConsumptionDependencyVersion`,
  `exactManifestDependencyVersion`, and `nonVersionDependencyPrefix`
  (`security_alert_reconciliation_observed_version.go`) mirror
  `supply_chain_impact_security_alert.go` / `supply_chain_impact_ranges.go` /
  `supply_chain_impact_version_match.go`: pure version-string and
  evidence-kind-fallback logic with no reducer-root state, taking the three
  `SecurityAlertConsumption` fields the logic actually reads instead of the
  root's full `supplyChainPackageConsumption` value type.

### Tests

No test in this package exercises manifest-dependency matching: they either
pass `nil` to a builder or, on the `Handle` path that rejects `nil`, an
explicit no-op extractor that returns no consumptions -- see "The
manifest-consumption seam" above for why the tests that DO need real
manifest matching live in the reducer root instead
(`security_alert_reconciliation_lockfile_test.go`,
`security_alert_scoped_npm_test.go`), and why
`security_alert_reconciliation_batch_insert_test_helpers_test.go` is a local
copy of the reducer root's generic batched-writer test infrastructure rather
than an import (Go test files never export across packages regardless).

### Evidence

No-Regression Evidence: #6061 relocates this family's production logic
without changing its behavior. Most hunks inside the moved production files
are package-clause and import requalification: symbols the reducer root
used to supply as one-line forwarders or aliases (`Intent`, `Result`,
`Domain`, `FactLoader`, `workloadIdentityExecer`, `quarantinedFact`, the
`reducerFact*`/`reducerBatchInsert*` batch-insert family,
`loadFactsForKinds`, `recordQuarantinedFacts`, `payloadStr`/`payloadBool`/
`payloadOrderedStrings`/`payloadStrings`/`uniqueSortedStrings`/
`compactStringSlice`, and `partitionDecodeFailures`) are now imported from
the leaf package that already owned them.

The decode seam is the one exception in that list and did not survive as an
import. The root's unexported `decodeSecurityAlertRepositoryAlert` was
deleted outright rather than requalified; call sites now use the exported
[`schemadecode.DecodeSecurityAlertRepositoryAlert`]. Grouping it with the
unchanged imports above would suggest a forwarder that no longer exists.

`activeRepositoryFactLoader`/
`activePackageManifestDependencyFactLoader` are locally redeclared, not
imported, for the reason above; `packageNameFromPURL`/
`packageNameFromPackageID`/`securityAlertDependencyScope`/
`securityAlertPayloadBoolPointer`/`securityAlertConsumptionEvidenceKind`/
`exactConsumptionDependencyVersion`/`exactManifestDependencyVersion`/
`nonVersionDependencyPrefix` are locally copied verbatim from their reducer
root originals for the same reason. The one real signature change is
`BuildSecurityAlertReconciliations`/`WithQuarantine` and
`SecurityAlertReconciliationHandler` gaining the injected
`ManifestConsumptionExtractor` (see above) in place of a direct call to the
root's `extractSecurityAlertManifestConsumptions`/
`securityAlertManifestConsumptionMatches`, which stayed in root
unchanged — the reducer root wires the identical function as that
extractor at every construction site, so the composed behavior for every
caller (the reducer handler, `supply_chain_impact`'s finding seeding, and
every existing test) is unchanged; only the seam between "decide" and
"look up manifest evidence" became an explicit parameter instead of an
implicit same-package call. Measured on this branch after the final edit:
`go build ./...` and `go vet ./...` both exit 0, and
`go test ./... -count=1` passes, including this package, the reducer root,
`internal/storage/postgres`, `internal/replay/costcounting`, `cmd/reducer`,
and `internal/query`.

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
