# YAML Parser

## Purpose

internal/parser/yaml owns YAML-family source extraction for Kubernetes,
Argo CD, Crossplane, Kustomize, Helm, and CloudFormation/SAM payload rows. It
exists so YAML parsing behavior can evolve behind a language-owned package
without depending on the parent parser dispatcher. It also emits metadata-only
declared Grafana observability rows from Helm values, GrafanaFolder and
GrafanaDashboard resources, dashboard ConfigMaps, folder, datasource, and alert
provisioning.

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

## Gotchas / invariants

Output ordering is part of the parser fact contract. Parse sorts every emitted
bucket before returning.

Helm template manifests are intentionally skipped after source preservation
because templated chart manifests are rendered elsewhere; Chart.yaml and values
files still emit Helm metadata. `values.yaml` files may also emit declared
Grafana observability metadata, but they do not prove applied or live provider
state.

Declared Grafana observability rows never store dashboard JSON, panel query
bodies, datasource URLs, secure datasource values, alert model bodies, contact
addresses, folder titles, provisioning paths, or private routing values. Unsafe
values are omitted and represented by fingerprints, redaction fields, or
coverage warnings.

YAML intrinsic tags such as Ref and Sub are converted to the decoded shapes
expected by the CloudFormation parser before template extraction.

SanitizeTemplating is parser hygiene only. Do not treat it as a general
template evaluator.

Pub dependency rows are source evidence only. Hosted `pubspec.lock` rows carry
exact versions; hosted `pubspec.yaml` rows carry requested ranges. Git/path,
private-hosted, dependency override, and mismatched lockfile rows stay out of
the `dependency` contract so the reducer keeps missing evidence visible.

## Related docs

- docs/public/languages/support-maturity.md
- docs/public/architecture.md
