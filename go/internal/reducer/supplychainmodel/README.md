# internal/reducer/supplychainmodel

## Purpose

Owns the shape of the supply-chain-impact family's evidence DTOs — the
advisory, affected-package/range, consumption, SBOM, OS-package, scanner
attachment, deployment/workload/service-context, and risk-signal rows that
`classifySupplyChainImpactPackage` reads to classify one CVE-x-package
finding — plus `ScopeGenerationKey`, the pure key-derivation helper those rows
are joined by.

It exists so this shared vocabulary can be read by a future split of the
supply_chain_impact + suppression family (67 files, over the repo's 40-file
dirgate cap) **without importing the reducer root**. The root's
`SupplyChainImpactHandler.Handle()` passes these types into six clusters
(vulnerability intelligence, SBOM/OCI evidence, OS-package evidence,
deployment/runtime context, reachability, and risk signals); if each cluster
became its own subpackage while these DTOs stayed at the root, every cluster
would need to import the root for the shapes it reads, and the root already
imports (or will import) the clusters — an import cycle. This package
resolves that in advance for the cluster split that follows issue #6061's PR1.

## Ownership boundary

**Owns:** the 15 DTO struct declarations above and `ScopeGenerationKey`. Plain
data and one pure function; no method, no I/O, no queue/graph handle.

**Does not own:** `supplyChainImpactIndex`, the struct that aggregates these
DTOs alongside the Go/JS-TS/Python/JVM reachability indexes
(`GoVulnerabilityFinding`, `jsTSPackageReachabilityIndex`,
`pythonReachabilityRepositoryEvidence`, `jvmReachabilityIndex`) and the
container-image-identity type (`supplyChainImageIdentity`). Those four
reachability shapes and the image-identity type are out of this package's
scope — they belong to separate later-PR families — so the aggregate that
references them stays at the reducer root rather than dragging them along
into this leaf. Of the 8 orchestration functions that build and classify
findings from these DTOs, 7 (`classifySupplyChainImpactPackage` and its
helpers) also stay at the root; this PR moves only the shapes, not the logic.
The 8th, `supplyChainScopeGenerationKey`, is a pure key-derivation helper
rather than orchestration logic, so it moved here as `ScopeGenerationKey`
alongside the DTOs — see "Exported surface" below.

## Exported surface

| symbol | what it is |
|---|---|
| `ImpactCVE` | one vulnerability advisory (CVE/GHSA/OSV) |
| `AffectedPackage` | one advisory-to-package affected-range record |
| `AffectedRange` | one typed version-range event group |
| `AffectedRangeEvent` | one introduced/fixed/last-affected/limit boundary |
| `AffectedProduct` | one CPE-based affected-product match |
| `PackageConsumption` | one manifest/lockfile package-consumption correlation |
| `SBOMComponent` | one SBOM component record |
| `OSPackage` | one OS-package vulnerability-scan record |
| `ScannerAnalysis` | one sibling scanner_worker.analysis envelope |
| `ScopeGenerationKey` | the ScopeID+GenerationID join key `OSPackage`/`ScannerAnalysis` share |
| `Attachment` | one SBOM-attestation attachment record |
| `DeploymentContext` | one deploy-event/deployment-declaration record |
| `DeploymentLaneContext` | one repository's deployment-lane membership |
| `WorkloadContext` | one repository-to-workload correlation |
| `ServiceContext` | one repository-to-service-catalog correlation |
| `RiskSignals` | one advisory's EPSS/KEV risk evidence |

The reducer root's 39 files that reference these types spell the qualified
`supplychainmodel.*` name directly (for example `supplychainmodel.ImpactCVE`)
rather than through an unexported local alias — every unexported name these
types used to carry at the root (`supplyChainImpactCVE` and so on) is gone.
Field access on values of these types (for example `finding.cveID` ->
`finding.CVEID`) changed too, because Go does not allow a package outside
`supplychainmodel` to read an unexported field even through a type alias, and
because there is no longer an alias to hide the qualification behind.

## Dependencies

The standard library, plus `internal/facts` for `facts.Envelope` (used only by
`ScopeGenerationKey`, to defer to the platform's own scope-generation key
format rather than re-deriving an equivalent one locally). `internal/facts`
imports no reducer package, so the chain is leaf-to-leaf with no cycle.

**This package must never import `internal/reducer`**, directly or
transitively. Adding that import defeats the reason the package exists and
re-blocks the cluster split issue #6061 exists to unblock.

