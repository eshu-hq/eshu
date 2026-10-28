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

// Candidate is one admitted file held between the content walk and emission.
//
// A pin file keeps its Body, empty when the file is over
// runtimedeclaration.MaxBytesPerFile: the emitter would skip it anyway, so the
// walk keeps only ByteSize and never carries an over-cap body. That bounds the
// retained memory to the cap times the number of pin files, however large a
// hostile pin file is.
//
// A package.json, Composer file, or go.mod is read when the walk meets it (Manifest is true)
// and keeps only its Extraction. The Extraction owns copies of the few strings
// it holds (see runtimedeclaration.Extract), so it does not pin the body and a
// repository with thousands of manifests retains their declarations, not their
// size. TestNoteRuntimeDeclarationCandidateRetainsNoManifestBody holds that.
type Candidate struct {
	// Body is the pin file content, retained only when ByteSize is at or under
	// runtimedeclaration.MaxBytesPerFile. Empty for a manifest.
	Body string
	// ByteSize is the full length of the file body in bytes.
	ByteSize int
	// Manifest marks a package.json or go.mod, whose declarations are in
	// Extraction.
	Manifest bool
	// Extraction is the manifest's parsed declarations and skip.
	Extraction runtimedeclaration.Extraction
}

// NoteRuntimeDeclarationCandidate records relativePath in candidates when it is
// one of the admitted version-pin files at any depth (see
// runtimedeclaration.SourceKindForPath), or a package.json or go.mod outside a
// dependency directory; a no-op otherwise. A pin body over
// runtimedeclaration.MaxBytesPerFile is recorded by size only. A manifest is
// parsed here and recorded only when it declares something or has a skip to
// report. The git collector calls it per content file in both streamFacts
// content branches so emission can run once after both close.
func NoteRuntimeDeclarationCandidate(candidates map[string]Candidate, relativePath, body string) {
	switch runtimedeclaration.ClassifyPath(relativePath) {
	case runtimedeclaration.FileNotAdmitted:
		return
	case runtimedeclaration.FileManifest:
		extraction, _ := runtimedeclaration.Extract(relativePath, body)
		if extraction.Len() == 0 && !extraction.Skip().Skipped() {
			return
		}
		candidates[relativePath] = Candidate{ByteSize: len(body), Manifest: true, Extraction: extraction}
	case runtimedeclaration.FilePin:
		if len(body) > runtimedeclaration.MaxBytesPerFile {
			candidates[relativePath] = Candidate{ByteSize: len(body)}
			return
		}
		candidates[relativePath] = Candidate{Body: body, ByteSize: len(body)}
	}
}

// NoteParsedComposerCandidate stores guarded parser evidence without reading
// the Composer body again. Empty parsed documents need no candidate.
func NoteParsedComposerCandidate(candidates map[string]Candidate, relativePath string, extraction runtimedeclaration.Extraction) {
	if runtimedeclaration.ClassifyPath(relativePath) != runtimedeclaration.FileManifest {
		return
	}
	if extraction.Len() == 0 && !extraction.Skip().Skipped() {
		return
	}
	candidates[relativePath] = Candidate{Manifest: true, Extraction: extraction}
}

// NoteUnavailableComposerCandidate records a typed skip for a Composer file
// whose native parser failed or was not run.
func NoteUnavailableComposerCandidate(candidates map[string]Candidate, relativePath string, byteSize int) {
	candidates[relativePath] = Candidate{Manifest: true, Extraction: runtimedeclaration.UnavailableComposerExtraction(byteSize)}
}

// NoteCappedComposerCandidate records a Composer file proven over the byte
// cap, without retaining or parsing its body.
func NoteCappedComposerCandidate(candidates map[string]Candidate, relativePath string, byteSize int) {
	candidates[relativePath] = Candidate{Manifest: true, Extraction: runtimedeclaration.CappedComposerExtraction(byteSize)}
}

// EmitRuntimeDeclarationFactsForCandidates sends one
// runtime_declaration.version fact per declaration of every accumulated file,
// in path order so the stream is deterministic. A no-op when candidates is
// empty. Each file is independent, so a delta generation re-emits only the
// files it changed. A file over its byte or token cap, or a package.json that
// is not readable, emits nothing; a manifest that lost values to the credential
// guard emits the rest. Each is reported through onSkip (relative path and the
// runtimedeclaration.Skip saying why); onSkip may be nil.
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
		if candidate.Manifest {
			envelopes, skip := runtimedeclaration.EmitExtraction(ctx, repoID, relativePath, candidate.Extraction)
			if skip.Skipped() && onSkip != nil {
				onSkip(relativePath, skip)
			}
			for _, envelope := range envelopes {
				w.Send(envelope)
			}
			continue
		}
		if candidate.ByteSize > runtimedeclaration.MaxBytesPerFile {
			if onSkip != nil {
				onSkip(relativePath, runtimedeclaration.Skip{Reason: runtimedeclaration.SkipReasonByteCap, ByteSize: candidate.ByteSize, ByteCap: runtimedeclaration.MaxBytesPerFile})
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
