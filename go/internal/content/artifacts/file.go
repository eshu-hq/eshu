// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import "context"

// File is one stored repository file as the config-artifact extractors read
// it: a row of the content_files table. The package defines its own type so a
// caller in storage/postgres does not inherit the dependencies of the query
// contract. The live query reader adapts querycontract.FileContent to it with
// one field-wise copy.
//
// Content is empty on a list row. The list statement selects an empty string
// in its place, so a list moves no content bytes.
type File struct {
	// RepoID is the repository the file belongs to.
	RepoID string
	// RelativePath is the file path inside the repository.
	RelativePath string
	// CommitSHA is the commit the stored content came from, or empty.
	CommitSHA string
	// Content is the stored text. Empty on a list row.
	Content string
	// ContentHash is the stored content hash.
	ContentHash string
	// LineCount is the stored line count.
	LineCount int
	// Language is the stored language label, or empty.
	Language string
	// ArtifactType is the stored artifact classification, or empty.
	ArtifactType string
}

// Reader is the content_files read port of the per-repository config-artifact
// function. The four statements it stands for are defined once in
// statements.go, so the live query reader and a derive in storage/postgres
// cannot drift in ORDER BY, the artifact_type mapping, or the blank predicate.
// The single-row statement (GetFileStatement) has no method here: the
// needed-content pass reads in chunks, and only the anchor repository's
// full pass reads one file at a time.
//
// Implementations must be safe for the bounded concurrent chunk reads of one
// repository (at most ChunksInFlight calls to ReadFiles at once).
type Reader interface {
	// ListFiles returns up to limit file rows of one repository in
	// relative_path order, with empty Content (ListStatement).
	ListFiles(ctx context.Context, repoID string, limit int) ([]File, error)
	// ReadFiles returns the stored rows, content included, for the given
	// paths in one statement (GetFilesStatement). A path with no stored row is
	// absent. The result order is unspecified. A failure returns no rows.
	ReadFiles(ctx context.Context, repoID string, relativePaths []string) ([]File, error)
	// NonBlankPaths returns the subset of relativePaths whose stored content
	// is not blank under strings.TrimSpace (NonBlankPathsStatement). No
	// content bytes cross the wire.
	NonBlankPaths(ctx context.Context, repoID string, relativePaths []string) ([]string, error)
}