## Telemetry

None. This package declares plain data types and one pure key-derivation
function; it performs no I/O and registers no metric, span, or log field. The
supply-chain-impact handler's instrumentation is unchanged by this move and
stays at the reducer root. See the `No-Observability-Change` rows for this
package's files in `docs/public/observability/telemetry-coverage.md`.

## Gotchas / invariants

**Field names are exported; the original unexported type names at the root
are gone.** The root has no compat alias file — every reducer-root reference
to these types spells the qualified `supplychainmodel.*` name directly, and
any code that constructs or reads a field on one of these DTOs uses the
exported field name now. Do not reintroduce an unexported root-side alias to
make a diff look smaller — the whole point of the move is that this package's
exported surface is reachable from `reducer` without a root import, and an
unexported local alias provides no such reachability for anything outside
`package reducer` (see the merge-bar discussion in issue #6061 for why a
compat file that only aliases unexported names was rejected).

**`supplyChainImpactIndex` deliberately does not move here.** Do not "finish
the job" by hoisting it too: its `images`, `goReachability`,
`jsTSPackageReachability`, `pythonReachability`, and `jvmReachability` fields
name four reachability-cluster types and one image-identity type that this
package does not own. Moving the aggregate would either drag those five types
along (out of scope for this PR) or force the aggregate itself to import
`internal/reducer` back for them (the cycle this package exists to avoid).

**`ScopeGenerationKey` must stay byte-identical to `facts.Envelope`'s own
key.** It is a thin forward, not a reimplementation, precisely so the join key
`OSPackage` and `ScannerAnalysis` are correlated by never drifts from the
scope-generation boundary the rest of the platform uses.

## Performance

The hoist is a relocation plus field export: no algorithm, query, allocation
shape, or control flow changes. Measured rather than asserted, because the
gate's hot-file list covers every `supply_chain_impact_*.go` file this diff
touches.

No-Regression Evidence: `go test ./internal/reducer ./internal/reducer/code/call -run '^$' -bench ... -benchtime=200x -count=5`
on go1.27.1 darwin/arm64, comparing `origin/main` c52c30560 against this branch
in two worktrees on the same machine, back to back. Medians of five runs:

| benchmark | base ns/op | head ns/op | B/op | allocs/op |
| --- | ---: | ---: | --- | --- |
| `BuildSupplyChainImpactIndexWithQuarantine` | 6,322,679 | 6,034,819 | 7,900,670 -> 7,900,673 | 100,158 -> 100,158 |
| `AddManifestDependencySupplyChainConsumption` | 1,008,316 | 980,043 | 872,460 -> 872,451 | 7,527 -> 7,526 |
| `ClassifySupplyChainImpactDetectionProfile/rpm_exact_affected` | 29.8 | 23.1 | 0 -> 0 | 0 -> 0 |
| `ClassifySupplyChainImpactDetectionProfile/dpkg_exact_affected` | 24.8 | 25.8 | 0 -> 0 | 0 -> 0 |

Input shape: the bench fixtures' own corpus -- 100,158 allocations and ~7.9 MB
per index build, 7,526 allocations per manifest-dependency consumption pass.
No graph backend, no queue, no rows written: these are in-memory extractors, so
there is no terminal queue or row count to report and none is claimed.

Read it as no change, not as a speedup. The allocation counts are identical or
one lower and the byte counts differ by three bytes in 7.9 MB, which is what a
pure relocation should look like. The two wall-clock wins on the millisecond
benchmarks are within run-to-run variance on a laptop, and the -22% on
`rpm_exact_affected` is noise at nanosecond scale: that base sample carried a
100.6 ns outlier against a 23.95 ns median for the same input. Nothing here
supports a performance claim in either direction, which is the point.

No-Observability-Change: no metric, span, log field, or failure class is added,
removed, or renamed. The types carry no instrumentation and never did; every
finding built from them is still counted by the `projection (supply-chain)` row
in `docs/public/observability/telemetry-coverage.md` under
`eshu_dp_supply_chain_impact_findings_total` and
`eshu_dp_supply_chain_suppression_decisions_total`, and the coverage page's
required-metric set is unchanged at 395 entries.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/supplychain/core/index.go` — the orchestration functions that read these DTOs
- `docs/internal/design/package-restructure.md` — the #6061 restructure this hoist is part of
- `docs/public/observability/telemetry-coverage.md` — the coverage rows for these files
