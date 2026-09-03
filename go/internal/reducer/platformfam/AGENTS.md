# AGENTS.md — internal/reducer/platformfam

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`,
`reducer/factwrite`, `reducer/gpphase`, `reducer/payloadcore`,
`internal/facts` and `pkg/log`. It must **never** import the parent
`internal/reducer` package, directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a slice diff, a payload accessor, a key normalizer) goes to
  `reducer/payloadcore`, with a one-line forwarder left in root so existing root
  callers compile unchanged;
- vocabulary (a fact-kind name, an enum, an outcome value) goes to
  `reducer/contract`, with a root alias;
- a shared write or readiness primitive goes to `reducer/factwrite` or
  `reducer/gpphase` the same way;
- a symbol the root genuinely owns as logic stays in root, and this package
  names the behaviour it needs as an interface instead.

`CrossRepoRelationshipResolver` is the worked example of that last case. Do not
replace it with the concrete root handler type.

## Do not widen the handler into PROVISIONS_PLATFORM writes

`PlatformMaterializationHandler` owns the `deployment_mapping` canonical fact
write and cross-repo resolution. The infrastructure-provisioning verb belongs to
the separate `platform_infra_materialization` domain, which writes through the
advisory-locked materializer so it serializes with workload materialization on
`Platform.id` (a UNIQUE constraint both MERGE against). Re-adding that write
here would reintroduce the commit-time MERGE race that split them.

## The nil-interface trap

`CrossRepoResolver` and the other seams are interfaces. A caller that assigns a
nil concrete pointer into one produces a non-nil interface value, and every
`!= nil` guard in `Handle` then dereferences it. When you add a seam, check the
reducer root's `defaults_domain_catalog.go` wiring and add the assign-only-when-
non-nil guard there, plus a `defaults_test.go` assertion that the field stays
nil when its dependencies are absent.

## Registry changes are contract changes

Adding or renaming a `TerraformRuntimeFamily` changes what the ingester infers
for real repositories. `Kind` values appear in canonical entity keys and in
`TerraformPlatformEvidenceKind` output, so a rename is a graph-truth migration,
not a refactor. Add a family with its cluster resource types, cluster module
patterns, and (only when the family has one) its service module patterns, and
extend `platform_families_test.go` in the same change --
`TestRuntimeFamiliesReturnsEightFamilies` pins the registry size on purpose.

## Telemetry

This package emits no instrument of its own; do not invent one. If a change here
needs a metric, add it to `go/internal/telemetry/instruments.go` first and add
the matching row to `docs/public/observability/telemetry-coverage.md` in the
same change. Per-stage timings belong in `Result.SubDurations`; counts and
booleans belong in `Result.SubSignals`, never in `SubDurations`, whose keys the
service layer suffixes with `_seconds`.

## Verification

    cd go && go test ./internal/reducer/platformfam/... ./internal/reducer -count=1
    cd go && go vet ./internal/reducer/...
