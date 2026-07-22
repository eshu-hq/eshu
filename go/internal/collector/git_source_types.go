// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RepositorySelector selects the repositories for one collector cycle.
type RepositorySelector interface {
	SelectRepositories(context.Context) (SelectionBatch, error)
}

// RepositorySnapshotter collects one narrowed parser/snapshot payload for one
// selected repository.
type RepositorySnapshotter interface {
	SnapshotRepository(context.Context, SelectedRepository) (RepositorySnapshot, error)
}

// SelectionBatch is one narrowed repository-selection batch for Go-owned fact
// shaping.
type SelectionBatch struct {
	ObservedAt   time.Time
	Repositories []SelectedRepository
}

// SelectedRepository is one repository chosen for the current collector cycle.
type SelectedRepository struct {
	RepoPath string `json:"repo_path"`
	// GitTreePath is the local Git checkout used for committed-tree reads when
	// RepoPath is a managed filesystem copy without its source .git directory.
	// Empty means RepoPath itself is the Git tree source.
	GitTreePath          string   `json:"git_tree_path,omitempty"`
	RemoteURL            string   `json:"remote_url"`
	IsDependency         bool     `json:"is_dependency"`
	DisplayName          string   `json:"display_name"`
	Language             string   `json:"language"`
	FileTargets          []string `json:"file_targets"`
	GitRefs              []GitRef `json:"git_refs,omitempty"`
	Delta                bool     `json:"delta,omitempty"`
	DeletedRelativePaths []string `json:"deleted_relative_paths,omitempty"`
	// Reconcile marks a forced full reconciliation observation whose generation
	// must bypass the freshness-hint skip so it always re-projects and retracts
	// any drift the delta path missed (epic #2340).
	Reconcile bool `json:"reconcile,omitempty"`
	// SourceCommitSHA carries the source commit resolved by the git-sync path
	// after checkoutRemoteBranch completed. It is populated only when Eshu's own
	// sync resolved and checked out a known remote SHA (git-sync selector modes
	// "explicit" and "githubOrg"). Non-sync selectors (filesystem, clone, and
	// any path that did not run checkoutRemoteBranch with a freshly-resolved
	// remote SHA) leave this empty. Snapshot code uses this to skip a redundant
	// git rev-parse HEAD subprocess when the SHA is already known.
	SourceCommitSHA string `json:"source_commit_sha,omitempty"`
	// Ref names a non-default git ref (branch or tag) being snapped for this
	// repository. When set, the scope identity becomes repo_id@ref and emitted
	// facts carry a ref payload field so multi-ref coverage is distinguishable.
	// Empty on the default-branch selection (canonical identity unchanged).
	// Enabler for epic #5393 / issue #5417.
	Ref string `json:"ref,omitempty"`
}

