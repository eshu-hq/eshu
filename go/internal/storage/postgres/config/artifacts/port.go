// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import "context"

// Row is one stored config_artifact_paths row of a repository, name-free.
// RawPath is the cleaned path of the build with no repository name. For a
// NameScoped row the read step strips the repository-name prefix from RawPath
// (strip only, never a second clean); for any other row the path is RawPath.
// The read step then sets source_repo to the graph repository name. The
// row_digest key is not part of the port: it is defined in the schema only.
type Row struct {
	RawPath      string
	RelativePath string
	EvidenceKind string
	NameScoped   bool
}

// Reason says how a related repository is served. ReasonTrusted means the
// stored rows are exact for the current content and extractor; every other
// value means the repository is served live, and the value is the reason
// label of the reads counter.
type Reason string

const (
	// ReasonTrusted: serve the stored rows. An empty row set is a derived
	// repository with no config artifacts, not a missing one.
	ReasonTrusted Reason = ""
	// ReasonMissing: no state row, or no version row. With the reader's
	// inner join a missing version row never matches; only a repository
	// with no content since the seed lacks one, and its live answer is empty.
	ReasonMissing Reason = "missing"
	// ReasonStaleVersion: the state row was derived from an older content
	// version than the current one.
	ReasonStaleVersion Reason = "stale_version"
	// ReasonStaleExtractor: the state row was derived by another extractor
	// version than the reader's.
	ReasonStaleExtractor Reason = "stale_extractor"
	// ReasonFailed: no derive of this repository has succeeded yet
	// (row_count is NULL).
	ReasonFailed Reason = "failed"
)

// State is the trust-relevant part of one config_artifact_repo_state row.
// ContentVersion and RowCount are NULL (nil) until the first successful
// derive. FailureCount counts failed derives since the last success, and
// LastFailureExtractorVersion is the extractor version of the last failure.
type State struct {
	ContentVersion              *int64
	ExtractorVersion            int
	RowCount                    *int
	FailureCount                int
	LastFailureExtractorVersion *int
}

// Classify applies the reader's trust condition (protocol section 4) to one
// repository: state is its state row or nil, version is its current
// config_artifact_content_versions.version or nil, and extractorVersion is
// the reader's extractor version. It returns ReasonTrusted only when a derive
// has succeeded for exactly the current content version and extractor
// version. A later failed derive of the same content does not untrust the
// rows, because they are still exact; the walker retries it.
//
// Precedence when several reasons hold: missing, failed, stale_version,
// stale_extractor. The stored reader must classify with the same rule.
func Classify(state *State, version *int64, extractorVersion int) Reason {
	switch {
	case state == nil || version == nil:
		return ReasonMissing
	case state.RowCount == nil || state.ContentVersion == nil:
		return ReasonFailed
	case *state.ContentVersion != *version:
		return ReasonStaleVersion
	case state.ExtractorVersion != extractorVersion:
		return ReasonStaleExtractor
	default:
		return ReasonTrusted
	}
}

// Repository is the stored-read answer for one related repository. Rows is
// meaningful only when Reason is ReasonTrusted; otherwise the caller serves
// the repository live and counts Reason.
type Repository struct {
	RepoID string
	Reason Reason
	Rows   []Row
}

// Reader is the read-model port the story and context routes read stored
// config-artifact rows through (decision record section 3.3, PR3c).
//
// ReadStored takes the access-filtered set of related repository ids and
// never reads a repository outside it (#5167 W3 P0). It answers with one
// statement and one snapshot, returns exactly one Repository per distinct
// requested id, in request order, and classifies each with Classify against
// extractorVersion. An error fails the whole read; the caller then serves
// every repository live. The Postgres implementation lands with the reader
// in PR3c; PR3b-1a defines the contract only.
type Reader interface {
	ReadStored(ctx context.Context, repoIDs []string, extractorVersion int) ([]Repository, error)
}
