// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/git/content"
	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/git/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/facts"
	parserjson "github.com/eshu-hq/eshu/go/internal/parser/json"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// ParserFileFactEnvelope builds the durable "file" fact envelope for one parser
// payload, exactly as the Git collector's streaming fact builder does. It is the
// real parser->envelope emission seam reused by the deterministic replay
// parser-fixture flavor (go/internal/replay/parserfixture), so the recorded
// fixtures capture production envelope shape and provenance rather than a
// re-implementation: SourceRef.SourceURI is the absolute file path, the stable
// fact key is "file:<repoID>:<relativePath>", and SourceRef.SourceRecordID
// equals that key.
//
// parsedFile is the map[string]any returned by parser.Engine.ParsePath; it must
// carry the "path" key the parser sets to the file's absolute path. repoPath is
// the repository root the relative path is computed against. The envelope's
// Payload embeds the parser payload under "parsed_file_data" after removing
// private Composer transport. gitcontent.FileFactEnvelope collapses the
// fingerprint wall-clock observation to zero so durable facts stay byte-deterministic (a recorder marking that
// subtree opaque still preserves the parser output byte for byte).
func ParserFileFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	parsedFile map[string]any,
	isDependency bool,
) facts.Envelope {
	if _, hasTransport := parsedFile["composer_runtime_evidence"]; hasTransport {
		clean := make(map[string]any, len(parsedFile)-1)
		for key, value := range parsedFile {
			if key != "composer_runtime_evidence" {
				clean[key] = value
			}
		}
		parsedFile = clean
	}
	return gitcontent.FileFactEnvelope(repoPath, repoID, scopeID, generationID, observedAt, parsedFile, isDependency)
}

// noteComposerOrRuntimeCandidate uses the parser's one Composer interpretation
// when available. A changed body cannot be combined with an earlier parse.
func noteComposerOrRuntimeCandidate(snapshot *RepositorySnapshot, candidates map[string]runtimedeclaration.Candidate, relativePath, body string, dropped *atomic.Bool, droppedPaths *scope.StreamDroppedPaths) {
	if filepath.Base(relativePath) == "composer.json" || filepath.Base(relativePath) == "composer.lock" {
		if parsed, ok := snapshot.ComposerRuntime[relativePath]; ok {
			if parsed.Digest != parserjson.ComposerRuntimeDigest([]byte(body)) {
				dropped.Store(true)
				droppedPaths.Note(relativePath)
				return
			}
			runtimedeclaration.NoteParsedComposerCandidate(candidates, relativePath, parsed.Extraction)
			return
		}
		if snapshot.ComposerRuntimeExpected {
			// A native snapshot whose parser did not accept this file cannot
			// infer runtime truth from a later content reread. Mark it dropped
			// so the next sync retries the file, and report a typed skip.
			dropped.Store(true)
			droppedPaths.Note(relativePath)
			runtimedeclaration.NoteUnavailableComposerCandidate(candidates, relativePath, len(body))
			return
		}
	}
	runtimedeclaration.NoteRuntimeDeclarationCandidate(candidates, relativePath, body)
}
