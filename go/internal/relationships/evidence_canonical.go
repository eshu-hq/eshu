// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// Canonical projects one relationship EvidenceFact into the unified
// truth.Evidence record (issue #3489). It preserves the existing confidence
// (clamped to [0,1]) and lifts the byte-level citation that the relationship
// model previously buried in the free-form Details map: the file path lives at
// Details["path"], and optional line/hash/commit refinements come from
// Details["start_line"], Details["end_line"], Details["content_hash"], and
// Details["commit_sha"] when discovery captured them. When no file path is
// present, the source entity identity is used as the citation locator so the
// resulting Evidence still validates.
//
// This is a non-destructive projection: EvidenceFact remains the durable
// relationship model. Canonical lets relationship evidence speak the unified
// contract for callers that need confidence and citation together.
func (f EvidenceFact) Canonical() truth.Evidence {
	citation := truth.Citation{
		ContentHash: detailsString(f.Details, "content_hash"),
		CommitSHA:   detailsString(f.Details, "commit_sha"),
		StartLine:   detailsInt(f.Details, "start_line"),
		EndLine:     detailsInt(f.Details, "end_line"),
		ByteOffset:  detailsInt(f.Details, "byte_offset"),
		ByteLength:  detailsInt(f.Details, "byte_length"),
	}
	if path := detailsString(f.Details, "path"); path != "" && f.SourceRepoID != "" {
		citation.RepoID = f.SourceRepoID
		citation.RelativePath = path
	} else if f.SourceEntityID != "" {
		citation.EntityID = f.SourceEntityID
	} else if f.SourceRepoID != "" {
		// Repo-only locator: cite the repository root via its identity.
		citation.EntityID = f.SourceRepoID
	}

	return truth.Evidence{
		Kind:       string(f.EvidenceKind),
		Confidence: clampConfidence(f.Confidence),
		Citation:   citation,
		Provenance: truth.Provenance{
			Basis:     truth.ProvenanceBasisSourceContent,
			Rationale: f.Rationale,
			Source:    detailsString(f.Details, "extractor"),
		},
	}
}

// detailsString reads a string-valued key from a relationship Details map.
func detailsString(details map[string]any, key string) string {
	if details == nil {
		return ""
	}
	value, _ := details[key].(string)
	return value
}

// detailsInt reads an int-valued key from a relationship Details map. JSON
// round-tripping stores numbers as float64, so both int and float64 are
// accepted.
func detailsInt(details map[string]any, key string) int {
	if details == nil {
		return 0
	}
	switch typed := details[key].(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
