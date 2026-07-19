// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package yaml extracts YAML-family parser payloads without depending on the
// parent parser dispatch package.
//
// Parse reads one YAML source file and emits the payload buckets consumed by
// the parent parser and content materializer: Kubernetes resources, Argo CD
// applications, Crossplane resources, Kustomize overlays, Helm chart metadata,
// Helm values metadata, Pub dependency rows, CloudFormation/SAM template rows,
// and Atlantis repo-level project rows (one AtlantisProject row per project in
// atlantis.yaml, dispatched by filename since the config carries no
// apiVersion/kind). GitLab CI pipelines are likewise dispatched by filename:
// one GitlabPipeline row per .gitlab-ci.yml plus one GitlabJob row per top-level
// job (hidden/template jobs and reserved global keywords excluded). DecodeDocuments and SanitizeTemplating remain available for parent
// compatibility paths that decode YAML-side metadata. Argo CD Application rows preserve the legacy
// singular source fields while adding positional source tuple fields that keep
// repo, path, revision, and root values aligned by source index. The package
// also emits metadata-only declared Grafana, Prometheus/Mimir, Loki, and Tempo
// observability rows from Helm values, Grafana resources, dashboard ConfigMaps,
// provisioning files, Prometheus Operator resources, Promtail client routes,
// OTel metric, log, and trace pipelines, OTel Prometheus receiver scrape
// configs, Loki gateway values, Tempo gateway values, Grafana Tempo datasource
// links, and chart ServiceMonitor settings while omitting dashboard JSON, query
// bodies, scrape targets, remote-write URLs, Loki and Tempo route URLs, tenant
// header values, tenant IDs, datasource URLs, secrets, contact routes, folder
// titles, provisioning paths, log label values, spans, traces, raw trace IDs,
// request attributes, TraceQL bodies, and trace tag values. Argo CD Application
// status and Kubernetes API-exported observability resources may also emit
// applied-state metadata, but raw status messages, labels, managed fields, UIDs,
// cluster URLs, dashboard bodies, query bodies, and Secret data stay out of
// parser payload rows. The package keeps output deterministic by sorting
// emitted buckets and by routing decoded CloudFormation documents through the
// shared CloudFormation parser contract.
//
// For a CloudFormation/SAM document, the package additionally walks the raw
// gopkg.in/yaml.v3 node tree (cloudformationPositionsFromRoot) to give the
// shared CloudFormation parser real per-entity line_number/end_line values
// instead of the single document-root line every Parameters/Conditions/
// Resources/Outputs entity previously shared (issue #5328). The walk is
// anchored strictly at the document root's own top-level section pairs, never
// by searching for a key name anywhere in the tree, and resolves anchors,
// aliases, and `<<:` merge keys. A degraded position -- an unresolvable
// section or an entity the walk could not attribute -- falls back to the
// section header's own line (never a fabricated per-entity guess) and is
// recorded as a cloudformation_position_fallbacks row for the collector layer
// to turn into telemetry; the entity itself is never dropped. JSON
// CloudFormation templates are unaffected: JSON decoding does not preserve
// per-key positions, a gap tracked separately in issue #5348.
package yaml
