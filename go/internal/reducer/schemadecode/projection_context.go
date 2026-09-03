// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// BuildProjectionContexts decodes each "repository" fact's outer envelope
// through the codegraph contracts seam
// ([DecodeCodegraphRepository]) to recover its join identity
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
func BuildProjectionContexts(envelopes []facts.Envelope, generationID string) map[string]sharedintent.ProjectionContext {
	contextByRepoID := make(map[string]sharedintent.ProjectionContext)
	for _, env := range envelopes {
		if env.FactKind != "repository" {
			continue
		}

		repository, err := DecodeCodegraphRepository(env)
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

		contextByRepoID[repositoryID] = sharedintent.ProjectionContext{
			ScopeID:          env.ScopeID,
			AcceptanceUnitID: repositoryID,
			SourceRunID:      sourceRunID,
			GenerationID:     generationID,
		}
	}
	return contextByRepoID
}
