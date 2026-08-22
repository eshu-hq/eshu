// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import "time"

// DiscoveryAdvisoryReport is an operator-facing JSON-safe summary of the repo
// discovery and materialization shape that made one index run cheap or noisy.
type DiscoveryAdvisoryReport struct {
	SchemaVersion string                         `json:"schema_version"`
	GeneratedAt   time.Time                      `json:"generated_at"`
	Run           DiscoveryAdvisoryRun           `json:"run"`
	Summary       DiscoveryAdvisorySummary       `json:"summary"`
	TopNoisyDirs  []DiscoveryAdvisoryDirectory   `json:"top_noisy_directories,omitempty"`
	TopNoisyFiles []DiscoveryAdvisoryFile        `json:"top_noisy_files,omitempty"`
	EntityCounts  DiscoveryAdvisoryEntityCount   `json:"entity_counts"`
	SkipBreakdown DiscoveryAdvisorySkipBreakdown `json:"skip_breakdown"`
}

// DiscoveryAdvisoryRun identifies the run/scope context for one advisory.
type DiscoveryAdvisoryRun struct {
	Component    string `json:"component,omitempty"`
	RepoID       string `json:"repo_id,omitempty"`
	RepoPath     string `json:"repo_path"`
	SourceRunID  string `json:"source_run_id,omitempty"`
	ScopeID      string `json:"scope_id,omitempty"`
	GenerationID string `json:"generation_id,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
}

// DiscoveryAdvisorySummary contains low-cardinality counts for one snapshot.
type DiscoveryAdvisorySummary struct {
	DiscoveredFiles   int `json:"discovered_files"`
	ParsedFiles       int `json:"parsed_files"`
	ParseSkippedFiles int `json:"parse_skipped_files"`
	ContentFiles      int `json:"content_files"`
	ContentEntities   int `json:"content_entities"`
	SkippedDirs       int `json:"skipped_dirs"`
	SkippedFiles      int `json:"skipped_files"`
}

// DiscoveryAdvisoryDirectory reports the noisiest indexed directories by
// materialized entity count.
type DiscoveryAdvisoryDirectory struct {
	Path            string         `json:"path"`
	IndexedFiles    int            `json:"indexed_files"`
	ContentEntities int            `json:"content_entities"`
	EntityTypes     map[string]int `json:"entity_types,omitempty"`
}

// DiscoveryAdvisoryFile reports the noisiest indexed files by entity count.
type DiscoveryAdvisoryFile struct {
	Path            string         `json:"path"`
	ContentEntities int            `json:"content_entities"`
	Language        string         `json:"language,omitempty"`
	EntityTypes     map[string]int `json:"entity_types,omitempty"`
}

// DiscoveryAdvisoryEntityCount reports entity cardinality by type/language and
// source file kind. BySourceFileKind is keyed by the bounded telemetry
// SourceFileKind* values (code, package_manifest, config, other) and lets
// operators spot content_entity explosions from lockfiles or config files
// without querying fact_records directly.
type DiscoveryAdvisoryEntityCount struct {
	ByType           map[string]int `json:"by_type,omitempty"`
	ByLanguage       map[string]int `json:"by_language,omitempty"`
	BySourceFileKind map[string]int `json:"by_source_file_kind,omitempty"`
}

// DiscoveryAdvisorySkipBreakdown mirrors discovery skip telemetry without
// putting paths into metrics.
type DiscoveryAdvisorySkipBreakdown struct {
	DirsByName       map[string]int `json:"dirs_by_name,omitempty"`
	DirsByUser       map[string]int `json:"dirs_by_user,omitempty"`
	FilesByExtension map[string]int `json:"files_by_extension,omitempty"`
	FilesByContent   map[string]int `json:"files_by_content,omitempty"`
	FilesByUser      map[string]int `json:"files_by_user,omitempty"`
	FilesHidden      int            `json:"files_hidden,omitempty"`
	FilesGitignore   int            `json:"files_gitignore,omitempty"`
	FilesEshuIgnore  int            `json:"files_eshuignore,omitempty"`
	// TrackedFilesEshuIgnore counts files git tracks that repo-local
	// .eshuignore rules still skipped (issue #5591). Unlike .gitignore,
	// which #5591 makes defer to git's own tracked set, .eshuignore remains
	// a deliberate operator opt-out that CAN skip a tracked file. This is a
	// subset of FilesEshuIgnore, broken out so operators can distinguish "the
	// operator explicitly chose to keep this tracked file out of the index"
	// from an ordinary untracked-file eshuignore skip.
	TrackedFilesEshuIgnore int `json:"tracked_files_eshuignore,omitempty"`
}
