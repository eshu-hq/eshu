// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// ProjectionContext holds the bounded-unit freshness context for one shared
// projection repository slice. It is the identity half of [Input]: a materializer
// builds one context per repository it loaded facts for, then stamps it onto
// every intent that repository produces.
type ProjectionContext struct {
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
	GenerationID     string
}

// ResolveAcceptanceUnitID returns the acceptance unit the context names,
// falling back to the repository ID when the context carries none. [Build]
// applies the same fallback, so a caller that reads the value back (to key a
// map, or to build a partition key) sees exactly what the stored row will
// carry. The method is not named for the field it reads because a Go struct
// cannot carry a field and a method under one name.
func (c ProjectionContext) ResolveAcceptanceUnitID(repositoryID string) string {
	if unitID := strings.TrimSpace(c.AcceptanceUnitID); unitID != "" {
		return unitID
	}
	return strings.TrimSpace(repositoryID)
}

// BuildProjectionContexts decodes each "repository" fact's outer envelope
// through the codegraph contracts seam
// ([schemadecode.DecodeCodegraphRepository]) to recover its join identity
// (RepoID) and optional SourceRunID before building one ProjectionContext per
// repository. A repository fact whose payload is missing a required identity
// field is skipped for context building -- dropped by returning early from the
// decoder's error, matching this function's pre-existing "skip and continue"
// shape for an absent identity; batch-wide quarantine visibility for this read
// site is provided by the callers' own file-fact quarantine, which is the
// accuracy hole issue #4749 targets (repo_id/relative_path used to silently
// collapse to an empty-string graph identity on "file" facts).
//
// SourceRunID stays required here: not every repository fact carries one, and an
// absent source run id is a legitimate reason to skip context building, not a
// malformed payload. A repository skipped here has no acceptance identity, so
// its rows cannot be fenced or freshness-gated and every caller drops them.
//
// TrimSpace preserves the pre-Contract-System payloadStr behavior: a
// whitespace-only repo_id must not become a non-canonical AcceptanceUnitID or
// map key. The real collector never emits a whitespace repo id, so this is
// behavior-equivalence, not new logic.
func BuildProjectionContexts(envelopes []facts.Envelope, generationID string) map[string]ProjectionContext {
	contextByRepoID := make(map[string]ProjectionContext)
	for _, env := range envelopes {
		if env.FactKind != "repository" {
			continue
		}

		repository, err := schemadecode.DecodeCodegraphRepository(env)
		if err != nil {
			continue
		}

		repositoryID := strings.TrimSpace(repository.RepoID)
		if repositoryID == "" {
			continue
		}
		var sourceRunID string
		if repository.SourceRunID != nil {
			sourceRunID = strings.TrimSpace(*repository.SourceRunID)
		}
		if sourceRunID == "" {
			continue
		}

		contextByRepoID[repositoryID] = ProjectionContext{
			ScopeID:          env.ScopeID,
			AcceptanceUnitID: repositoryID,
			SourceRunID:      sourceRunID,
			GenerationID:     generationID,
		}
	}
	return contextByRepoID
}