// RepositorySnapshot captures one repository parse snapshot and content transport.
type RepositorySnapshot struct {
	RepoPath    string `json:"repo_path"`
	GitTreePath string `json:"git_tree_path,omitempty"`
	RemoteURL   string `json:"remote_url"`
	FileCount   int    `json:"file_count"`
	// HeadCommitSHA is the Git commit the snapshot content reflects (the
	// checked-out HEAD). It becomes ScopeGeneration.SourceCommitSHA, the durable
	// baseline a later delta sync diffs against. Empty for non-git snapshots.
	HeadCommitSHA   string                  `json:"head_commit_sha,omitempty"`
	ImportsMap      map[string][]string     `json:"imports_map"`
	FileData        []map[string]any        `json:"file_data"`
	ContentFiles    []ContentFileSnapshot   `json:"content_files"`
	ContentEntities []ContentEntitySnapshot `json:"content_entities"`
	// GitRefs carries source-observed branch/ref heads for this repository.
	GitRefs []GitRef `json:"git_refs,omitempty"`
	// TerraformStateCandidates carries metadata-only repo-local state-file
	// candidates. Raw .tfstate bytes never enter repository snapshots.
	TerraformStateCandidates []TerraformStateCandidate `json:"terraform_state_candidates,omitempty"`
	// DiscoveryAdvisory summarizes noisy repo discovery and materialization
	// shapes for focused operator tuning.
	DiscoveryAdvisory *DiscoveryAdvisoryReport `json:"discovery_advisory,omitempty"`
	// ContentFileMetas holds body-free file metadata for two-phase snapshots.
	// When populated, streamFacts re-reads bodies from AbsolutePath at emit time
	// instead of carrying all bodies in memory.
	ContentFileMetas []ContentFileMeta `json:"content_file_metas,omitempty"`
	// DocumentationFileMetas holds body-free repository documentation metadata
	// for files that should emit documentation facts without parser content rows.
	DocumentationFileMetas []ContentFileMeta `json:"documentation_file_metas,omitempty"`
	// Delta marks snapshots that contain only file-scoped changes from a Git
	// resync rather than a full repository view.
	Delta bool `json:"delta,omitempty"`
	// DeltaRelativePaths holds every repo-relative path touched by a delta
	// generation, including deleted paths that have no parsed file payload.
	DeltaRelativePaths []string `json:"delta_relative_paths,omitempty"`
	// DeletedRelativePaths holds repo-relative paths that disappeared between
	// Git revisions and must be retracted from content and graph projections.
	DeletedRelativePaths []string `json:"deleted_relative_paths,omitempty"`
	// Reconcile marks a forced full reconciliation snapshot. Its generation
	// carries an empty freshness hint so the commit-time skip never elides it,
	// guaranteeing a periodic full re-projection that retracts drift the delta
	// path missed (epic #2340).
	Reconcile bool `json:"reconcile,omitempty"`
	// TaintEvidence carries intraprocedural value-flow taint findings resolved to
	// the graph Function entity they concern. Empty unless the parser emitted
	// taint_findings (gated by ESHU_EMIT_DATAFLOW), so the snapshot is
	// byte-identical when the value-flow gate is off. It is evidence with
	// confidence and provenance, never canonical truth.
	TaintEvidence []TaintEvidenceSnapshot `json:"taint_evidence,omitempty"`
	// InterprocTaintEvidence carries cross-function value-flow findings, each
	// resolved to the source and sink Function entities it spans. Empty unless the
	// parser emitted interproc_findings (gated by ESHU_EMIT_DATAFLOW); byte-
	// identical when off. Evidence, never canonical truth.
	InterprocTaintEvidence []InterprocTaintEvidenceSnapshot `json:"interproc_taint_evidence,omitempty"`
	// FunctionSummaries carries each function's raw value-flow Effects read from
	// the parser's dataflow_summaries bucket, keyed by the durable FunctionID. The
	// reducer persists them to the function-summary store for cross-repo
	// composition. Empty unless the parser emitted dataflow_summaries (gated by
	// ESHU_EMIT_DATAFLOW and a supplied RepositoryID); byte-identical when off.
	FunctionSummaries []FunctionSummarySnapshot `json:"function_summaries,omitempty"`
	// FunctionSources carries each function's param-level value-flow taint sources
	// read from the parser's dataflow_sources bucket. The reducer persists them as
	// interproc source ports for the cross-repo fixpoint. Empty unless the parser
	// emitted dataflow_sources; byte-identical when off.
	FunctionSources []FunctionSourceSnapshot `json:"function_sources,omitempty"`
	// DataflowFunctions carries raw per-function CFG, reaching-definition, and
	// control-dependence parser facts read from the dataflow_functions bucket.
	// These exact parser facts back API/MCP code-flow summaries without re-running
	// parsers. Empty unless the value-flow gate emitted dataflow_functions.
	DataflowFunctions []DataflowFunctionSnapshot `json:"dataflow_functions,omitempty"`
	// DataflowCatalogVersions carries parser-emitted value-flow catalog content
	// hashes. It is folded into snapshot freshness so catalog-only changes force
	// re-evaluation even when a file produces no findings. Empty when the
	// dataflow gate is off.
	DataflowCatalogVersions []DataflowCatalogVersionSnapshot `json:"dataflow_catalog_versions,omitempty"`
	// DataflowScanned records that the value-flow gate (ESHU_EMIT_DATAFLOW) was on
	// for this snapshot, independent of whether TaintEvidence or
	// InterprocTaintEvidence produced any findings. It drives a per-generation
	// marker fact so the reducer reconciles (and retracts stale) value-flow
	// evidence even when the current finding set is empty. False — and omitted —
	// when the gate is off, preserving the byte-identical-when-off guarantee.
	DataflowScanned bool `json:"dataflow_scanned,omitempty"`
}

// DataflowCatalogVersionSnapshot is one parser-emitted value-flow catalog
// content hash used only for freshness.
type DataflowCatalogVersionSnapshot struct {
	Language string `json:"language"`
	Catalog  string `json:"catalog"`
	Version  string `json:"version"`
}

// TaintEvidenceSnapshot is one intraprocedural value-flow taint finding resolved
// to the graph Function entity it concerns. The finding-to-entity join is done
// here in the collector, where the parse payload carries both the function
// entities and the findings, so the reducer can project the evidence against the
// Function node by uid without re-resolving names.
type TaintEvidenceSnapshot struct {
	FunctionUID  string  `json:"function_uid"`
	RelativePath string  `json:"relative_path"`
	FunctionName string  `json:"function_name"`
	Language     string  `json:"language"`
	Kind         string  `json:"kind"`
	SinkKind     string  `json:"sink_kind"`
	SourceKind   string  `json:"source_kind"`
	Binding      string  `json:"binding"`
	SourceLine   int     `json:"source_line"`
	SinkLine     int     `json:"sink_line"`
	Confidence   float64 `json:"confidence"`
	ClassContext string  `json:"class_context,omitempty"`
	SinkLabel    string  `json:"sink_label,omitempty"`
	SourceLabel  string  `json:"source_label,omitempty"`
	GuardReason  string  `json:"guard_reason,omitempty"`
}

