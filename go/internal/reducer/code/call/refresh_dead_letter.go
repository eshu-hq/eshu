// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// DeltaGenerationRepoIDs returns the repository IDs whose repository fact
// carried delta_generation in this cycle, whether or not their delta file
// scope was usable. BuildRefreshIntentsWithDeltaFileScopes uses the set to
// dead-letter an unusable delta scope instead of widening its retract to the
// whole repository. Identity follows buildCodeCallDeltaFileScopesByRepoID
// (the decoded codegraph repository ID), so the two agree on which
// repositories run on a delta generation; a fact that does not decode
// carries no context either, so skipping it loses no refresh.
func DeltaGenerationRepoIDs(envelopes []facts.Envelope) map[string]struct{} {
	var ids map[string]struct{}
	for _, env := range envelopes {
		if env.FactKind != factload.FactKindRepository || !PayloadBool(env.Payload, "delta_generation") {
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
		if ids == nil {
			ids = make(map[string]struct{})
		}
		ids[repositoryID] = struct{}{}
	}
	return ids
}

// buildCodeCallDeltaDeadLetterRefreshIntent builds the refresh intent for a
// repository on a delta generation whose delta file scope is unusable
// (#10023 gap 1). It keeps the whole-scope partition key and the refresh
// identity the fences rank on, but flags delta_projection with empty paths:
// the generation carries content facts for its changed files only, so no
// correct retract exists here, and collectDeltaFilePaths rejects the empty
// paths before any statement runs. The partition fails and the intent
// dead-letters, which an operator can see; the alternative deletes every
// unchanged file's edge with nothing left to restore it.
func buildCodeCallDeltaDeadLetterRefreshIntent(
	repositoryID string,
	context sharedintent.ProjectionContext,
	createdAt time.Time,
) sharedintent.Row {
	payload := map[string]any{
		"repo_id":          repositoryID,
		"action":           "refresh",
		"intent_type":      sharedintent.RepoRefreshIntentType,
		"evidence_source":  RepoRefreshEvidenceSource,
		"delta_projection": true,
		"delta_file_paths": []string{},
	}

	return sharedintent.Build(sharedintent.Input{
		ProjectionDomain: reducercontract.DomainCodeCalls,
		PartitionKey:     RefreshPartitionKey(repositoryID),
		ScopeID:          context.ScopeID,
		AcceptanceUnitID: context.ResolveAcceptanceUnitID(repositoryID),
		RepositoryID:     repositoryID,
		SourceRunID:      context.SourceRunID,
		GenerationID:     context.GenerationID,
		Payload:          payload,
		CreatedAt:        createdAt,
	})
}
