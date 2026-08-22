// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gitrepo is Eshu's git repository collector: it selects repositories,
// snapshots their working trees, and streams the facts that snapshot produces.
//
// It implements collector.Source. The root collector package owns the seam
// every collector kind plugs into (Service, Source, Committer, CollectedGeneration
// and the claimed-work machinery); this package owns everything specific to
// collecting from a git repository.
//
// Three families make up the core and move as one unit: repository selection
// (git_selection_*), working-tree snapshotting (git_snapshot_*), and the source
// that drives them (git_source*). They form a measured three-way production
// import cycle, so they cannot be split further without a dependency-inversion
// refactor that has not been done. The leaf emitters that CAN stand alone —
// gitdocs, gitobs, gitcodeowners, gitsubmodule, gitsvccatalog, gittfstate and
// workflowimage — are nested subpackages, with everything they share below them
// in gitmodel.
//
// Snapshot input shape is this package's contract. Git-backed selection captures
// source-observed branch/ref heads so downstream query routes can expose branch
// selectors without inventing names. Raw Terraform-state bytes never enter a
// repository snapshot; only metadata-only state candidates are emitted for the
// Terraform-state collector path to approve and read.
//
// Full Git snapshots emit reducer follow-ups for shell-exec materialization
// alongside the existing workload, code-call, deployment, SQL, and inheritance
// follow-ups. Full and delta Git generations emit one unconditional
// rationale-materialization follow-up after their content-entity facts,
// including generations with no current rationale comments, so downstream
// reconciliation can retract stale EXPLAINS edges.
//
// Documentation extraction lives in the gitdocs subpackage; see its doc.go for
// the format list and the metadata-only boundaries (DOCX comments and tracked
// changes, legacy XLS, PPTX hidden slides and speaker notes). Prose surfaces may
// emit non-authoritative document-evidence claim candidates, but API contract
// operations, schemas, channels, GraphQL SDL fields, spreadsheet cells, slide
// text, archive membership, and diagram labels or links remain documentation
// evidence; they do not prove service ownership. Declared Grafana,
// Prometheus/Mimir, Loki, and Tempo observability rows plus applied
// Argo CD/Kubernetes observability state rows become metadata-only observability
// source facts in the gitobs subpackage; reducers and query surfaces own any
// later declared/applied/observed coverage truth.
//
// SCIP indexing is opt-in: SCIP_INDEXER=1/true/yes/on enables it when a selected
// file group's external scip-* binary is available. Unset, unrecognized,
// false/off/0/no values keep native-only parsing. SCIP groups are planned by
// bounded language priority and package/workspace root, then run through a
// bounded worker pool before supplementing native parser output with call facts
// for matching files only. SCIP must not shrink the discovered parser file set:
// files selected by discovery but omitted from index.scip still parse through
// the native parser and emit normal content facts. Value-flow catalog content
// hashes are freshness-only snapshot metadata: they retrigger gated taint
// analysis when matcher rules change without streaming extra facts or changing
// gate-off snapshots.
//
// No-Regression Evidence:
// `TestLoadSnapshotSCIPConfigDefaultsDisabledForTopLanguageList` covers the
// native-only default, and `TestSCIPSnapshotKeepsSelectedFilesMissingFromIndex`
// covers an explicitly enabled SCIP snapshot where one selected Python file is
// missing from SCIP output and still emits native parser metadata.
//
// Observability Evidence: the completeness guard reuses the existing
// `collector snapshot stage completed` parse summary,
// `eshu_dp_file_parse_duration_seconds`, file parsed counters, fact emission
// signals, `eshu_dp_scip_snapshot_attempts_total` outcome counter, and
// `eshu_dp_scip_process_wait_seconds` process-slot wait histogram. SCIP binary,
// indexer, and parser fallback reasons are logged with bounded language,
// reason, and failure_class fields; limiter slot acquisition logs bounded
// language and wait_seconds fields. The path adds no worker, queue, graph write,
// status field, span, or runtime setting.
package gitrepo
