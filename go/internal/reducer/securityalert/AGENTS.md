# AGENTS.md — internal/reducer/securityalert

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` still applies; these add to it.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`,
`reducer/factload`, `reducer/factdecode`, `reducer/factwrite`,
`reducer/payloadcore`, `reducer/schemadecode`, `internal/facts`,
`internal/packageidentity`, `internal/telemetry`, `internal/truth`, and the
factschema SDK. It must **never** import the parent `internal/reducer`
package, directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is
a signal about where the symbol belongs, not a reason to reach upward — see
`servicecatalog/AGENTS.md`'s version of this rule for the general decision
tree. The one addition specific to this package:

- **manifest-dependency matching is a genuine injected seam, not a
  forwarder.** `ExtractSecurityAlertManifestConsumptions` needs
  `ExtractPackageManifestDependencies`/`PackageConsumptionKeys` — real decode
  and package-identity-normalization logic owned by the package correlation
  family, not a thin wrapper. It could not move here without either
  duplicating that decode subsystem (rejected: it is not a thin helper, and
  duplicating it risks security_alert's matching silently drifting from
  supply_chain_impact's identical matching) or taking over an unrelated,
  unscoped family. `ManifestConsumptionExtractor`
  (`reconciliation.go`) is the resulting seam: this package
  defines the function type and calls it if non-nil, and never imports the
  package correlation family directly; the reducer root wires
  `correlation.ExtractSecurityAlertManifestConsumptions` at the
  handler's one construction site, `defaults_additive_domains_supply_chain.go`.
  Do not "simplify" this by trying to inline the manifest logic here, by
  importing package correlation, or by duplicating the matcher — read
  `packages/correlation/security_alert_manifest_dependency_match.go`'s
  file-level comment first if you think you've found a way.

## `_test.go` exports do not cross package boundaries

A `_test.go` file's exported symbols are visible only to the package's own
test binary, never to another package importing it normally. These root test
files still need their own copies of this package's fixtures/doubles because
of that boundary, not because anyone chose to duplicate them for style:

- `security_alert_reconciliation_lockfile_test.go` and
  `security_alert_scoped_npm_test.go` (root) exercise real manifest-dependency
  matching end to end, which this package cannot do on its own (see above) —
  they call `securityalert.BuildSecurityAlertReconciliations`/
  `WithQuarantine`/`SecurityAlertReconciliationHandler` with
  `correlation.ExtractSecurityAlertManifestConsumptions` wired in,
  and keep their own
  copies of `recordingSecurityAlertReconciliationFactLoader`/`Writer`.
  `securityAlertDecisionsByFactID` is root's alone -- it never moved and this
  package has no copy.
- `security_alert_test_fixtures_test.go` (root) duplicates
  `securityAlertEnvelope`, `packageConsumptionCorrelationEnvelope`,
  `supplyChainImpactFindingEnvelope`, and
  `securityAlertEnvelopeMissingRepositoryID` for `supply_chain_impact`'s own
  tests and the root-side scoping test below.
- `security_alert_scope_test.go` (root) tests
  `supplyChainImpactUsesSecurityAlertScope`, a `supply_chain_impact`-owned
  root function this package cannot import, using
  `securityalert.ExtractProviderSecurityAlerts`/
  `ExtractProviderSecurityAlertsWithQuarantine`.

If you rename or reshape a fixture here that one of those root files mirrors
(by name or by the comment citing it), check whether the root copy needs the
same change — nothing enforces they stay in sync.

`reconciliation_batch_insert_test_helpers_test.go` in this
package is itself a local copy of the reducer root's generic
`reducer_fact_batch_insert_test_helpers_test.go`, trimmed to the
non-versioned pieces this package's writer test uses — the same pattern
`servicecatalog` and `containerimage` each use for their own copy.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md`, and `AGENTS.md`. The gate checks only that they exist; keeping
  their contents true is on you.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`. This
  package's one family-specific instrument is
  `eshu_dp_security_alert_reconciliation_impact_wait_total` (#8827); it also
  feeds the shared `eshu_dp_reducer_input_invalid_facts_total` counter through
  `factdecode.RecordQuarantinedFacts` and the generic
  `eshu_dp_reducer_executions_total`/`_run_duration_seconds` pair every
  handler emits. A telemetry row must cite those, not invent one.
- **`verify-performance-evidence.sh`** — fires on this path. It needs
  `No-Regression Evidence:` and `No-Observability-Change:` markers, unbolded
  and at the start of their line, in a tracked note. `README.md` here carries
  them; keep them unbolded and line-initial or the gate stops seeing them.
- **`verify-dirgate.sh`** — the cap is a git-derived ratchet plus write-once
  baselines (`go/cmd/dirgate`). Growth of a directory past max(40, its count at
  the base) needs a `Dirgate-Growth:` trailer in the commit message that adds the
  files; shrinking or moving files out needs nothing. Never edit the baselines.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose name matches a sibling package, so a compatibility shim or
  bridge file must be named for its subject —
  `security_alert_manifest_dependency_match.go`, not
  `securityalert_compat.go`.
- Do not suppress `dirgate` with `//nolint`.
- Do not hand-roll a matching `ManifestConsumptionExtractor` in this
  package's own tests. Builder tests pass `nil`; a `Handle` test, which
  rejects `nil`, passes an explicit no-op that returns no consumptions (see
  `README.md`). A test that needs real manifest matching belongs in the
  reducer root alongside the extractor's real implementation, not here with a
  stand-in that could silently diverge from production behavior.
- Do not change `SecurityAlertReconciliationDecision`'s field set casually.
  Outside this package it is named by the reducer root's
  `manifest_dependency.go` and lockfile test, and by
  `internal/replay/costcounting`'s cost test. `internal/storage/postgres`
  imports this package but names `SecurityAlertReconciliationFactFilter`, a
  different type, and nothing under `internal/query` imports it at all --
  check with `rg -l 'reducer/securityalert"' go/ --glob '*.go'` rather than
  trusting this list, since no gate reads it against the code.

## The impact fence (#8827)

- `ImpactNotReadyFailureClass` is non-counting (enrolled in
  `storage/postgres/reducer_queue_readiness_sql.go`). It freezes
  `attempt_count`, so never bound the in-flight wait with an attempt
  comparison; the bound is `crossscope.ProducerReadinessMaxWait` from the
  producer row's last-workable time. `TestEveryReadinessFailureClassIsEnrolled`
  enforces the `*_not_ready` naming and enrollment.
- A probe error must stay a counting error. Never convert it into the
  non-counting class.
- Keep the give-up visible: the counter outcome, the WARN log,
  `sub_signal_impact_wait_gave_up`, and the persisted `impact_wait_outcome`
  move together with the cap. A settled producer (`ImpactProducerState.Settled`)
  is a genuine no-match, not a give-up: it must not set any of the four
  (#8956). Changing `PersistedImpactWaitOutcomes` moves the query OpenAPI enum,
  the MCP description, and the contract matrix in the same change.
- A nil `ImpactProducerReadiness` disables the fence. Keep the reducer-root and
  `cmd/reducer` wiring guards green when touching registration.
