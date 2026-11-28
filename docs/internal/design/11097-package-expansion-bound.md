# Design Record: Package-Path Provenance Bound in Supply-Chain Impact Evidence Load (`#11097`)

Refs `#11097`, `#11087`, `#10860`, `#10972`.

## Problem

Each `supply_chain_impact` reducer pass previously followed `PackageIDs` and
`PURLs` symmetrically from any loaded `vulnerability.affected_package` fact to
every other vulnerability naming that package, and from any loaded foreign
`PackageConsumption`, `SBOMComponent`, or `VulnerabilitySuppression` fact back
into `affected_package_*` probes. On a shared package (such as `npm` or `PyPI`
dependencies with dozens of historical CVEs), one vulnerability scope stored
copies of findings belonging to unrelated scopes, amplifying stored rows in
`supply_chain_impact_finding_supports`, triggering unnecessary holder fan-out
reopens, and risking round-cap truncation.

## Arbiter Ruling (`2026-11-28`, `arbiter-eshu`)

### Decision 1 — Shape A (Direction Split with Own-Scope 1-Hop OS Sibling Exemption)

- **Answer to `#11097` Decision 1:** Neither `BranchVulnerabilityUpstreamCVEID`
  (`go/internal/storage/postgres/facts/impact/branches.go`, probes only
  `vulnerability.epss_score` and `vulnerability.known_exploited`) nor
  `os_package_advisory_load.go` (loads installed `vulnerability.os_package` rows
  only) loads cross-scope sibling OS-vendor `vulnerability.affected_package`
  records (`DSA-*`, `DLA-*`, `USN-*`, `DEBIAN-CVE-*`, `ALPINE-CVE-*`). The only
  indexed arms that pull sibling OS-advisory records across batch/slot scopes
  for `decidingObservation` (`os_advisory_deciding_record.go`) and
  `recordsListingThePackage` (`retraction.go`) are `affected_package_package_id`
  and `affected_package_purl`.
- **Chosen shape:** Mirror `#11087` (`CriteriaExpansionBound` +
  `SeedProductCriteria`) with an independent `PackageExpansionBound` switch and
  `SeedPackageIDs` / `SeedPURLs` filter arms:
  1. **Owned consumer/anchor/suppression sources** stay on `PackageIDs` /
     `PURLs` (driving all probes):
     - `sbom.component`: owned iff `envelope.ScopeID == ownScopeID` or
       `document_id` belongs to `ownedDocuments(envelopes, ownScopeID)`.
     - `reducer_package_consumption_correlation`,
       `vulnerability.go_module_evidence`,
       `security_alert.repository_alert`, `package_registry.package`,
       `vulnerability.suppression` (`scope.package_id`, `scope.purl`), and
       `content_entity` Go manifest dependencies: owned iff
       `envelope.ScopeID == ownScopeID`.
  2. **Own-scope 1-hop OS sibling exemption:** `vulnerability.affected_package`
     rows with `envelope.ScopeID == ownScopeID` AND
     `(osPackageClassEcosystem(ecosystem) || osPackageFamilyFromPURL(purl) != "")`
     stay on `PackageIDs` / `PURLs` so Round 1 pulls sibling distro records for
     the same OS package, while foreign OS bulletins loaded in Round 2 do not
     chain unrelated OS packages into Round 3+.
  3. **Seeded non-exempt `vulnerability.affected_package` rows** (matching seed
     `CVEIDs` / `AdvisoryIDs` closed over `aliases` and `upstream` keys) route
     to `SeedPackageIDs` / `SeedPURLs`.
  4. **Transitive and foreign-consumer packages/PURLs** are suppressed at
     derivation and counted in `package_bound_suppressed_transitive`. Provenance
     sets fold ASCII case and whitespace (`packageidentity.LowerASCII` over
     `strings.TrimSpace`, preserving `#9584` Unicode lookalike isolation) while
     retaining stored spellings on filter lists, and owned PURLs also admit
     their version-stripped `packageIDFromPURL` form so versioned SBOM
     components keep versionless advisory and suppression PURLs on the owned
     arm.
  5. **Foreign alert/suppression CVE isolation:** Foreign `security_alert` and
     `vulnerability.suppression` rows loaded solely via `SeedPackageIDs` or
     `SeedPURLs` (non-owned package, non-seeded CVE/advisory) do not widen
     `next.CVEIDs` or `next.AdvisoryIDs`; owned passes and seeded CVEs remain
     untouched.
  6. **Scoped reruns (`#9779`):** OS-class `ScopedFindingPackage` rows remain
     eligible for owned `PackageIDs` / `PURLs`; non-OS rows route to
     `SeedPackageIDs` / `SeedPURLs`; `restrictSupplyChainImpactFilter`
     intersects both owned and seed arms with `scope.packages` / `scope.purls`.
- **Storage rebinding (`facts_active_supply_chain_impact.go`):**
  - `supplyChainImpactParamValues` binds `PackageIDs`, `PURLs`,
    `LowerPackageIDs`, and `LowerPURLs` to the unions of owned and seed lists
    so `package_consumption_package_id`, `sbom_component_purl`, `package_id`,
    `package_version_purl`, and `vulnerability_suppression` keep full discovery.
  - `supplyChainImpactBranchValues` copy-on-write rebinds
    `affected_package_package_id`, `affected_package_purl`, and
    `foreign_key_guard` (preserving `#11087` `ProductCriteria` rebinding on
    `foreign_key_guard` simultaneously) to owned `PackageIDs` / `PURLs` and
    their lowered twins.
  - Seam off (`SeedPackageIDs` and `SeedPURLs` empty) returns the shared map
    untouched, preserving byte-for-byte pre-`#8880` storage differential parity.

