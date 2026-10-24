// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
)

const gitRuntimeDeclarationCollectorInstanceID = "git-runtime-declaration"

// NoteRuntimeDeclarationCandidate records body under relativePath in candidates
// when relativePath is one of the admitted version-pin files at any depth (see
// runtimedeclaration.SourceKindForPath); a no-op otherwise. The git collector
// calls it per content file in both streamFacts content branches so emission
// can run once after both close.
func NoteRuntimeDeclarationCandidate(candidates map[string]string, relativePath, body string) {
	if _, ok := runtimedeclaration.SourceKindForPath(relativePath); ok {
		candidates[relativePath] = body
	}
}

// EmitRuntimeDeclarationFactsForCandidates sends one
// runtime_declaration.version fact per (line, token) of every accumulated pin
// file, in path order so the stream is deterministic. A no-op when candidates
// is empty. Each pin file is independent, so a delta generation re-emits only
// the pin files it changed.
func EmitRuntimeDeclarationFactsForCandidates(
	w model.FactStreamWriter,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	candidates map[string]string,
) {
	if len(candidates) == 0 {
		return
	}
	paths := make([]string, 0, len(candidates))
	for relativePath := range candidates {
		paths = append(paths, relativePath)
	}
	sort.Strings(paths)
	instanceID := gitRuntimeDeclarationCollectorInstanceID
	for _, relativePath := range paths {
		ctx := runtimedeclaration.FixtureContext{
			ScopeID:             scopeID,
			GenerationID:        generationID,
			ObservedAt:          observedAt,
			SourceURI:           relativePath,
			CollectorInstanceID: &instanceID,
		}
		for _, envelope := range runtimedeclaration.Emit(ctx, repoID, relativePath, candidates[relativePath]) {
			w.Send(envelope)
		}
	}
}
