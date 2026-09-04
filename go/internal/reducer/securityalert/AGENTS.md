# AGENTS.md — internal/reducer/securityalert

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

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
  forwarder.** `extractSecurityAlertManifestConsumptions` needs
  `extractPackageManifestDependencies`/`packageConsumptionKeys` — real decode
  and package-identity-normalization logic owned by the still-in-root
  package-consumption-correlation family, not a thin wrapper. It could not
  move here without either duplicating that decode subsystem (rejected: it is
  not a thin helper, and duplicating it risks security_alert's matching
  silently drifting from supply_chain_impact's identical matching) or taking
  over an unrelated, unscoped family. `ManifestConsumptionExtractor`
  (`security_alert_reconciliation.go`) is the resulting seam: this package
  defines the function type and calls it if non-nil; the reducer root keeps
  the real implementation and wires it at every construction site. Do not
  "simplify" this by trying to inline the manifest logic here — read
  `security_alert_manifest_dependency_match.go`'s file-level comment in root
  first if you think you've found a way.

## `_test.go` exports do not cross package boundaries

A `_test.go` file's exported symbols are visible only to the package's own
test binary, never to another package importing it normally. Three root test
files still need their own copies of this package's fixtures/doubles because
of that boundary, not because anyone chose to duplicate them for style:

- `security_alert_reconciliation_lockfile_test.go` and
  `security_alert_scoped_npm_test.go` (root) exercise real manifest-dependency
  matching end to end, which this package cannot do on its own (see above) —
  they call `securityalert.BuildSecurityAlertReconciliations`/
  `WithQuarantine`/`SecurityAlertReconciliationHandler` with the root's own
  `extractSecurityAlertManifestConsumptions` wired in, and keep their own
  copies of `recordingSecurityAlertReconciliationFactLoader`/`Writer` and
  `securityAlertDecisionsByFactID`.
- `security_alert_test_fixtures_test.go` (root) duplicates
  `securityAlertEnvelope`, `packageConsumptionCorrelationEnvelope`,
  `supplyChainImpactFindingEnvelope`, and
  `securityAlertEnvelopeMissingRepositoryID` for `supply_chain_impact`'s own
  tests and the root-side scoping test below.
- `supply_chain_impact_security_alert_scope_test.go` (root) tests
  `supplyChainImpactUsesSecurityAlertScope`, a `supply_chain_impact`-owned
  root function this package cannot import, using
  `securityalert.ExtractProviderSecurityAlerts`/
  `ExtractProviderSecurityAlertsWithQuarantine`.

If you rename or reshape a fixture here that one of those root files mirrors
(by name or by the comment citing it), check whether the root copy needs the
same change — nothing enforces they stay in sync.

`security_alert_reconciliation_batch_insert_test_helpers_test.go` in this
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
  package registers no domain-specific instrument (it feeds the shared
  `eshu_dp_reducer_input_invalid_facts_total` counter through
  `factdecode.RecordQuarantinedFacts` and the generic
  `eshu_dp_reducer_executions_total`/`_run_duration_seconds` pair every
  handler emits), so its telemetry row must cite those, not invent one.
- **`verify-performance-evidence.sh`** — fires on this path. It needs
  `No-Regression Evidence:` and `No-Observability-Change:` markers, unbolded
  and at the start of their line, in a tracked note. `README.md` here carries
  them; keep them unbolded and line-initial or the gate stops seeing them.
- **`verify-dirgate.sh`** — this directory counts against the per-directory
  cap, and the `internal/reducer` row in `scripts/lib/dirgate-grandfather.tsv`
  is a monotonic ratchet. If you move files, re-derive the row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the mirror
  with `generate-dirgate-grandfather-go.sh`. Never hand-edit either, and
  never grandfather a count upward.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose name matches a sibling package, so a compatibility shim or
  bridge file must be named for its subject —
  `security_alert_manifest_dependency_match.go`, not
  `securityalert_compat.go`.
- Do not suppress `dirgate` with `//nolint`.
- Do not pass a non-nil `ManifestConsumptionExtractor` from within this
  package's own tests. Every test here uses `nil` deliberately (see
  `README.md`); a test that needs real manifest matching belongs in the
  reducer root alongside the extractor's real implementation, not here with a
  hand-rolled stand-in that could silently diverge from production behavior.
- Do not change `SecurityAlertReconciliationDecision`'s field set casually.
  `internal/storage/postgres`, `internal/replay/costcounting`, and
  `internal/query`'s scope test all name it directly; grep the whole module
  before touching it.
