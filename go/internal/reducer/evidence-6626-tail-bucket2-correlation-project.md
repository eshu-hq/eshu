# Evidence: retire correlation/project compat aliases (#6626 tail, bucket 2)

## What changed

64 aliases and 14 thin forwarders in the reducer-root compat surface are
deleted, and every caller now spells the owning subpackage:

- `cicdrun` (9): `CICDRunCorrelation{Exact,Derived,Decision,Write,WriteResult,Writer,Handler}`,
  `PostgresCICDRunCorrelationWriter`,
  `cicdWorkflowImageBuiltFromEvidenceSource`.
- `crossrepo` (10): `CrossRepoEvidenceSource`, `EvidenceFactLoader`,
  `AssertionLoader`, `ResolutionPersister`, `ResolvedUpsertResult`,
  `CompletionWakeResult`, `DeployableUnitCorrelationWaker`,
  `ScopeRepositoryReader`, `RepoDependencyIntentWriter`,
  `CrossRepoRelationshipHandler`.
- `supplychaincore` (14): `SupplyChainImpact{FactFilter,Finding,AffectedExact,Write,WriteResult,Handler,Writer,Tx,Beginner,WinnersMaintainer,FencingTokenIssuer,WriteSupersededFailureClass}`,
  `PostgresSupplyChainImpactWriter`, `JVMReachabilityFactFilter`.
- `sharedintent` (13): `SharedProjection{IntentInput,AcceptanceKey,UnroutableRow,WriteReport,EdgeWriter}`
  (true targets `sharedintent.{Input,AcceptanceKey,UnroutableRow,WriteReport,EdgeWriter}`),
  `PartitionLeaseManager`, `AcceptedGeneration{Lookup,Prefetch}`,
  `UnroutableReason{MissingRequiredField,NoStatementForType}`,
  `RepoRefreshIntentType`, `repoRefreshAction`, `retractViaRefreshKey`,
  plus the `BuildSharedProjectionIntent`/`CarriesNoEdge` forwarders (true
  targets `sharedintent.{Build,CarriesNoEdge}`) and the
  `deltaScopeRepositorySet`/`uniqueRepositoryIDs`/`applyRepoRefreshDeltaScope`/`sharedProjectionRowRepoID`/`repoWideRetractRefreshPartitionKey`/`RepoWideRetractDomains`
  forwarders (true targets `sharedintent.{DeltaScopeRepositorySet,UniqueRepositoryIDs,ApplyRepoRefreshDeltaScope,RowRepoID,RepoWideRetractRefreshPartitionKey,RepoWideRetractDomains}`).
- `worker` (14): `FirstProjectionLookup`, `Partition{ProcessorConfig,ProcessResult}`,
  `ProcessPartitionOnce`, `LoadSharedProjectionConfig` (true target
  `worker.LoadConfig`), `SharedProjection{RunnerConfig,Runner}` (true
  targets `worker.{RunnerConfig,Runner}`),
  `SharedProjection{PartitionCandidateReader,UnhashedCandidateReader}`
  (true targets `worker.{PartitionCandidateReader,UnhashedCandidateReader}`),
  `DefaultSharedProjectionLeaseOwnerPrefix` (true target
  `worker.DefaultLeaseOwnerPrefix`), `default{BatchLimit,SharedPollInterval,EvidenceSource}`
  (true targets `worker.{DefaultBatchLimit,DefaultPollInterval,DefaultEvidenceSource}`),
  `sharedProjectionDomains` (callers now read `worker.Domains()` directly),
  `sharedAcceptance{LookupEvent,Telemetry}` (true targets
  `worker.{AcceptanceLookupEvent,AcceptanceTelemetry}`), plus the
  `LatestIntentsByRepoAndPartition`/`FilterAuthoritativeIntents` forwarders.
- `crossscope` (4): `CrossScopeProducer{NotReadyFailureClass,Readiness,ReadinessByDomain}`
  (true targets `crossscope.{ProducerNotReadyFailureClass,ProducerReadiness,ProducerReadinessByDomain}`),
  `crossScopeProducerNotReadyError` (true target
  `crossscope.ProducerNotReadyError`), plus the `CrossScopeCompletionEdges`/
  `crossScopeDependenciesForRegistration` forwarders (true targets
  `crossscope.{CompletionEdges,DependenciesForRegistration}`).