// InterprocTaintEvidenceSnapshot is one cross-function value-flow finding
// resolved to the source and sink Function entities it spans. Both endpoints are
// resolved here in the collector by function name within the file (the parser's
// FunctionID carries the name but not the graph uid), so the reducer can project
// a source->sink evidence edge without re-resolving.
type InterprocTaintEvidenceSnapshot struct {
	SourceFunctionUID  string           `json:"source_function_uid"`
	SinkFunctionUID    string           `json:"sink_function_uid"`
	RelativePath       string           `json:"relative_path"`
	SourceFunctionName string           `json:"source_function_name"`
	SinkFunctionName   string           `json:"sink_function_name"`
	Language           string           `json:"language"`
	SinkKind           string           `json:"sink_kind"`
	SourceKind         string           `json:"source_kind"`
	Confidence         float64          `json:"confidence"`
	Cloud              bool             `json:"cloud,omitempty"`
	WhyTrail           []map[string]any `json:"why_trail,omitempty"`
	WhyTrailTruncated  bool             `json:"why_trail_truncated,omitempty"`
}

// ContentFileSnapshot captures one portable file-content record.
type ContentFileSnapshot struct {
	RelativePath    string `json:"relative_path"`
	Body            string `json:"content_body"`
	Digest          string `json:"content_digest"`
	Language        string `json:"language"`
	ArtifactType    string `json:"artifact_type"`
	TemplateDialect string `json:"template_dialect"`
	IACRelevant     *bool  `json:"iac_relevant"`
	CommitSHA       string `json:"commit_sha"`
}

// ContentFileMeta captures file metadata without the body string.
// Used in the two-phase snapshot architecture: Phase A collects metadata
// during parse/materialize (bodies temporary), Phase B re-reads bodies from
// disk during fact streaming so memory stays O(single_file) not O(repo).
type ContentFileMeta struct {
	RelativePath    string `json:"relative_path"`
	Digest          string `json:"content_digest"`
	Language        string `json:"language"`
	ArtifactType    string `json:"artifact_type"`
	TemplateDialect string `json:"template_dialect"`
	IACRelevant     *bool  `json:"iac_relevant"`
	CommitSHA       string `json:"commit_sha"`
}

// ContentEntitySnapshot captures one portable content-entity record.
type ContentEntitySnapshot struct {
	EntityID        string         `json:"entity_id"`
	RelativePath    string         `json:"relative_path"`
	EntityType      string         `json:"entity_type"`
	EntityName      string         `json:"entity_name"`
	StartLine       int            `json:"start_line"`
	EndLine         int            `json:"end_line"`
	StartByte       *int           `json:"start_byte"`
	EndByte         *int           `json:"end_byte"`
	Language        string         `json:"language"`
	ArtifactType    string         `json:"artifact_type"`
	TemplateDialect string         `json:"template_dialect"`
	IACRelevant     *bool          `json:"iac_relevant"`
	SourceCache     string         `json:"source_cache"`
	Metadata        map[string]any `json:"metadata"`
	IndexedAt       time.Time      `json:"indexed_at"`
}

// GitSource converts narrowed snapshot batches into durable collector
// generations. Generations are streamed through a bounded channel so memory
// stays proportional to the channel buffer size, not the total number of
// repositories.
type GitSource struct {
	Component       string
	Selector        RepositorySelector
	Snapshotter     RepositorySnapshotter
	Tracer          trace.Tracer
	Instruments     *telemetry.Instruments
	Logger          *slog.Logger
	SnapshotWorkers int

	// LargeRepoThreshold is the file-count threshold above which a repository
	// is classified as "large" and must acquire the large-repo semaphore before
	// snapshotting. Default: 500.
	LargeRepoThreshold int
	// LargeRepoMaxConcurrent is the maximum number of large repositories that
	// can be snapshotted concurrently. Small repositories bypass this limit
	// entirely. Default: 2.
	//
	// Tuning guide:
	//   1 = safest for memory; only one large parse at a time
	//   2 = good balance; two large repos + remaining workers on small repos
	//   4 = aggressive; requires more RAM but faster on large-heavy workloads
	//
	// Set via ESHU_LARGE_REPO_MAX_CONCURRENT environment variable.
	LargeRepoMaxConcurrent int

	// StreamBuffer controls the generation stream channel buffer size.
	// When 0 (default), the buffer equals the worker count so completed
	// small-repo snapshots don't block behind slow large-repo commits.
	// Each buffered generation holds metadata and a fact channel reference;
	// file bodies are re-read from disk via two-phase streaming.
	//
	// Set via ESHU_STREAM_BUFFER environment variable.
	StreamBuffer int

	// Streaming state, lazily initialized on first Next call.
	// The channel carries one CollectedGeneration at a time; the coordinator
	// goroutine closes it when all workers finish or on first error.
	stream    chan CollectedGeneration
	streamErr error
	started   bool
}

// Next returns one Go-shaped collected generation, streaming from background
// snapshot workers. On the first call it launches background goroutines that
// discover repos, snapshot them concurrently, and feed results through a
// bounded channel. Subsequent calls read one generation at a time.
//
// When the current batch is fully consumed the stream resets so the next call
