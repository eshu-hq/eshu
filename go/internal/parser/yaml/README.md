# YAML Parser

## Purpose

internal/parser/yaml owns YAML-family source extraction for Kubernetes,
Argo CD, Crossplane, Kustomize, Helm, CloudFormation/SAM, Atlantis
(`atlantis.yaml` repo-level project), and GitLab CI
(`.gitlab-ci.yml` pipeline + jobs) payload rows. It
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

Argo CD Application rows preserve the existing singular `source_repo`,
`source_path`, `source_revision`, and `source_root` fields from the primary
source while also emitting positional `source_repos`, `source_paths`,
`source_revisions`, and `source_roots` CSV fields for parsed sources. Empty
path, revision, or root positions are preserved so downstream consumers do not
mis-associate source details with the wrong repository.

CloudFormation/SAM classification and row extraction belong to the sibling
internal/parser/cloudformation package. YAML owns file decoding and intrinsic
tag normalization before passing a decoded document to that package.

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

Performance Evidence: issue #5328 adds a second `gopkg.in/yaml.v3` decode pass
(`decodeDocumentNodes` in `cloudformation_positions.go`) that is gated strictly
behind CloudFormation/SAM detection -- `Parse` calls it at most once per file,
only after `cloudformation.IsTemplate` matches at least one already-decoded
document, so every other YAML shape this package handles (Kubernetes, Argo CD,
Crossplane, Kustomize, Helm, Atlantis, GitLab CI) pays zero extra decode cost.
`BenchmarkParseCloudFormationTemplateRepresentative`/`...Large` in
`cloudformation_bench_test.go` drive the stable public `Parse()` entrypoint
unchanged, so the same benchmark body ran on both sides of the change: origin/main
commit `2395e65c4` (single `DecodeDocuments` pass, document-root `line_number`
only, via a `git worktree add --detach` saved copy so the shared feature
worktree's tracked files stayed untouched) versus this branch (adds
`decodeDocumentNodes` plus the per-entity position walk). `go test
./internal/parser/yaml/ -run '^$' -bench BenchmarkParseCloudFormationTemplate
-benchmem -benchtime=2s -count=6` on darwin/arm64 (Apple M4 Pro):

| Shape (resources) | Before (old) | After (new) | Delta |
| --- | ---: | ---: | ---: |
| Representative (100) ns/op | 1.291ms | 2.252ms | +74.5% (p=0.002, n=6) |
| Representative (100) B/op | 1.232Mi | 1.962Mi | +59.3% |
| Representative (100) allocs/op | 18.98k | 33.35k | +75.8% |
| Large / AWS per-stack resource ceiling (500) ns/op | 6.001ms | 10.639ms | +77.3% (p=0.002, n=6) |
| Large (500) B/op | 5.551Mi | 8.869Mi | +59.8% |
| Large (500) allocs/op | 85.97k | 152.14k | +77.0% |

No-Regression Evidence: the added cost is bounded, one-time-per-file, and stays
inside the async collector snapshot/parse stage (`internal/collector` ->
`internal/parser`), never a query-serving or graph-write hot path. Even at 500
resources -- CloudFormation's own hard per-stack resource limit, so this is the
worst-case partition a single template can ever present, not merely a
"large" pick -- the absolute added cost is ~4.6ms per file. Non-CFN YAML files
(the overwhelming majority of a repository's manifests) are provably unaffected:
`decodeDocumentNodes` is only reachable after `cloudformation.IsTemplate`
matches, confirmed by `TestParse...` cases in `language_test.go` and the
detection guard in `language.go`'s `Parse`.

Observability Evidence: every degraded per-entity position (an unresolved alias
target, an unattributable merge key, or a missing raw-node match) is counted
through `eshu_dp_cloudformation_position_fallback_total`
(`internal/telemetry/instruments.go`, wired at
`internal/collector/git_snapshot_parse_partitions.go:451`), documented in
`docs/public/observability/telemetry-coverage.md`. An operator can use this
counter to see how often the added decode pass is not paying for a real
per-entity line gain.

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
layer can turn it into telemetry. JSON CloudFormation templates keep the
single document-root line_number and never get an end_line: JSON decoding
does not preserve per-key positions, tracked separately in issue #5348.

SanitizeTemplating is parser hygiene only. Do not treat it as a general
template evaluator.

Pub dependency rows are source evidence only. Hosted `pubspec.lock` rows carry
exact versions; hosted `pubspec.yaml` rows carry requested ranges. Git/path,
private-hosted, dependency override, and mismatched lockfile rows stay out of
the `dependency` contract so the reducer keeps missing evidence visible.

## Related docs

- docs/public/languages/support-maturity.md
- docs/public/architecture.md
