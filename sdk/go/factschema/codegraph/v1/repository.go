// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Repository is the schema-version-1 typed payload for the "repository" fact
// kind (Contract System v1 §3.1, docs/internal/design/contract-system-v1.md).
//
// A repository fact is the git collector's once-per-generation repository
// summary (go/internal/collector/git_fact_builder.go repositoryFactEnvelope).
// RepoID is the join identity code-graph-core reducer handlers key
// repository-scoped intents on
// (go/internal/reducer/code_call_materialization_intents.go
// buildCodeCallProjectionContexts, buildCodeCallDeltaFileScopesByRepoID). It
// is required for the same reason as File.RepoID: a fact missing it must
// dead-letter, not silently collapse to an empty-string acceptance-unit key.
//
// The required set tracks what the reducer READS for identity, not what the
// collector always emits. RepoID is the ONLY required field: the reducer read
// sites consume RepoID (required), plus SourceRunID, LocalPath,
// DeltaRelativePaths, and DeltaDeletedRelativePaths (all optional). GraphID,
// GraphKind, Name, ParsedFileCount, and IsDependency are unconditionally
// emitted but no code-graph-core reducer read site consumes them, so requiring
// them would dead-letter usable graph truth — the wrong contract. GitRefs and
// DefaultBranch are read only by the projector, not the reducer, and are
// schema-declared (not required) so a future schema change cannot silently
// break the projector read.
type Repository struct {
	// RepoID is the repository's canonical id. Required: it is the
	// acceptance-unit key every code-graph-core shared-projection intent is
	// built against, read by buildCodeCallProjectionContexts and
	// buildCodeCallDeltaFileScopesByRepoID.
	RepoID string `json:"repo_id"`

	// GraphID is the canonical graph node identity the collector assigns this
	// repository (its RepoID). Optional: no reducer read site consumes it.
	GraphID *string `json:"graph_id,omitempty"`

	// GraphKind discriminates this envelope's graph node kind (the emitter
	// always writes the literal "repository"). Optional: no reducer read site
	// consumes it.
	GraphKind *string `json:"graph_kind,omitempty"`

	// Name is the repository's display name. Optional: unconditionally emitted
	// but not read by any code-graph-core reducer read site.
	Name *string `json:"name,omitempty"`

	// ParsedFileCount is the repository's parsed-file count, formatted as a
	// decimal string by the emitter (fmt.Sprintf("%d", parsedFileCount)) —
	// deliberately a STRING on the wire, not an int; do not retype this to a
	// numeric field. Optional: unconditionally emitted but not read by any
	// code-graph-core reducer read site, so requiring it would dead-letter a
	// fact the reducer can fully process from RepoID alone.
	ParsedFileCount *string `json:"parsed_file_count,omitempty"`

	// IsDependency reports whether this repository was observed as a
	// dependency of another repository rather than a primary source. Optional:
	// unconditionally emitted but not read by any code-graph-core reducer read
	// site.
	IsDependency *bool `json:"is_dependency,omitempty"`

	// RepoSlug is the repository's slug identifier, when the source metadata
	// carries one. Optional: repositoryFactEnvelope writes this key only when
	// repo.RepoSlug is non-empty.
	RepoSlug *string `json:"repo_slug,omitempty"`

	// RemoteURL is the repository's remote URL, when observed. Optional:
	// written only when repo.RemoteURL is non-empty.
	RemoteURL *string `json:"remote_url,omitempty"`

	// LocalPath is the repository's local checkout path, when observed.
	// Optional: written only when repo.LocalPath is non-empty.
	LocalPath *string `json:"local_path,omitempty"`

	// DefaultBranch is the name of the ref flagged as this repository's
	// default branch. Optional: derived from GitRefs by
	// repositoryDefaultBranch and written only when a default branch was
	// resolved. Schema-declared even though only the projector reads it
	// (go/internal/projector/canonical_*), matching the incident-family
	// SQL-loader-only field precedent (sdk/go/factschema/AGENTS.md).
	DefaultBranch *string `json:"default_branch,omitempty"`

	// imports_map (the repository-level import graph the parser resolved,
	// import specifier -> resolving file paths) is deliberately NOT modeled as
	// a typed field. Its wire shape is map[string][]string, which renders as a
	// JSON Schema "additionalProperties" of array type — a shape the collector
	// conformance validator's supported subset rejects
	// (sdk/go/collector/conformance/payload_schema.go: additionalProperties
	// value type "array" is not supported). No code-graph-core reducer read
	// site consumes imports_map, so it passes through the open top-level object
	// (additionalProperties: true) untyped, exactly like an aws_resource
	// unmodeled key, rather than being typed into a conformance-incompatible
	// schema. Model it only if a consumer needs it AND the conformance subset
	// grows array-valued additionalProperties support.

	// GitRefs lists every source-observed branch head the collector captured
	// for this repository. Optional: written only when at least one ref
	// resolves. Schema-declared for the same reason as DefaultBranch — the
	// projector reads it, not the reducer, but the #4573 payload-usage
	// manifest gate only sees reducer decode calls, so declaring it here keeps
	// the schema honest.
	GitRefs []GitRef `json:"git_refs,omitempty"`

	// DeltaGeneration reports whether this fact was emitted for a delta
	// (incremental) generation rather than a full repository scan. Optional:
	// a pointer so "not a delta" (absent) stays distinct from an observed
	// false; the emitter only ever writes this key as literal true.
	DeltaGeneration *bool `json:"delta_generation,omitempty"`

	// ReconciliationGeneration reports whether this fact was emitted for a
	// reconciliation pass. Optional, same nil-vs-false rationale as
	// DeltaGeneration.
	ReconciliationGeneration *bool `json:"reconciliation_generation,omitempty"`

	// DeltaRelativePaths lists the repo-relative paths changed in this delta
	// generation. Optional: written only alongside DeltaGeneration.
	DeltaRelativePaths []string `json:"delta_relative_paths,omitempty"`

	// DeltaDeletedRelativePaths lists the repo-relative paths deleted in this
	// delta generation. Optional: written only alongside DeltaGeneration.
	DeltaDeletedRelativePaths []string `json:"delta_deleted_relative_paths,omitempty"`

	// SourceRunID is the ingestion run id that produced this fact. Optional:
	// written only when the source run id is non-blank. Read directly by
	// buildCodeCallProjectionContexts to build each repository's
	// ProjectionContext.
	SourceRunID *string `json:"source_run_id,omitempty"`
}

// GitRef is the closed sub-struct for one source-observed Git reference head,
// mirroring go/internal/collector.GitRef and the wire shape
// repositoryFactGitRefsPayload emits. It carries no Attributes pass-through:
// the collector's ref summary is a small, fully-modeled shape.
type GitRef struct {
	// Name is the branch name. Required: repositoryFactGitRefsPayload skips
	// any ref whose name is blank before emission.
	Name string `json:"name"`

	// Kind is the ref kind (for example "branch"). Required: the emitter
	// defaults blank kinds to "branch" before emission, so a git_refs entry
	// always carries one.
	Kind string `json:"kind"`

	// HeadSHA is the ref's observed head commit SHA. Required:
	// repositoryFactGitRefsPayload skips any ref whose head SHA is blank.
	HeadSHA string `json:"head_sha"`

	// IsDefault reports whether this ref is the repository's default branch.
	// Required: the emitter always writes this boolean, even when false.
	IsDefault bool `json:"is_default"`
}
