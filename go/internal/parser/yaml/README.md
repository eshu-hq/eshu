# YAML Parser

## Purpose

internal/parser/yaml owns YAML-family source extraction for Kubernetes,
Argo CD, Crossplane, Kustomize, Flux CD Kustomization custom resources, Helm,
CloudFormation/SAM, Atlantis (`atlantis.yaml` repo-level project), and GitLab
CI (`.gitlab-ci.yml` pipeline + jobs) payload rows. It
exists so YAML parsing behavior can evolve behind a language-owned package
without depending on the parent parser dispatcher. It also emits metadata-only
declared observability rows from Helm values, GrafanaFolder and
GrafanaDashboard resources, dashboard ConfigMaps, folder, datasource, alert
provisioning, Prometheus Operator scrape and rule resources, Prometheus/Mimir
Helm values, Promtail client routes, OTel metric and log pipelines, OTel
Prometheus receiver scrape configs, Loki gateway values, OTel trace pipelines,
Tempo gateway values, Grafana Tempo datasource links, and chart ServiceMonitor
settings. It also emits metadata-only applied observability rows from Argo CD
Application status resources and Kubernetes API-exported observability
resources when status, resource version, UID, or managed-fields state proves
the file represents applied state rather than declared intent.

## Ownership boundary

This package is responsible for reading one YAML file, decoding YAML documents,
normalizing templated YAML enough for parser-safe reads, emitting hosted Pub
dependency rows from `pubspec.yaml` and `pubspec.lock`, and returning
deterministic payload buckets. The parent internal/parser package still owns
registry lookup, engine dispatch, repository path resolution, and content
metadata inference.

