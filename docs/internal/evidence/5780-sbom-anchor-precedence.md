# #5780 — SBOM branch must not clobber the consumption RepositoryID anchor

Validation record for the sibling fix to #5779. #5779 (PR #5782) repaired the
`os_package` branch of `classifySupplyChainImpactPackage`
(`go/internal/reducer/supply_chain_impact_index.go`) so an image-identity source
anchor no longer overwrites a consumption-derived git repository, using
`supplyChainRepositoryAnchorIsReplaceable` to distinguish a real git anchor from
a blank or OCI-registry one.

## What was still wrong

The SBOM/component branch above it overwrote `finding.RepositoryID` with
`image.repositoryID` unconditionally. A finding carrying a per-package
consumption anchor plus SBOM evidence, but no `os_package` evidence, never
reaches the `os_package` repair and therefore shipped the OCI registry path
(`oci-registry://...`) as its anchor. That namespace is disjoint from every git
`repository:...` id which `matchingSupplyChainWorkloads` / `Services` /
`DeploymentLanes` join on by exact equality, so the finding was unreachable from
workload/service/environment context: unit-green, dead in production — the
failure mode #5463 exists to prevent.

## Fix

Apply the same `supplyChainRepositoryAnchorIsReplaceable` guard to the SBOM
branch, so a per-package consumption anchor outranks the image-level identity
regardless of which image-evidence path supplied the digest. A blank anchor
still takes the OCI path exactly as before, so behavior is unchanged when no
consumption evidence exists.

## Verification

No-Regression Evidence: this is a value-only correction to one already-computed
struct field (`finding.RepositoryID`) inside an in-memory classification walk —
no new Cypher, `MATCH`/`MERGE` anchor, graph round trip, fact load, or
worker/lease/queue/batch surface. `supply_chain_impact_index.go` is
content-flagged hot only because it already contains the unchanged
`scanner_worker.analysis` digest join. Focused proof:
`cd go && go test ./internal/reducer -count=1` stays green, including the
existing #5779 regressions from `main` and the new failing-then-green
`TestSupplyChainImpactFindingPrefersConsumptionRepositoryOverSBOMImageIdentity`,
which fails (`RepositoryID = "oci-registry://..."`) when the guard is reverted
and passes with it. Golden corpus: the only supply-chain finding whose
`repository_id` the B-12 snapshot pins
(`testdata/golden/e2e-20repo-snapshot.json`, `findings[].repository_id` for
CVE-2026-00010 = `repository:r_217415d9`) is a dpkg OS-package finding;
`reducer_package_consumption_correlation` anchors are produced only for
package-manifest dependencies (`package_correlation_writer.go`), never for a
Debian system package, so its consumption anchor is absent and this guard is a
no-op there. The change alters `repository_id` values only, never node/edge
counts or finding existence, so no other snapshot assertion can drift;
`scripts/verify-golden-corpus-gate.sh` (triggered on `go/internal/reducer/**`)
re-confirms live in CI.

No-Observability-Change: no metrics, spans, structured logs, or status fields
are added or altered; the change reassigns one in-memory finding field before
projection.
