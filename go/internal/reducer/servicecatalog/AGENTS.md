# AGENTS.md — internal/reducer/servicecatalog

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` still applies; these add to it.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`,
`reducer/factload`, `reducer/factdecode`, `reducer/factwrite`,
`reducer/payloadcore`, `reducer/schemadecode`, `reducer/packages/source`,
`reducer/crossscope`, `internal/facts`, `internal/relationships`, `internal/telemetry`,
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
  `source.ExactURLMatch`/`NormalizeExactURL`, next to the sibling
  `CanonicalURLKey` canonicalizer;
- a symbol the root genuinely owns as logic AND is still shared by other
  in-root families — `CorpusFencedResolvedRelationshipLoader` — is
  declared locally here instead, structurally identical, per
  `internal/reducer/code/taint/graph_ports.go`'s precedent. Never hoist one of
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
- **`verify-dirgate.sh`** — the cap is a git-derived ratchet plus write-once
  baselines (`go/cmd/dirgate`). Growth of a directory past max(40, its count at
  the base) needs a `Dirgate-Growth:` trailer in the commit message that adds the
  files; shrinking or moving files out needs nothing. Never edit the baselines.

## The runtime readiness fence (#7816)

`prepareServiceMaterializations` runs the corpus fence, then
`checkServiceRuntimeReadiness`, then `attachServiceRuntimeEvidence`, all
before any write. Keep that order. A new writer of `WorkloadInstance` nodes
or `RUNS_ON` edges must be added to `runtimeProducerDomains` (if its queue is
`fact_work_items`) or classified in `runtimeWriterClasses`
(`service_catalog_runtime_producer_guard_test.go`) with a reason; the guard
fails otherwise. This package imports `reducer/crossscope` for the elapsed
bound and the cycle anchor the log line reports; that is a downward import to
a shared tier.

The `repo_dependency` shared projection is the second RUNS_ON writer (#8169).
It is fenced through its pending `shared_projection_intents` rows:
`runtimeSharedIntentProducerDomains` names its domain and the guard classifies
`canonical_relationships.go` as its `sharedIntentProducer`. Its bound counts
from the intent's `created_at` (`RuntimeProducerState.workableAnchor`, the
later of the two anchors). Repair of a generation the fence let commit
(no_producer, bound_exceeded) is not this package's job: the
`workload_materialization` ACK re-triggers the catalog in
`go/internal/storage/postgres/service_catalog_runtime_retrigger.go`, keyed on
the `repository_id` of this handler's own correlation decisions. Keep the
decision payload's `repository_id` equal to the repository the fence and the
runtime read use (`serviceRepositoryIndex`), or the re-trigger misses.

The bound is measured per in-flight repository from
`RuntimeProducerState.NewestInFlightWorkableAt` (the producer row's
last-workable time, `GREATEST(COALESCE(reopened_at, created_at),
COALESCE(last_attempt_at, created_at), COALESCE(visible_at, created_at),
COALESCE(claim_until, created_at))`), and the handler proceeds only when every
in-flight repository is past it. Do not bound on the catalog intent's
`crossscope.ReadinessCycleAnchor`: it counts queue and corpus-fence wait, so a
one-minute-old producer would commit an empty family (#7816 amendment A1). Do
not bound on the cycle start alone: the producer's non-counting corpus-fence
retries never move it, so every producer would read as exceeded when a long
corpus fence closes (amendment A2). Do not drop `claim_until`: heartbeat
writes only `claim_until` and `updated_at`, so a live producer run longer than
30 minutes would read as exceeded (amendment A3). A handler that heartbeats
forever holds the catalog; that gap is #8211. Never anchor on `updated_at`,
and keep a zero anchor deferring.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose name matches a sibling package, so a compatibility shim goes as
  a stanza in `compat_correlation.go`, never a new `*_compat.go`
  (target-tree decision 2a).
- Do not suppress `dirgate` with `//nolint`.
- Do not add a root forwarder for something only this package's own tests
  use. Root forwarders exist because a specific still-in-root file names the
  symbol unqualified; check the service-catalog stanza's own comments in
  `compat_correlation.go` before adding another one.
- Do not change what feeds `ServiceMaterializationGenerationID` (which fields,
  or their order) without checking the idempotent re-materialization contract
  it backs: an identical evidence set must keep producing an identical
  generation id, or a repeat re-materialization stops being a true no-op.