`image_overrides.go` builds the `image_overrides` bucket (issue #5440;
`environment` field resolution split into the sibling
`image_overrides_environment.go` to keep both files under the 500-line
package-file cap): one row per declared container image version override,
carrying the tag/digest
version truth that `helm_values[].image_repositories` and
`kustomize_overlays[].image_refs` intentionally discard from their own
tag-less identity buckets. It is purely additive -- adding it does not
change either existing bucket's output. Helm rows come from every nested
`image:` map in a values file (mirroring `collectHelmImageRepositories`'s
own walk); Kustomize rows come from a `kustomization.yaml`'s `images[]`
list, skipping an entry that declares only `name` with none of
`newName`/`newTag`/`digest` -- a no-op match-target entry with no actual
version override, so it does not get a phantom row (issue #5440 review P2).
Both producers' rows are sorted deterministically by full row content
(`sortImageOverrideRowsByContent`) before dedupe and before the caller's own
bucket sort, so the same file always parses to the same row order (issue
#5440 review; see Performance below for why an unsorted input would
otherwise make that order vary run to run). Both producers also dedupe
exact-duplicate rows (`dedupeImageOverrideRows`): two declarations identical
on every field carry no distinguishing information, so they collapse to
one; a row differing in any field (the same repository under a different
tag) is kept.

`environment` is inferred from two independent signals, in priority order.
`imageOverrideDirectoryEnvironment` resolves the
`.../environments/<env>/...` path-segment signal: `<env>` must be a real
DIRECTORY (at least one further path segment -- the file itself, or a
deeper directory -- must follow it, so `environments/values.yaml` on its
own is NOT a signal, only `environments/<env>/values.yaml` is), the result
is lowercased, and when a path contains more than one `environments/`
marker the LAST one that satisfies the directory guard wins -- the
declaration closest to the file, not the first one encountered in the path
(issue #5440 P1 and P2-1, independent review; a later marker that fails the
directory guard is skipped, not treated as clearing an earlier valid one).
A captured `<env>` segment that is itself the marker keyword
`"environments"` (two markers back to back, e.g.
`environments/environments/values.yaml`) is ALSO treated as guard-failing,
never recorded as a value (issue #5440 P2, round-3 independent review) --
recording it would be worse than `""`: `helmImageOverrideEnvironment` only
falls through to the filename inference when the path signal is empty, so a
phantom `"environments"` path result would silently suppress a correct
`values-prod.yaml` -> `"prod"` filename match. This path signal is an
explicit author declaration and is NOT gated by an
environment-token allowlist: an author who laid a repo out with an
"environments" directory chose that name deliberately, whatever it is. A
`values-<env>.yaml`/`values.<env>.yaml` filename match for Helm is a
separate, lower-priority signal used only when the path signal is absent --
an INFERENCE, not a declaration -- and IS gated behind
`helmImageOverrideEnvironmentTokens`, a closed allowlist mirroring
`isDeploymentEnvironmentToken`
(`go/internal/query/repository_deployment_evidence_read_model.go`), so a
non-environment suffix such as `values.schema.yaml` or `values.example.yaml`
resolves to `""` rather than a fabricated environment. Neither signal scans
arbitrary path segments for environment-like keywords -- broader
environment detection is issue #5444's scope.

This bucket has no consumer yet (round-4 review corrected an earlier,
inaccurate #5441 citation here and at seven other sites -- #5441 is "iac:
persist relationship Details and Terraform attributes at the graph write"
and has nothing to do with image_overrides). Issue #5445 ("contract the
extraction surface: registry entries + typed accessors") governs the
typed-accessor CONTRACT this bucket follows. Issue #5469 ("vuln: tiered
deployed-version resolution") aims to judge a vulnerability finding's
version from the strongest available tier, including branch-resolved
manifest evidence -- a Helm/Kustomize declared image tag/digest is the kind
of evidence that tier would use, though #5469 does not yet name this bucket
explicitly. Graph projection of this bucket (a node label and reducer
materialization) has no assigned issue; it is not this package's scope
regardless.

Argo CD Application rows preserve the existing singular `source_repo`,
`source_path`, `source_revision`, and `source_root` fields from the primary
source while also emitting positional `source_repos`, `source_paths`,
`source_revisions`, and `source_roots` CSV fields for parsed sources. Empty
path, revision, or root positions are preserved so downstream consumers do not
mis-associate source details with the wrong repository.

Kubernetes-family entity rows never fabricate scalar metadata from absent
fields. A missing `metadata.name` remains the empty base `name` so path and line
identity stay available, while a missing `metadata.namespace` is omitted;
neither field may contain the string `<nil>`.

CloudFormation/SAM classification and row extraction belong to the sibling
internal/parser/cloudformation package. YAML owns file decoding and intrinsic
tag normalization before passing a decoded document to that package.

`flux.go` owns Flux CD Kustomization custom resource detection and capture
(`kustomize.toolkit.fluxcd.io/*` apiVersion group, kind `Kustomization`),
kept a dedicated path rather than reusing `parseKustomization` because a Flux
Kustomization nests its declarative fields under `spec` (`sourceRef`, `path`,
`targetNamespace`) instead of carrying them at the document root like a
`kustomization.yaml` build manifest (issue #5342). `flux_source.go` owns the
Flux CD source-of-truth custom resources it reconciles against
(`source.toolkit.fluxcd.io/*` apiVersion group: `GitRepository`,
`OCIRepository`, `Bucket`), which previously fell through to the generic
`k8s_resources`/`parseK8sResource` path and lost `spec.url`/`spec.ref`/
`spec.bucketName`. All four buckets (`flux_kustomizations`,
`flux_git_repositories`, `flux_oci_repositories`, `flux_buckets`) are
registered content entities reachable through `get_entity_context` (issue
#5360 PR A). The `RECONCILES_FROM` correlation edge from a
`FluxKustomization` to its source CR is not materialized by this package --
see `docs/public/languages/flux.md`.

`flux_helm.go` owns the Flux HelmRelease custom resource
(`helm.toolkit.fluxcd.io/*` apiVersion group, kind `HelmRelease`), captured in
its own file rather than folded into `flux.go` to keep both files well under
the package line limit. It captures `spec.chart.spec` (chart/version/sourceRef,
the same `source_ref_kind/name/namespace` keys `flux.go` uses for
Kustomization) OR `spec.chartRef` (kind/name/namespace, under DISTINCT
`chart_ref_*` keys -- never folded into `source_ref_*`). `flux_source.go` also
owns the Flux HelmRepository custom resource
(`source.toolkit.fluxcd.io/*` apiVersion group, kind `HelmRepository`),
capturing `spec.url` and `spec.type` (under `repo_type`, deliberately not the
generic `type` key). Both new buckets (`flux_helm_releases`,
`flux_helm_repositories`) are registered content entities reachable through
`get_entity_context` (issue #5483 C1). The `RECONCILES_FROM` correlation edge
extension from a `FluxHelmRelease` to its resolved source/chart CR is
materialized in `go/internal/storage/cypher/canonical_flux_helm_edges.go`, not
by this package -- see `docs/public/languages/flux.md`.

## Exported surface

The godoc contract is in doc.go. Current exports are Parse, Options,
DecodeDocuments, and SanitizeTemplating.

## Dependencies

This package imports internal/parser/shared for source reads, common payload
fields, numeric conversion, bucket appends, and deterministic bucket sorting.
It imports internal/parser/cloudformation for shared CloudFormation/SAM
template extraction. It must not import the parent internal/parser package,
collector packages, graph storage, projector, query, or reducer code.

## Telemetry

This package emits no metrics, spans, or logs. Parse timing remains owned by the
collector snapshot path and parent parser engine.

## Performance

Performance Evidence: issue #5328's original fix decoded every CloudFormation/
SAM source twice -- `DecodeDocuments` (`language.go`) flattens each document to
`map[string]any` via `yamlNodeToAny`, discarding every node's real `Line`
except a single document-root capture, then a second full `gopkg.in/yaml.v3`
decode pass (`decodeDocumentNodes`) re-parsed the identical source to recover
the raw node tree the position walk needs. That second decode was measured as
the dominant added cost (a throwaway microbench isolating `DecodeDocuments`
alone vs `DecodeDocuments`+`decodeDocumentNodes` on the same representative
100/500-resource templates showed +87% ns/op and +85% allocs/op from the
second pass alone, matching the regression below almost exactly), so it has
been retired: `decodeDocumentsWithNodes` now performs exactly one decode pass
and returns each document's already-produced raw `*yamlv3.Node` root
alongside the flattened value, index-aligned; `cloudformationPositionsFromRoot`
reuses that same root instead of re-decoding. Retaining the root costs only an
`O(1)` pointer append per document -- non-CFN YAML (Kubernetes, Argo CD,
Crossplane, Kustomize, Helm, Atlantis, GitLab CI) never triggers a second
parse and pays no measurable extra cost (a 100-document Kubernetes manifest
stream microbench showed +0.05% allocs/op, i.e. exactly the one extra pointer
append per document, no second-order cost).
`BenchmarkParseCloudFormationTemplateRepresentative`/`...Large` in
`cloudformation_bench_test.go` drive the stable public `Parse()` entrypoint
unchanged, so the same benchmark body ran across three points: origin/main
commit `2395e65c4` (baseline, pre-#5328, single `DecodeDocuments` pass,
document-root `line_number` only), commit `c5d7247a8` (the double-decode fix
this recovery replaces), and this branch (`decodeDocumentsWithNodes` reuse) --
each via a `git worktree add --detach` saved copy so the shared feature
worktree's tracked files stayed untouched. `go test ./internal/parser/yaml/
-run '^$' -bench BenchmarkParseCloudFormationTemplate -benchmem -benchtime=2s
-count=8` on darwin/arm64 (Apple M4 Pro):

| Shape (resources) | origin/main baseline | Double-decode (old) | Reuse (new) | New vs baseline | New vs old |
| --- | ---: | ---: | ---: | ---: | ---: |
| Representative (100) ns/op | 1.308ms | 2.681ms | 1.344ms | +2.7% (p=0.021, n=8) | -49.9% (p=0.000, n=8) |
| Representative (100) B/op | 1.232Mi | 1.962Mi | 1.269Mi | +3.0% | -35.3% |
| Representative (100) allocs/op | 18.98k | 33.35k | 19.36k | +2.0% | -42.0% |
| Large / AWS per-stack resource ceiling (500) ns/op | 6.003ms | 11.253ms | 6.068ms | +1.1% (p=0.005, n=8) | -46.1% (p=0.000, n=8) |
| Large (500) B/op | 5.551Mi | 8.869Mi | 5.668Mi | +2.1% | -36.1% |
| Large (500) allocs/op | 85.97k | 152.14k | 87.44k | +1.7% | -42.5% |

The reuse recovers essentially the entire #5328 regression: the residual
~2-3% delta above the pre-#5328 baseline is the real per-entity position walk
itself (`cloudformationPositionsFromRoot`), which is the intended feature, not
decode overhead.

No-Regression Evidence: the remaining cost is bounded, one-time-per-file, and
stays inside the async collector snapshot/parse stage (`internal/collector` ->
`internal/parser`), never a query-serving or graph-write hot path. Even at 500
resources -- CloudFormation's own hard per-stack resource limit, so this is the
worst-case partition a single template can ever present, not merely a
"large" pick -- the absolute added cost over the pre-#5328 baseline is ~65us
per file (down from ~5.25ms under the retired double-decode path). Non-CFN
YAML files (the overwhelming majority of a repository's manifests) are
provably unaffected: `decodeDocumentsWithNodes` performs one decode pass
regardless of document type, so there is no CFN-detection branch left to keep
non-CFN files off of a second parse -- there is no second parse to avoid.

Observability Evidence: every degraded per-entity position (an unresolved alias
target, an unattributable merge key, or a missing raw-node match) is counted
through `eshu_dp_cloudformation_position_fallback_total`
(`internal/telemetry/instruments.go`, wired at
`internal/collector/git_snapshot_parse_partitions.go:451`), documented in
`docs/public/observability/telemetry-coverage.md`. An operator can use this
counter to see how often the position walk is not paying for a real
per-entity line gain.

Performance Evidence (`image_overrides`, issue #5440): this is additive new
extraction, not a rewrite, so there is no output-equivalence claim to prove --
only the added cost. `BenchmarkParseHelmValuesRepresentativeImages` (20 nested
`image:` blocks) and `BenchmarkParseKustomizationRepresentativeImages` (20
`images[]` entries), both in `image_overrides_bench_test.go`, drive the stable
public `Parse()` entrypoint unchanged, so the identical benchmark body ran on
origin/main commit `6f780442a` (before, via a `git worktree add --detach`
scratch copy, removed after measurement) and on this branch (after). `go test
./internal/parser/yaml -run '^$' -bench
'BenchmarkParseHelmValuesRepresentativeImages|BenchmarkParseKustomizationRepresentativeImages'
-benchmem -count=3` on darwin/arm64 (Apple M1 Max), mean of 3 runs:

| Fixture (20 images) | Before ns/op | After ns/op | Δ ns/op | Before B/op | After B/op | Δ B/op | Before allocs/op | After allocs/op | Δ allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Helm values.yaml | 367.2us | 360.7us | -1.8% (noise) | 248,979 | 266,682 | +7.1% | 3,689 | 3,944 | +255 (+6.9%) |
| Kustomize kustomization.yaml | 101.5us | 107.4us | +5.8% | 67,594 | 84,827 | +25.5% | 871 | 1,135 | +264 (+30.3%) |

No-Regression Evidence: the added cost is bounded, proportional to the number
of declared images in the file (a second small map built per image, no new
document decode or tree walk beyond the existing `collectHelmImageRepositories`
walk for Helm and the existing `images[]` list iteration for Kustomize), and
stays inside the same async collector snapshot/parse stage as the rest of this
package -- never a query-serving or graph-write hot path. The Helm ns/op delta
is within run-to-run noise (both directions observed across repeat runs); the
B/op and allocs/op deltas are the real, expected cost of populating the new
bucket's per-image rows and are not compounding: they hold on both the modest
(20-image) representative fixture and the empty/no-image case, which adds zero
rows (`TestParseHelmValuesImageOverridesEmptyWhenNoImages`).

Accuracy-fix follow-up (same issue, same benchmarks): a review found
`helmValuesEnvironment` was inventing environments from any
`values-<X>.yaml`/`values.<X>.yaml` suffix (`values.schema.yaml` ->
`"schema"`), and that two Helm `image:` blocks or Kustomize `images[]`
entries declaring the exact same repository/tag/digest produced duplicate
rows with no field to distinguish them. The fix gates the filename inference
behind a closed environment-token allowlist (one map lookup per Helm values
file, negligible and not separately isolated below) and adds an
exact-duplicate-row dedupe pass to both producers. The dedupe pass went
through three implementations before landing; per this repo's
Prove-The-Theory-First discipline, the second was proposed as a theory,
measured, and disproven, so the numbers for all three are kept below rather
than only the final one.

`go test ./internal/parser/yaml -run '^$' -bench
'BenchmarkParseHelmValuesRepresentativeImages|BenchmarkParseKustomizationRepresentativeImages'
-benchmem -count=5` on darwin/arm64 (Apple M1 Max), same machine, same
session, run back to back in the order below:

1. **origin/main baseline** -- no `image_overrides` bucket at all.
2. **Sprintf dedupe** (first shipped version) -- `dedupeImageOverrideRows`
   built a `fmt.Sprintf`-formatted string as the seen-set key.
3. **Struct-key dedupe (disproven theory)** -- replaced the string key with a
   flat, directly-comparable 9-field struct (`name, repository, tag, digest,
   environment, source, path, lang string; lineNumber int`), on the theory
   that a comparable struct needs no string-building allocation. Measured:
   `unsafe.Sizeof` put the struct at 136 bytes, over the Go runtime's
   128-byte `maxKeySize` threshold for in-bucket map-key storage, so the
   runtime silently fell back to indirect (pointer-boxed) key storage --
   allocs/op came back byte-for-byte identical to the Sprintf version on both
   fixtures (3,968 / 1,159), proving the theory wrong: it removed the string
   formatting but not the allocation.
4. **Linear-scan dedupe (shipped)** -- dropped the map entirely.
   `dedupeImageOverrideRows` scans the already-deduped slice so far
   (pre-sized to `len(rows)`, so `append` never reallocates) and compares
   every field via `imageOverrideRowsEqual`. This is O(n^2) in declared
   images per file, not O(n), but needs exactly ONE allocation total
   (the pre-sized slice) regardless of row count -- see
   `BenchmarkParseHelmValuesLargeImages`/`...LargeImages` below for the
   200-image worst-case-partition proof that this stays linear-looking in
   practice at realistic file sizes.

| Fixture (20 images) | 1. origin/main ns/op | 2. Sprintf ns/op | 3. struct-key ns/op | 4. linear-scan ns/op (shipped) | 1. B/op | 2. B/op | 3. B/op | 4. B/op | 1. allocs/op | 2. allocs/op | 3. allocs/op | 4. allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Helm values.yaml | 360.7us | 391.0us | 410.2us | 397.2us | 248,979†/266,682 | 272,855 | 270,345 | 266,848 | 3,689†/3,944 | 3,968 | 3,968 | **3,945** |
| Kustomize kustomization.yaml | 101.5us†/107.4us | ~122.5us | ~123.3us | ~123.4us (one 145.5us outlier) | 67,594†/84,827 | 90,385 | 88,486 | 84,989 | 871†/1,135 | 1,159 | 1,159 | **1,136** |

†origin/main's own pre-`image_overrides` number, from the original PR's
before/after table above -- included as the true zero-feature baseline;
column 1's other value is `image_overrides` with no dedupe at all (the
"before-fix" row from the original accuracy-fix report), the correct
before/after pair for the dedupe-only cost this table isolates.

The allocation counts are the reliable signal here (Go's `-benchmem` counts
are exact and stable to a byte/zero allocs across every repeat run; ns/op has
real machine-level jitter, visible in the Kustomize outlier column 4). Column
4 (shipped) recovers allocs/op to +1 over the no-dedupe baseline on Helm
(3,944 -> 3,945) and +1 on Kustomize (1,135 -> 1,136) -- within the "+0 to
+2" target, not the +24 both prior implementations carried. B/op is
similarly back within 0.06%-0.19% of the no-dedupe baseline on both
fixtures. ns/op stayed elevated by a similar magnitude across ALL THREE
dedupe implementations (roughly +8% to +14% over the origin/main baseline,
regardless of algorithm), which points to session-level machine timing
variance across this benchmark run rather than a cost specific to any one
dedupe design -- it is reported as-is rather than explained away, since it
was not isolated with a true same-binary A/B run.

`BenchmarkParseHelmValuesLargeImages`/`BenchmarkParseKustomizationLargeImages`
(200 images, 10x the representative fixture) confirm the O(n^2) scan does
not dominate at realistic file sizes: ns/op, B/op, and allocs/op all scaled
roughly 9-10x for a 10x increase in image count (Helm: 397.2us/266,848B/
3,945allocs at 20 images -> ~4.06ms/2.428MB/37,173allocs at 200 images, a
~9.4-10.2x scale-up; Kustomize: ~123.4us/84,989B/1,136allocs at 20 images ->
~1.25ms/666,084B/9,126allocs at 200 images, a ~7.9-10.1x scale-up) --
consistent with the O(n) row-construction/YAML-decode cost that already
dominates `Parse()`, not the O(n^2) comparison scan, which allocates nothing
and only matters at file sizes far beyond what a real Helm values file or
kustomization images[] list declares.

No-Regression Evidence: the shipped dedupe pass's allocation cost is now
indistinguishable from not deduping at all (+1 alloc/op on both fixtures),
and its CPU cost stays inside the same async collector snapshot/parse stage
as the rest of this package -- never a query-serving or graph-write hot
path. The whole feature (allowlist gate + dedupe) buys a P1 accuracy fix (no
fabricated `values.schema.yaml`/`values.example.yaml` environments) plus
phantom-duplicate-row elimination for effectively free allocation cost.

Determinism-fix follow-up (same issue, same benchmarks): a second
independent review found `image_overrides` row order was nondeterministic
for Helm -- two rows tied on `line_number` (Helm hardcodes it to 1) and
`name` (the same repository declared under two different tags) fell through
to `collectHelmImageOverrides`'s map-walk arrival order, and Go deliberately
randomizes map iteration per call (reviewer reproduction: 300 parses of
byte-identical input, 5 different orderings). `sortImageOverrideRowsByContent`
(image_overrides.go) now content-sorts both producers' rows before dedupe,
using the same `imageOverrideRowFields` list dedupe compares on
(`slices.SortFunc`). The same round also skips a Kustomize no-op
`images[]` entry (only `name`, no `newName`/`newTag`/`digest`) rather than
emitting a phantom override row (issue #5440 review P2) -- the 20/200-image
Kustomize fixtures below each have roughly 1-in-3 such entries by
construction, so this round's numbers include that legitimate row-count
reduction alongside the sort's own cost. `-count=5`, same session:

| Fixture | ns/op (prev -> now) | B/op (prev -> now) | allocs/op (prev -> now) |
| --- | --- | --- | --- |
| Helm 20 images | 397.2us -> ~410.4us (+3.3%) | 266,848 -> 266,844 (flat) | 3,945 -> 3,945 (+0) |
| Helm 200 images | ~4.06ms -> ~4.20ms (+3.4%) | 2,427,953 -> 2,427,990 (flat) | 37,173 -> 37,174 (+1) |
| Kustomize 20 images | ~123.4us -> ~116.5us (-5.6%, P2 fewer rows) | 84,989 -> 80,266 (-5.6%, P2) | 1,136 -> 1,087 (-49, P2) |
| Kustomize 200 images | ~1.25ms -> ~1.03ms (-17.6%, P2 fewer rows) | 666,084 -> 614,540 (-7.7%, P2) | 9,126 -> 8,597 (-5.8%, P2) |

The sort itself adds zero measurable allocations (`slices.SortFunc` sorts a
slice already in memory) and a small, expected CPU cost on Helm, where the
fixture has no no-op entries to mask it (+3.3-3.4%, an `O(n log n)`
content-comparison sort over 15-20 already-in-memory rows). Kustomize's
numbers move in the OPPOSITE direction -- faster, less memory -- because its
representative fixture's 1-in-3 no-op entries are now skipped entirely
(P2), a real, intended reduction in work, not noise. No fixture shows a
regression that would concern the repo-scale performance contract; the sort
runs once per file, over a small in-memory row count, inside the same async
parse stage as everything else in this package.

Marker-selection-fix follow-up (same issue, same benchmarks): a third
independent review found `imageOverrideDirectoryEnvironment` returned on
the FIRST `environments` path marker that satisfied the directory guard,
which is not necessarily the correct one --
`modules/environments/scripts/environments/prod/values.yaml` resolved to
`"scripts"` instead of `"prod"` (issue #5440 review P2-1). The fix scans
every marker instead of stopping at the first, keeping the LAST
guard-passing one. None of this package's benchmark fixtures contain an
`environments/` path segment, so this is a pure correctness fix with no
expected cost on the representative/large fixtures; re-measured to confirm
rather than assumed. `-count=5`, same session:

| Fixture | ns/op (prev -> now) | B/op (prev -> now) | allocs/op (prev -> now) |
| --- | --- | --- | --- |
| Helm 20 images | ~410.4us -> ~392.2us (noise) | 266,844 -> 266,840 (flat) | 3,945 -> 3,945 (+0) |
| Helm 200 images | ~4.20ms -> ~4.16ms (noise) | 2,427,990 -> 2,427,970 (flat) | 37,174 -> ~37,174 (flat) |
| Kustomize 20 images | ~116.5us -> ~113.9us (noise) | 80,266 -> 80,266 (flat) | 1,087 -> 1,087 (+0) |
| Kustomize 200 images | ~1.03ms -> ~1.03ms (flat) | 614,540 -> 614,544 (flat) | 8,597 -> 8,597 (+0) |

Confirmed, not assumed: B/op and allocs/op are unchanged (within a few
bytes -- normal small map-growth variance) on every fixture, and ns/op
deltas are within the same few-percent run-to-run band this session has
shown throughout, not a directional regression.

Keyword-collision-fix follow-up (same issue, same benchmarks): a fourth
independent review found the captured `<env>` segment could itself be the
marker keyword `"environments"` when two markers sit back to back
(`environments/environments/values.yaml` -> `"environments"`), and that this
phantom value silently suppressed a correct filename-inferred fallback
(`environments/environments/values-prod.yaml` -> `"environments"` instead of
`"prod"`) -- issue #5440 P2, round-3 independent review. The fix adds one
more string comparison per marker occurrence (reject a captured segment
equal to `"environments"`, case-insensitively). No benchmark fixture
contains an `environments/` path segment, so again no cost is expected;
re-measured to confirm:

| Fixture | ns/op (prev -> now) | B/op (prev -> now) | allocs/op (prev -> now) |
| --- | --- | --- | --- |
| Helm 20 images | ~392.2us -> ~394.9us (noise) | 266,840 -> 266,844 (flat) | 3,945 -> 3,945 (+0) |
| Helm 200 images | ~4.16ms -> ~4.33ms (noise) | 2,427,970 -> 2,427,968 (flat) | ~37,174 -> ~37,174 (flat) |
| Kustomize 20 images | ~113.9us -> ~128.6us (noise, elevated jitter this run) | 80,266 -> 80,266 (flat) | 1,087 -> 1,087 (+0) |
| Kustomize 200 images | ~1.03ms -> ~1.02ms (flat) | 614,544 -> 614,539 (flat) | 8,597 -> 8,597 (+0) |

B/op and allocs/op -- the reliable, deterministic signal -- are unchanged on
every fixture, exactly as expected for a branch that only executes when a
captured segment equals the literal marker keyword, which none of these
fixtures' paths ever produce. The Kustomize 20-image ns/op figure is the
noisiest measurement across this whole session (it has shown a wide spread
independent of any code change since round 3); it is reported as-is rather
than smoothed over.

## Gotchas / invariants

Output ordering is part of the parser fact contract. Parse sorts every emitted
bucket before returning.

Helm template manifests are intentionally skipped after source preservation
because templated chart manifests are rendered elsewhere; Chart.yaml and values
files still emit Helm metadata. `values.yaml` files may also emit declared
Grafana, Prometheus/Mimir, Loki, and Tempo observability metadata, but they do
not prove applied or live provider state.

Applied observability rows are limited to source class, source kind, Argo CD
sync/health state, Kubernetes resource identity, generation, UID fingerprint,
cluster/server fingerprint, freshness/outcome, and resource class. Declared-only
manifests do not emit applied rows.

Declared observability rows never store dashboard JSON, panel query bodies,
raw PromQL or LogQL, scrape target addresses, datasource, remote-write, or Loki
route URLs, tenant header values, tenant IDs, secure datasource values, alert
model bodies, contact addresses, folder titles, provisioning paths, log label
values, Tempo route URLs, spans, traces, raw trace IDs, request attributes,
TraceQL bodies, trace tag values, or private routing values. Unsafe values are
omitted and represented by fingerprints, redaction fields, or coverage
warnings. Applied rows follow the same boundary and never store raw status
messages, dashboard payloads, query bodies, Secret data, labels, managed fields,
raw Kubernetes UIDs, or raw cluster server URLs.

YAML intrinsic tags such as Ref and Sub are converted to the decoded shapes
expected by the CloudFormation parser before template extraction.

For a CloudFormation/SAM document, this package also walks the raw
gopkg.in/yaml.v3 node tree to give each Parameters/Conditions/Resources/
Outputs entity its own real line_number/end_line, instead of the single
document-root line every entity in the document used to share (issue #5328).
The walk is anchored strictly at the document root's own top-level section
pairs -- never by searching for a key name anywhere in the tree -- so a
resource's Properties block that happens to nest its own key named
`Resources` or `Outputs` (for example an `AWS::CloudFormation::Stack`
resource) is never mistaken for a template section. Anchors, aliases, and
`<<:` merge keys resolve with a cycle guard; a structural fallback (an
unresolvable section, or an entity the walk could not attribute) degrades to
the section header's own line rather than a fabricated per-entity guess, and
records a `cloudformation_position_fallbacks` payload row so the collector
layer can turn it into telemetry. The JSON adapter performs the equivalent
ordered-entry walk for JSON CloudFormation templates (issue #5348), so JSON
CFN entities now carry the same real per-entity `line_number`/`end_line`.

SanitizeTemplating is parser hygiene only. Do not treat it as a general
template evaluator.

Pub dependency rows are source evidence only. Hosted `pubspec.lock` rows carry
exact versions; hosted `pubspec.yaml` rows carry requested ranges. Git/path,
private-hosted, dependency override, and mismatched lockfile rows stay out of
the `dependency` contract so the reducer keeps missing evidence visible.

## Related docs

- docs/public/languages/support-maturity.md
- docs/public/architecture.md
