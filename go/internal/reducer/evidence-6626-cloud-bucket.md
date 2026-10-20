# Evidence: retire cloud-bucket compat aliases (#6626 cloud bucket)

## What changed

68 aliases, consts, vars, and thin forwarders in the reducer-root compat
surface are deleted from `compat_cloud.go`, and every caller now spells the
owning subpackage:

- `awscloud` (15): `CloudResourceContainerImageEdgeWriter`,
  `AWSCloudImageMaterializationHandler`,
  `AWSCloudImageNodesNotReadyFailureClass`,
  `awsCloudImageMaterializationDomainDefinition`,
  `AWSCloudRuntimeDrift{EvidenceLoader,FindingWriter,Write,WriteResult,Handler,ReadinessChecker,FencingTokenIssuer,Tx}`,
  `PostgresAWSCloudRuntimeDriftWriter`,
  `AWSCloudRuntimeDrift{WriteSuperseded,StatePending}FailureClass`.
- `containerimage` (19): `ContainerImageIdentity{Write,Decision,WriteResult,Writer,Handler,PriorSupport,Transaction}`,
  `PostgresContainerImageIdentity{Writer,SupportWriter}`,
  `ContainerImageExistenceLookup`, `GraphContainerImageExistenceLookup`,
  `ContainerImage{Provenance,DerivedFrom}EdgeWriter`,
  `BuildContainerImageIdentityDecisions`, `digestFromImageRef`,
  `containerImage{BuiltFrom,DerivedFrom}Rows`,
  `containerImage{BuiltFrom,DerivedFrom}ProvenanceEvidenceSource`.
- `obscoverage` (10): `ObservabilityCoverage{CorrelationHandler,CorrelationWriter,CorrelationWrite,CorrelationWriteResult,MaterializationHandler,EdgeWriter}`,
  `PostgresObservabilityCoverageCorrelationWriter`,
  `ObservabilityCoverageEvidenceSource`,
  `observabilityCoverageMaterializationDomainDefinition`,
  `ObservabilityCoverageNodesNotReadyFailureClass`.
- `cloudjoin` (3): `cloudResourceJoinIndex`, `buildCloudResourceJoinIndex`,
  `cloudResourceUID`.
- `platformfam` (12): `PlatformMaterialization{Write,WriteResult,Writer,Handler}`,
  `PostgresPlatformMaterializationWriter`, `PlatformGraphLocker`,
  `WorkloadMaterializationReplayer`, `RuntimeFamilies`,
  `LookupRuntimeFamily`, `InferInfrastructureRuntimeFamilyKind`,
  `MatchesServiceModuleSource`, `TerraformPlatformEvidenceKind`.
- `shell` (7): `ShellExecIntentWriter`, `ShellExecMaterializationHandler`,
  `shellExecMaterializationFactKinds`, `ExtractShellExecRows`,
  `loadShellExecMaterializationFacts`, `buildShellExecRefreshIntents`,
  `buildShellExecSharedIntentRows`.
- `sharedintent` (2): `PartitionHashForKey`, `PartitionForKey`.

Caller files repointed: reducer-root wiring (`defaults*.go`,
`defaults_domain_catalog.go`, `defaults_handlers.go`), handler wiring
(`platform_infra_materialization.go`, `repo_dependency_projection_runner.go`,
`repo_dependency_projection_replay_skip_test.go`,
`gcp_resource_materialization.go`), root tests (codedataflow, partitioning,
shared-projection, provenance-edge, subduration, tombstone), 7
`internal/storage/postgres` drift tests to `awscloud.*`, and the replay
costcounting test to `containerimage.*`. `cmd/reducer`, projector, query,
and ifa callers already named the leaves. `compat_cloud.go` keeps only the
`GraphQueryRunner` pin block plus retired-stanza notes. New guard test
`TestCloudBucketFamilyRetired` in
`compat_alias_retirement_cloud_test.go` scans the reducer package directory
for any re-declared alias. No golden, spec, manifest, or inventory content
changed beyond the regenerated capability surface.

Struct field names and keyed-literal keys keep their bare spelling (struct
scope, not the retired package scope).

## Why no regression

No-Regression Evidence: every retired entry is an identical-type alias
(`type X = pkg.X`), an identical const/var, or a thin forwarder with the
same signature, so every repoint is spelling-only; no signature, envelope,
row, or ordering change.

Proof on this head: `go build ./...` exit 0; `go vet ./...` exit 0 (after
fixing the 6 compiler-reported breakages the deletion exposed: 2 shell
func-name misspellings, 1 skip-test field type, 1 unused import each in
`idempotency_cases_test.go` and the tombstone test, 1 missing costcounting
import — the RED state, all green after); `TestCloudBucketFamilyRetired`
PASS; `go test -count=1` green over `internal/reducer`, all six leaf
packages, `internal/replay/costcounting`, and the touched postgres unit
tests; `go test -race` over changed packages PASS (287s, pre-push floor);
`git diff --check` clean; gofumpt and golangci-lint clean on staged files.

Benchmark Evidence: `BenchmarkExtractShellExecRows` (repointed caller)
1000 x 5410488 ns/op, 5216326 B/op, 43094 allocs/op on Intel i7-8700K
(12x) — row-building path unchanged, same code under the new spelling.
Baseline: `origin/main` 0f76c3edb1 (post-#8671); after: this head; backend
n/a (pure compile-time spelling change, no query or driver path touched);
input shape n/a (no fixture or row-shape change).

Local toolchain note: the repo pins go 1.26.x (`GOTOOLCHAIN=go1.26.9`)
while the host mise `go` is 1.27.2, whose export data the go1.26-built
lint cache cannot read. All Go proof above ran under the pinned toolchain
with `CGO_CFLAGS="-O2 -g -std=gnu17"` (host glibc/gcc C23 workaround).

## Observability

No-Observability-Change: no metric, span, structured-log, status, or
pprof surface is added, removed, or renamed; the touched files carry no
telemetry emitters and the rewrites preserve every call argument.

Refs #6626.