190 `.go` files (+1092/-1224): 100 caller files outside the reducer root
repointed from `reducer.X` to the canonical spelling, 90 root files
repointed from the bare spelling (struct field names, keyed-literal keys,
and same-name field declarations keep their bare spelling; only the type
position is qualified). New guard test
`TestTailBucket2CorrelationProjectFamilyRetired` in
`compat_alias_retirement_tail2_test.go` scans the reducer package
directory for any re-declared alias (bucket 3-8 pattern). `compat_cloud.go`
(awscloud family) and the `gpphase`/`repair`, `reducercontract`,
`schemadecode`, `owners`, value/cleanup/projection/materialization/codecall
stanzas are untouched and stay for their own buckets. `doc.go` prose and
caller comments now name the canonical spellings. No golden, spec,
manifest, or inventory content changed except one
`scripts/lib/replay_tier_inventory.json` digest repin for the enrolled
replay file whose qualifier spelling changed with no behavior change
(declarations verified unchanged, same precedent as batch 6 bucket 5).
The `ReducerDerivedSharedProjectionRowRepoIDBinding` anchor moved from the
retired row-repo helper to `buildCodeImportOwnerIndex`, the root joint
consumer of both bound kinds; the binding ID is unchanged so generated
contracts are unaffected.

## Why no regression

No-Regression Evidence: every retired entry is an identical-type alias
(`type X = pkg.Tail`) or a thin forwarder with the same signature and body
shape (verified entry-by-entry against the base alias targets, including
the differing tails: `sharedintent.EdgeWriter`, `sharedintent.Build`,
`worker.LoadConfig`, `worker.Runner`, `crossscope.ProducerReadiness`,
`crossscope.CompletionEdges`, and the rest), so every repoint is
spelling-only; no signature, envelope, row, value, or ordering change. The
one non-mechanical call-site change is `sharedProjectionDomains` (a
package-init snapshot) becoming `worker.Domains()` at three read-only
range/assertion sites over a static list. The `contract.Bind` Symbol
re-anchor names a real top-level declaration in the same package, and the
`replay_tier_inventory.json` repin covers a spelling-only qualifier change
with declarations byte-identical.
Proof on this head: `go build -p 1 ./...` exit 0; `go vet -p 1 ./...`
exit 0; gofmt and gofumpt clean over every touched file; `go test
-count=1` green over `./internal/reducer/...`,
`./internal/storage/postgres/...`, `./internal/storage/cypher/...`,
`./cmd/reducer/`, `./cmd/ifa/`, `./internal/replay/costcounting/`,
`./internal/replay/offlinetier/`, `./internal/collector/repo/git/`;
guard RED (78 failures on the unedited tree) / GREEN (clean tree);
`scripts/test-verify-golden-corpus-gate.sh` pass;
`go run ./cmd/ci-gates validate --registry specs/ci-gates.v1.yaml --drift`
pass; telemetry-coverage row check pass; `git diff --check` clean; `rg`
for `reducer.<name>` for every name in the guard test's retired-name list,
over all Go sources: zero hits (the sweep covers the full retired set,
including the unexported names and forwarders a prefix pattern would
miss). Baseline: parent base build/test state; after: this head; backend
n/a (pure compile-time spelling change, no query or driver path touched);
input shape n/a (no fixture or row-shape change).
Local toolchain note: the repo pins go 1.26.x while the host mise `go`
is newer, whose build-cache export data the pinned toolchain cannot
read, so all Go proof above ran under the pinned toolchain (`CC=clang`,
`GOROOT`/`GOBIN` unset). Two full-parallel `go build ./...` link steps
hit transient host `disk quota exceeded` errors on untouched `cmd`
binaries and pass serially (`-p 1`) and in isolation; unrelated to this
change.

## Observability

No-Observability-Change: no metric, span, structured-log, status, or
pprof surface is added, removed, or renamed; the touched files carry no
telemetry emitters and the rewrites preserve every call argument. The
re-anchored `contract.Bind` Symbol and the repinned inventory digest are
build-time metadata with no runtime surface.

Refs #6626.
