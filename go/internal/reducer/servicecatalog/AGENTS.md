# AGENTS.md — internal/reducer/servicecatalog

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`,
`reducer/factload`, `reducer/factdecode`, `reducer/factwrite`,
`reducer/payloadcore`, `reducer/schemadecode`, `reducer/packagesourcecore`,
`internal/facts`, `internal/relationships`, `internal/telemetry`,
`internal/truth`, and the factschema SDK. It must **never** import the parent
`internal/reducer` package, directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper or an identity derivation goes to a shared-core tier
  (`payloadcore`, `factload`, `factwrite`, `factdecode`, `schemadecode`), with
  a one-line forwarder left in root so existing root callers compile
  unchanged;
- vocabulary (a domain name, a result status, a fact-kind string) goes to
  `reducer/contract`, with a root alias — this is how
  `ServiceCatalogCorrelationFactKind` moved;
- real logic shared with a sibling family that has also moved goes to that
  family's leaf package — this is how
  `exactPackageSourceURLMatch`/`normalizePackageSourceExactURL` became
  `packagesourcecore.ExactURLMatch`/`NormalizeExactURL`, next to the sibling
  `CanonicalURLKey` canonicalizer;
- a symbol the root genuinely owns as logic AND is still shared by other
  in-root families — `RepositoryScopedResolvedRelationshipLoader` — is
  declared locally here instead, structurally identical, per
  `internal/reducer/codetaint/graph_ports.go`'s precedent. Never hoist one of
  these unilaterally; that touches packages this family does not own.

Read the root declaration before deciding: a body of
`return payloadcore.CleanFactFilterValues(...)` is a forwarder and costs
nothing to bypass, while a real implementation needs a deliberate hoist or a
local structural redeclaration.

## `service_materialization_*` is not a second family

Do not split the materialization files into their own package. Their
handler methods (`attachServiceIncidentEvidence`,
`attachServiceVulnerabilityEvidence`, `attachServiceDocumentationEvidence`)
are declared directly on `ServiceCatalogCorrelationHandler`, and the
deployment/dependencies/runtime evidence builders feed the same handler
through its optional loader fields. Verify this at the declaration before
trusting a filename prefix elsewhere in this repo — a prefix boundary is not
always a package boundary.

## `_test.go` exports do not cross package boundaries

A `_test.go` file's exported symbols are visible only to the package's own
test binary (`go test ./internal/reducer/servicecatalog`), never to another
package importing it normally. The root's own wiring tests
(`defaults_cicd_test.go`, `service_runtime_instance_lookup_test.go`) exercise
`ServiceCatalogCorrelationHandler` and `PostgresServiceMaterializationWriter`
end to end through root's own `implementedDefaultDomainDefinitions` and
`GraphServiceRuntimeInstanceLoader`, so they need their own copies of this
package's unexported test doubles
(`stubServiceCatalogCorrelationFactLoader`,
`recordingServiceCatalogCorrelationWriter`,
`fakeServiceScopedIncidentLoader`, the `fakeServiceMaterializationStore`
family, and several fact-builder fixtures). Root keeps a trimmed local copy in
`service_catalog_correlation_root_test_doubles_test.go`. If you rename or
reshape a fixture here that file's comment says it mirrors, check whether the
root copy needs the same change — nothing enforces they stay in sync.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md`, and `AGENTS.md`. The gate checks only that they exist; keeping
  their contents true is on you.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`. If your
  file registers no instrument, use a `No-Observability-Change:` marker naming
  the signals that already cover the stage. Do not invent a metric that is
  absent from `go/internal/telemetry/instruments.go`.
- **`verify-performance-evidence.sh`** — fires on this path. It needs
  `No-Regression Evidence:` and `No-Observability-Change:` markers, unbolded
  and at the start of their line, in a tracked note. `README.md` here carries
  them; keep them unbolded and line-initial or the gate stops seeing them.
- **`verify-dirgate.sh`** — this directory counts against the per-directory
  cap, and the `internal/reducer` row in `scripts/lib/dirgate-grandfather.tsv`
  is a monotonic ratchet. If you move files, re-derive the row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the mirror with
  `generate-dirgate-grandfather-go.sh`. Never hand-edit either, and never
  grandfather a count upward.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose name matches a sibling package, so a compatibility shim must be
  named for its subject — `service_catalog_correlation_compat.go`, not
  `servicecatalog_compat.go`.
- Do not suppress `dirgate` with `//nolint`.
- Do not add a root forwarder for something only this package's own tests
  use. Root forwarders exist because a specific still-in-root file names the
  symbol unqualified; check `service_catalog_correlation_compat.go`'s own
  comments before adding another one.
- Do not change what feeds `ServiceMaterializationGenerationID` (which fields,
  or their order) without checking the idempotent re-materialization contract
  it backs: an identical evidence set must keep producing an identical
  generation id, or a repeat re-materialization stops being a true no-op.
