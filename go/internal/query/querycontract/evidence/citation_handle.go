// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

// EvidenceCitationHandle is a caller-supplied reference to one piece of
// evidence (a file location or a graph entity) that an evidence-citation or
// visualization-packet response resolves and cites back.
//
// This type moved here from root package query's evidence_citation.go
// (#6060) and on into this leaf (#6597). The parent querycontract's
// visualization and answer packets carry it, so it sits below them. Root still
// publishes the exported alias query.EvidenceCitationHandle in
// evidence_citation_public.go for serviceintel; every field stays exported and
// unchanged.
type EvidenceCitationHandle struct {
	Kind           string  `json:"kind,omitempty"`
	RepoID         string  `json:"repo_id,omitempty"`
	RelativePath   string  `json:"relative_path,omitempty"`
	EntityID       string  `json:"entity_id,omitempty"`
	EvidenceFamily string  `json:"evidence_family,omitempty"`
	Reason         string  `json:"reason,omitempty"`
	StartLine      int     `json:"start_line,omitempty"`
	EndLine        int     `json:"end_line,omitempty"`
	Confidence     float64 `json:"confidence,omitempty"`
}

// EvidenceCitationHandleKey is the deduplication key for an
// EvidenceCitationHandle: every field that makes two handles the same
// citation, with none of the presentation-only fields (Confidence).
type EvidenceCitationHandleKey struct {
	kind           string
	repoID         string
	relativePath   string
	entityID       string
	evidenceFamily string
	reason         string
	startLine      int
	endLine        int
}

// EvidenceCitationHandleKey returns handle's deduplication key.
func (handle EvidenceCitationHandle) EvidenceCitationHandleKey() EvidenceCitationHandleKey {
	return EvidenceCitationHandleKey{
		kind:           handle.Kind,
		repoID:         handle.RepoID,
		relativePath:   handle.RelativePath,
		entityID:       handle.EntityID,
		evidenceFamily: handle.EvidenceFamily,
		reason:         handle.Reason,
		startLine:      handle.StartLine,
		endLine:        handle.EndLine,
	}
}
