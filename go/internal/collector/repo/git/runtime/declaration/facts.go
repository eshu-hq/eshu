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

// Candidate is one admitted pin file held between the content walk and
// emission. Body is empty when the file is over runtimedeclaration.MaxBytesPerFile:
// the emitter would skip it anyway, so the walk keeps only ByteSize and never
// carries an over-cap body. That bounds the retained memory to the cap times the
// number of pin files, however large a hostile pin file is.
type Candidate struct {
	// Body is the file content, retained only when ByteSize is at or under
	// runtimedeclaration.MaxBytesPerFile.
	Body string
	// ByteSize is the full length of the file body in bytes.
	ByteSize int
}

// NoteRuntimeDeclarationCandidate records relativePath in candidates when it is
// one of the admitted version-pin files at any depth (see
// runtimedeclaration.SourceKindForPath); a no-op otherwise. A body over
// runtimedeclaration.MaxBytesPerFile is recorded by size only. The git
// collector calls it per content file in both streamFacts content branches so
// emission can run once after both close.
func NoteRuntimeDeclarationCandidate(candidates map[string]Candidate, relativePath, body string) {
	if _, ok := runtimedeclaration.SourceKindForPath(relativePath); !ok {
		return
	}
	if len(body) > runtimedeclaration.MaxBytesPerFile {
		candidates[relativePath] = Candidate{ByteSize: len(body)}
		return
	}
	candidates[relativePath] = Candidate{Body: body, ByteSize: len(body)}
}

// EmitRuntimeDeclarationFactsForCandidates sends one
// runtime_declaration.version fact per (line, token) of every accumulated pin
// file, in path order so the stream is deterministic. A no-op when candidates
// is empty. Each pin file is independent, so a delta generation re-emits only
// the pin files it changed. A file over runtimedeclaration.MaxBytesPerFile or
// runtimedeclaration.MaxTokensPerFile emits nothing and is reported through
// onSkip (relative path and the runtimedeclaration.Skip saying which cap
// tripped); onSkip may be nil.
func EmitRuntimeDeclarationFactsForCandidates(
	w model.FactStreamWriter,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	candidates map[string]Candidate,
	onSkip func(relativePath string, skip runtimedeclaration.Skip),
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
		candidate := candidates[relativePath]
		if candidate.ByteSize > runtimedeclaration.MaxBytesPerFile {
			if onSkip != nil {
				onSkip(relativePath, runtimedeclaration.Skip{Reason: runtimedeclaration.SkipReasonByteCap, ByteSize: candidate.ByteSize})
			}
			continue
		}
		envelopes, skip := runtimedeclaration.EmitFile(ctx, repoID, relativePath, candidate.Body)
		if skip.Skipped() && onSkip != nil {
			onSkip(relativePath, skip)
		}
		for _, envelope := range envelopes {
			w.Send(envelope)
		}
	}
}