### Decision 2 — Holder Fan-Out and AC #2

- `FindSupplyChainImpactHoldersQuery`, `fanout.go`, `fanout_inflight_narrow.go`,
  and `supply_chain_impact_fanout_reopen.go` remain unchanged. Removing foreign
  finding copies from `supply_chain_impact_finding_supports` reduces fan-out at
  the source without risking stale-copy convergence bugs or relaxing in-flight
  soundness.
- **AC #2 explanation (173 of 300 sampled runs):** `fanout_inflight_narrow.go`
  and `fanout_stamp.go` intentionally flag active in-flight non-marker scopes
  (`Set A ∩ Set B`) with `fanout_origin.cves` even when those scopes hold no
  support row for the stamped CVEs (and prior to `#10972 D4b`, every in-flight
  pass was flagged unconditionally); `clearReplayFlag` clears disjoint stamps
  on acknowledgement. Post-`#10972 D4b` QA measurement dropped the cohort from
  `173/300` (`57.7%`, Nov 20) to `16/300` (`5.3%`, Nov 28), all 16 in-flight
  flagged passes.

### Decision 3 — Seed Initialization and Closure

- `newPackageExpansionBoundState` seeds from `initial.CVEIDs`,
  `initial.AdvisoryIDs`, `scoped.cves`, and `scoped.advisoryIDs`.
- `observe` runs to a local fixed point over `vulnerability.cve` envelopes
  whose `cve_id` or `advisory_id` is already seeded, closing over `cve_id`,
  `advisory_id`, `aliases`, and `upstream` keys. Non-seeded records never enter
  the seed set.

### Decision 4 — AC #5 (`sbom.document` Removal) and Rollback

- `sbom.document` is absent from `candidateFactKinds` in
  `go/internal/projector/supply/chain/impact/intents.go`. A characterization
  test pins this behavior in `intents_test.go`, and the fix is tracked in
  `#11689` rather than widening `#11097`.
- **One-line rollback:** Remove `PackageExpansionBound` in
  `go/internal/reducer/defaults_additive_domains_supply_chain.go` (nil restores
  unbounded package expansion independently of `CriteriaExpansionBound`).

## Rejected Alternatives

- **Shape B (unconditional exclusion of all `vulnerability.affected_package`
  from `PackageIDs`/`PURLs`):** Breaks multi-scope OS-advisory sibling
  consolidation (AC #4) because `affected_package_*` are the only indexed arms
  loading sibling distro records.
- **Widening `BranchVulnerabilityUpstreamCVEID` or adding a GIN index on
  `payload->'upstream'`:** Violates the `#9914` contract (`facts/impact/AGENTS.md`)
  and risks unmeasured write amplification on the 591 GB `fact_records` table.
- **Exempting foreign-scope OS advisories in Round 2+:** Allows multi-package
  distro bulletins (`DSA`/`USN`/`RHSA`) to chain unrelated OS packages toward
  the 8-round cap.
- **Merging `PackageExpansionBound` into `CriteriaExpansionBound`:** Prevents
  independent observation and independent one-line rollback.

## Telemetry and Observability

- Zero new OTel metrics.
- Every pass reports seven zero-comparable `Result.SubSignals` keys:
  `package_bound_suppressed_transitive`, `package_bound_loads`,
  `package_bound_owned_package_ids`, `package_bound_seed_package_ids`,
  `package_bound_owned_purls`, `package_bound_seed_purls`, and
  `package_bound_seed_cves`.
- `rounds_cut_frontier` adds `seed_package_ids` and `seed_purls` to its closed
  arm set.
- Documented in `docs/public/reference/telemetry/supply-chain-active-evidence.md`,
  `docs/public/observability/telemetry-coverage-reducer-core.md`, and
  `go/internal/reducer/supplychain/core/README.md`.

## QA Pre-Merge Audit (`the QA environment`, `2026-11-28T16:52Z`)

- **Census A (`supply_chain_impact_canonical_winners` vs active
  `supply_chain_impact_finding_supports`, read-only replica, `ON_ERROR_STOP=1`,
  `0` failed scopes):** `80,752 / 81,286` (`99.34%`) pass with an active
  own-CVE/advisory support (`50,234` direct winner scope + `30,518` active
  support). Every single one of the `18` remaining failing CVEs (`534` keys,
  `0.66%`) has `own_active = 1`, `inflight = 1`, `dead_letter = 0`,
  `no_work_item = 0` (`ORPHAN_OR_UNEXPLAINED_CVES = 0`).
- **AC #2 (`fanout_origin.cves` non-holder reopens):** Dropped from `173/300`
  (`57.7%`, Nov 20 pre-`#10972 D4b`) to `16/300` (`5.3%`, Nov 28), all 16
  attributable to the intentional in-flight replay arm
  (`FindInFlightNonMarkerScopes` / `Set A ∩ Set B` in
  `fanout_inflight_narrow.go`).
