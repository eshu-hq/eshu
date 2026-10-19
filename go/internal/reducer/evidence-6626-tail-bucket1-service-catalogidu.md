# Evidence: retire service/catalogidu compat aliases (#6626 tail, bucket 1)

## What changed

55 aliases and thin forwarders in the reducer-root compat surface are
deleted, and every caller now spells the owning subpackage:

- `servicecatalog` (27): `ServiceCatalogCorrelation{Outcome,Decision,Write,WriteResult,Writer,Handler}`,
  `PostgresServiceCatalogCorrelationWriter`,
  `BuildServiceCatalogCorrelationDecisions`,
  `ServiceMaterialization{Write,Writer,Tx,Row}`,
  `PostgresServiceMaterializationWriter`,
  `ServiceMaterializationGenerationID`, `ServiceOwnershipEvidence{,Key}`,
  `ServiceEvidencePayloadHash`,
  `RepositoryScopedRuntimeInstanceLoader`,
  `ServiceRuntime{ProducerReadiness,Instance,EvidenceKey}`,
  `ServiceScoped{Documentation,Incident}EvidenceLoader`,
  `ServiceVulnerabilityAdvisoryLoader`,
  `Service{Documentation,Incident,Vulnerability}Record`.
- `sbomattest` (5): `SBOMAttestationAttachment{Decision,Write,Writer,Handler}`,
  `PostgresSBOMAttestationAttachmentWriter`.
- `summary` (8): `CodeFunction{Summary,Source,GraphID}{Loader,Writer}`,
  `ValueFlowFixpointProjector`, `CodeFunctionSummaryMaterializationHandler`.
- `iamcan` (4) + `iamescalation` (1): `IAMCan{Assume,Perform}{EdgeWriter,MaterializationHandler}`,
  `IAMEscalationEdgeWriter`.
- `secretsiam` (9): `SecretsIAMTrustChain{LoadStats,EvidenceLoader,Writer,Handler}`,
  `PostgresSecretsIAMTrustChainWriter`,
  `SecretsIAMGraph{Writer,ProjectionHandler}`, plus two
  unexported domain-definition forwarders.

24 non-test caller files repointed (postgres evidence loaders, reducer
wiring/defaults, serviceintel adapter, collector + replay cost tests),
plus test-only caller updates. New guard test
`TestTailBucket1ServiceCatalogFamilyRetired` in
`compat_alias_retirement_tail1_test.go` scans the reducer package
directory for any re-declared alias (bucket 3-8 pattern).
`compat_cloud.go` (awscloud family) is untouched and stays for a later
bucket. No golden, spec, manifest, or inventory content changed. No
historical evidence record spells a retired symbol, so there is no
deliberate remainder.

59 `.go` files (+485/-656), plus this note.

## Why no regression

No-Regression Evidence: every retired entry is an identical-type alias
(`type X = pkg.X`) or a thin forwarder with the same signature and body
shape, so every repoint is spelling-only; no signature, envelope, row,
or ordering change.
Proof on this head: `go build ./...` exit 0; `go vet ./...` exit 0;
gofmt and gofumpt clean; `go test -count=1` green over every touched
package tree; guard RED (seeded re-declaration fails) / GREEN (clean
tree passes); `scripts/test-verify-golden-corpus-gate.sh` pass;
`ci-gates validate --drift` pass; telemetry-coverage row check pass;
`git diff --check` clean. `rg` for `reducer.<name>` for every name
in the guard test's retired-name list, over all Go sources: exit 1,
zero hits (the sweep covers the full retired set, including the
SBOM-verified, IAMEscalation-failure-class, ServiceOwnership,
ServiceEvidence, RepositoryScoped, ServiceRuntime, ServiceScoped,
and ServiceRecord names a prefix pattern would miss).
Baseline: parent base build/test state; after: this head; backend n/a
(pure compile-time spelling change, no query or driver path touched);
input shape n/a (no fixture or row-shape change).

Local toolchain note: the repo pins go 1.26.x while the host mise `go`
is newer, whose build-cache export data the pinned toolchain cannot
read, so package-loading tests fail under the host driver. All Go proof
above ran under the pinned toolchain (`CC=clang`, `GOROOT`/`GOBIN`
unset).

## Observability

No-Observability-Change: no metric, span, structured-log, status, or
pprof surface is added, removed, or renamed; the touched files carry no
telemetry emitters and the rewrites preserve every call argument.

Refs #6626.
