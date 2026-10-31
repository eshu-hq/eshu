// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/git/content"
	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/git/runtime/declaration"
	runtimepaths "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/facts"
	parserjson "github.com/eshu-hq/eshu/go/internal/parser/json"
	pythonparser "github.com/eshu-hq/eshu/go/internal/parser/python"
	"github.com/eshu-hq/eshu/go/internal/parser/pythondep"
	rubyparser "github.com/eshu-hq/eshu/go/internal/parser/ruby"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
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
// private Composer, Python, and Ruby transport. gitcontent.FileFactEnvelope collapses the
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
	_, hasComposerTransport := parsedFile["composer_runtime_evidence"]
	_, hasPythonTransport := parsedFile["python_runtime_evidence"]
	_, hasRubyTransport := parsedFile["ruby_runtime_evidence"]
	_, hasJVMTransport := parsedFile["jvm_runtime_evidence"]
	_, hasDotNetTransport := parsedFile["dotnet_runtime_evidence"]
	_, hasIACTransport := parsedFile["iac_runtime_evidence"]
	_, hasRustTransport := parsedFile["rust_runtime_evidence"]
	_, hasElixirTransport := parsedFile["elixir_runtime_evidence"]
	_, hasHaskellTransport := parsedFile["haskell_runtime_evidence"]
	_, hasCabalTransport := parsedFile["cabal_runtime_evidence"]
	_, hasDartTransport := parsedFile["dart_runtime_evidence"]
	_, hasSwiftTransport := parsedFile["swift_runtime_evidence"]
	if hasComposerTransport || hasPythonTransport || hasRubyTransport || hasJVMTransport || hasDotNetTransport || hasIACTransport || hasRustTransport || hasElixirTransport || hasHaskellTransport || hasCabalTransport || hasDartTransport || hasSwiftTransport {
		clean := make(map[string]any, len(parsedFile)-1)
		for key, value := range parsedFile {
			if key != "composer_runtime_evidence" && key != "python_runtime_evidence" && key != "ruby_runtime_evidence" && key != "jvm_runtime_evidence" && key != "dotnet_runtime_evidence" && key != "iac_runtime_evidence" && key != "rust_runtime_evidence" && key != "elixir_runtime_evidence" && key != "haskell_runtime_evidence" && key != "cabal_runtime_evidence" && key != "dart_runtime_evidence" && key != "swift_runtime_evidence" {
				clean[key] = value
			}
		}
		parsedFile = clean
	}
	return gitcontent.FileFactEnvelope(repoPath, repoID, scopeID, generationID, observedAt, parsedFile, isDependency)
}

// noteRustCandidate notes the Cargo.toml candidate when the path is a Cargo
// manifest. It reports whether it handled the path; a Cargo.toml without
// parsed evidence or an expected snapshot falls through to the other runtime
// checks below, matching the inline dispatch it replaces.
func noteRustCandidate(snapshot *RepositorySnapshot, candidates map[string]runtimedeclaration.Candidate, relativePath, body string, dropped *atomic.Bool, droppedPaths *scope.StreamDroppedPaths) bool {
	if filepath.Base(relativePath) != "Cargo.toml" {
		return false
	}
	if parsed, ok := snapshot.RustRuntime[relativePath]; ok {
		if parsed.Digest != shared.RuntimeDigest([]byte(body)) {
			dropped.Store(true)
			droppedPaths.Note(relativePath)
			return true
		}
		runtimedeclaration.NoteParsedRustCandidate(candidates, relativePath, parsed.Extraction)
		return true
	}
	if snapshot.RustRuntimeExpected {
		if len(body) > runtimepaths.MaxManifestBytesPerFile {
			runtimedeclaration.NoteCappedRustCandidate(candidates, relativePath, len(body))
			return true
		}
		dropped.Store(true)
		droppedPaths.Note(relativePath)
		runtimedeclaration.NoteUnavailableRustCandidate(candidates, relativePath, len(body))
		return true
	}
	return false
}

// noteSwiftCandidate notes the Package.swift tools-version candidate when the
// path is a Package.swift manifest. It reports whether it handled the path; a
// Package.swift without parsed evidence or an expected snapshot falls through
// to the other runtime checks below, matching the inline dispatch it
// replaces.
func noteSwiftCandidate(snapshot *RepositorySnapshot, candidates map[string]runtimedeclaration.Candidate, relativePath, body string, dropped *atomic.Bool, droppedPaths *scope.StreamDroppedPaths) bool {
	if filepath.Base(relativePath) != "Package.swift" {
		return false
	}
	if parsed, ok := snapshot.SwiftRuntime[relativePath]; ok {
		if parsed.Digest != shared.RuntimeDigest([]byte(body)) {
			dropped.Store(true)
			droppedPaths.Note(relativePath)
			return true
		}
		runtimedeclaration.NoteParsedSwiftCandidate(candidates, relativePath, parsed.Extraction)
		return true
	}
	if snapshot.SwiftRuntimeExpected {
		if len(body) > runtimepaths.MaxManifestBytesPerFile {
			runtimedeclaration.NoteCappedSwiftCandidate(candidates, relativePath, len(body))
			return true
		}
		dropped.Store(true)
		droppedPaths.Note(relativePath)
		runtimedeclaration.NoteUnavailableSwiftCandidate(candidates, relativePath, len(body))
		return true
	}
	return false
}

// noteDartOrSwiftCandidate notes the Dart pubspec/.fvmrc or Swift
// Package.swift candidate, preserving the Dart-before-Swift dispatch order.
// It reports whether either handler claimed the path.
func noteDartOrSwiftCandidate(snapshot *RepositorySnapshot, candidates map[string]runtimedeclaration.Candidate, relativePath, body string, dropped *atomic.Bool, droppedPaths *scope.StreamDroppedPaths) bool {
	if noteDartCandidate(snapshot, candidates, relativePath, body, dropped, droppedPaths) {
		return true
	}
	return noteSwiftCandidate(snapshot, candidates, relativePath, body, dropped, droppedPaths)
}

// noteElixirCandidate notes the mix.exs candidate when the path is a Mix
// manifest. It reports whether it handled the path; a mix.exs without parsed
// evidence or an expected snapshot falls through to the other runtime checks
// below, matching the inline dispatch it replaces.
func noteElixirCandidate(snapshot *RepositorySnapshot, candidates map[string]runtimedeclaration.Candidate, relativePath, body string, dropped *atomic.Bool, droppedPaths *scope.StreamDroppedPaths) bool {
	if filepath.Base(relativePath) != "mix.exs" {
		return false
	}
	if parsed, ok := snapshot.ElixirRuntime[relativePath]; ok {
		if parsed.Digest != shared.RuntimeDigest([]byte(body)) {
			dropped.Store(true)
			droppedPaths.Note(relativePath)
			return true
		}
		runtimedeclaration.NoteParsedElixirCandidate(candidates, relativePath, parsed.Extraction)
		return true
	}
	if snapshot.ElixirRuntimeExpected {
		if len(body) > runtimepaths.MaxManifestBytesPerFile {
			runtimedeclaration.NoteCappedElixirCandidate(candidates, relativePath, len(body))
			return true
		}
		dropped.Store(true)
		droppedPaths.Note(relativePath)
		runtimedeclaration.NoteUnavailableElixirCandidate(candidates, relativePath, len(body))
		return true
	}
	return false
}

// noteComposerOrRuntimeCandidate uses the parser's one Composer interpretation
// when available. A changed body cannot be combined with an earlier parse.
func noteComposerOrRuntimeCandidate(snapshot *RepositorySnapshot, candidates map[string]runtimedeclaration.Candidate, relativePath, body string, dropped *atomic.Bool, droppedPaths *scope.StreamDroppedPaths) {
	if noteRustCandidate(snapshot, candidates, relativePath, body, dropped, droppedPaths) ||
		noteElixirCandidate(snapshot, candidates, relativePath, body, dropped, droppedPaths) ||
		noteHaskellCandidate(snapshot, candidates, relativePath, body, dropped, droppedPaths) ||
		noteDartOrSwiftCandidate(snapshot, candidates, relativePath, body, dropped, droppedPaths) {
		return
	}
	if isIACRuntimePath(relativePath) {
		if parsed, ok := snapshot.IACRuntime[relativePath]; ok {
			if parsed.Digest != shared.RuntimeDigest([]byte(body)) {
				dropped.Store(true)
				droppedPaths.Note(relativePath)
				return
			}
			runtimedeclaration.NoteParsedIACCandidate(candidates, relativePath, parsed.Extraction)
			return
		}
		if snapshot.IACRuntimeExpected {
			if len(body) > runtimepaths.MaxManifestBytesPerFile {
				runtimedeclaration.NoteCappedIACCandidate(candidates, relativePath, len(body))
				return
			}
			dropped.Store(true)
			droppedPaths.Note(relativePath)
			runtimedeclaration.NoteUnavailableIACCandidate(candidates, relativePath, len(body))
			return
		}
	}
	if isJVMRuntimePath(relativePath) {
		if parsed, ok := snapshot.JVMRuntime[relativePath]; ok {
			if parsed.Digest != shared.RuntimeDigest([]byte(body)) {
				dropped.Store(true)
				droppedPaths.Note(relativePath)
				return
			}
			runtimedeclaration.NoteParsedJVMCandidate(candidates, relativePath, parsed.Extraction)
			return
		}
		if snapshot.JVMRuntimeExpected {
			if len(body) > runtimepaths.MaxManifestBytesPerFile {
				runtimedeclaration.NoteCappedJVMCandidate(candidates, relativePath, len(body))
				return
			}
			dropped.Store(true)
			droppedPaths.Note(relativePath)
			runtimedeclaration.NoteUnavailableJVMCandidate(candidates, relativePath, len(body))
			return
		}
	}
	if filepath.Base(relativePath) == "global.json" || filepath.Ext(relativePath) == ".csproj" {
		if parsed, ok := snapshot.DotNetRuntime[relativePath]; ok {
			if parsed.Digest != shared.DotNetRuntimeDigest([]byte(body)) {
				dropped.Store(true)
				droppedPaths.Note(relativePath)
				return
			}
			runtimedeclaration.NoteParsedDotNetCandidate(candidates, relativePath, parsed.Extraction)
			return
		}
		if snapshot.DotNetRuntimeExpected {
			if len(body) > runtimepaths.MaxManifestBytesPerFile {
				runtimedeclaration.NoteCappedDotNetCandidate(candidates, relativePath, len(body))
				return
			}
			dropped.Store(true)
			droppedPaths.Note(relativePath)
			runtimedeclaration.NoteUnavailableDotNetCandidate(candidates, relativePath, len(body))
			return
		}
	}
	if filepath.Base(relativePath) == "Gemfile" || filepath.Base(relativePath) == "Gemfile.lock" {
		if parsed, ok := snapshot.RubyRuntime[relativePath]; ok {
			if parsed.Digest != rubyparser.RubyRuntimeDigest([]byte(body)) {
				dropped.Store(true)
				droppedPaths.Note(relativePath)
				return
			}
			runtimedeclaration.NoteParsedRubyCandidate(candidates, relativePath, parsed.Extraction)
			return
		}
		if snapshot.RubyRuntimeExpected {
			if len(body) > runtimepaths.MaxManifestBytesPerFile {
				runtimedeclaration.NoteCappedRubyCandidate(candidates, relativePath, len(body))
				return
			}
			dropped.Store(true)
			droppedPaths.Note(relativePath)
			runtimedeclaration.NoteUnavailableRubyCandidate(candidates, relativePath, len(body))
			return
		}
	}
	if filepath.Base(relativePath) == "pyproject.toml" || filepath.Base(relativePath) == "Pipfile" || filepath.Base(relativePath) == "setup.py" {
		if parsed, ok := snapshot.PythonRuntime[relativePath]; ok {
			if parsed.Digest != pythondep.PythonRuntimeDigest([]byte(body)) {
				dropped.Store(true)
				droppedPaths.Note(relativePath)
				return
			}
			runtimedeclaration.NoteParsedPythonCandidate(candidates, relativePath, parsed.Extraction)
			return
		}
		if snapshot.PythonRuntimeExpected {
			// A native parse miss is deterministic only when the current body
			// itself is malformed or capped. A valid body the parser missed
			// requests retry; never infer runtime truth from this reread.
			if len(body) > runtimepaths.MaxManifestBytesPerFile {
				runtimedeclaration.NoteCappedPythonCandidate(candidates, relativePath, len(body))
				return
			}
			var state string
			var err error
			if filepath.Base(relativePath) == "setup.py" {
				var evidence pythondep.PythonRuntimeEvidence
				evidence, err = pythonparser.ExtractSetupPYRuntimeEvidence([]byte(body))
				state = evidence.State
			} else {
				var evidence pythondep.PythonRuntimeEvidence
				evidence, err = pythondep.ExtractPythonRuntimeEvidence(filepath.Base(relativePath), []byte(body))
				state = evidence.State
			}
			if err != nil || state == "unavailable" {
				runtimedeclaration.NoteUnavailablePythonCandidate(candidates, relativePath, len(body))
				return
			}
			dropped.Store(true)
			droppedPaths.Note(relativePath)
			runtimedeclaration.NoteUnavailablePythonCandidate(candidates, relativePath, len(body))
			return
		}
	}
	if filepath.Base(relativePath) == "composer.json" || filepath.Base(relativePath) == "composer.lock" {
		if runtimepaths.ClassifyPath(relativePath) != runtimepaths.FileManifest {
			return
		}
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

// noteHaskellCandidate notes the stack.yaml candidate when the path is a
// Stack manifest. It reports whether it handled the path; a stack.yaml
// without parsed evidence or an expected snapshot falls through to the other
// runtime checks below, matching the inline dispatch it replaces, as does a
// body the generic reader accepts with no Stack evidence (a CloudFormation
// template carrying the stack.yaml name).
func noteHaskellCandidate(snapshot *RepositorySnapshot, candidates map[string]runtimedeclaration.Candidate, relativePath, body string, dropped *atomic.Bool, droppedPaths *scope.StreamDroppedPaths) bool {
	if filepath.Base(relativePath) != "stack.yaml" {
		return false
	}
	if parsed, ok := snapshot.HaskellRuntime[relativePath]; ok {
		if parsed.Digest != shared.RuntimeDigest([]byte(body)) {
			dropped.Store(true)
			droppedPaths.Note(relativePath)
			return true
		}
		runtimedeclaration.NoteParsedHaskellCandidate(candidates, relativePath, parsed.Extraction)
		return true
	}
	if snapshot.HaskellRuntimeExpected {
		// A CloudFormation template literally named stack.yaml parses fine
		// but carries no Stack evidence. The generic reader below admits the
		// same body with no declarations and no skip, so fall through to it
		// instead of marking the stream dropped with a misleading
		// manifest_unparseable skip. A body the generic reader rejects or
		// declares from keeps the retry path below.
		if extraction, ok := runtimepaths.Extract(relativePath, body); ok && extraction.Len() == 0 && !extraction.Skip().Skipped() {
			return false
		}
		if len(body) > runtimepaths.MaxManifestBytesPerFile {
			runtimedeclaration.NoteCappedHaskellCandidate(candidates, relativePath, len(body))
			return true
		}
		dropped.Store(true)
		droppedPaths.Note(relativePath)
		runtimedeclaration.NoteUnavailableHaskellCandidate(candidates, relativePath, len(body))
		return true
	}
	return false
}

// isDartRuntimePath reports whether relativePath is a Dart version source:
// a pubspec manifest or an .fvmrc pin, at any depth.
func isDartRuntimePath(relativePath string) bool {
	switch filepath.Base(relativePath) {
	case "pubspec.yaml", "pubspec.yml", ".fvmrc":
		return true
	default:
		return false
	}
}

// noteDartCandidate notes the Dart pubspec or .fvmrc candidate when the path
// is a Dart version source. It reports whether it handled the path; a
// changed body cannot be combined with an earlier parse.
func noteDartCandidate(snapshot *RepositorySnapshot, candidates map[string]runtimedeclaration.Candidate, relativePath, body string, dropped *atomic.Bool, droppedPaths *scope.StreamDroppedPaths) bool {
	if !isDartRuntimePath(relativePath) {
		return false
	}
	if parsed, ok := snapshot.DartRuntime[relativePath]; ok {
		if parsed.Digest != shared.RuntimeDigest([]byte(body)) {
			dropped.Store(true)
			droppedPaths.Note(relativePath)
			return true
		}
		runtimedeclaration.NoteParsedDartCandidate(candidates, relativePath, parsed.Extraction)
		return true
	}
	if snapshot.DartRuntimeExpected {
		if len(body) > runtimepaths.MaxManifestBytesPerFile {
			runtimedeclaration.NoteCappedDartCandidate(candidates, relativePath, len(body))
			return true
		}
		dropped.Store(true)
		droppedPaths.Note(relativePath)
		runtimedeclaration.NoteUnavailableDartCandidate(candidates, relativePath, len(body))
		return true
	}
	return false
}

func isJVMRuntimePath(relativePath string) bool {
	switch filepath.Base(relativePath) {
	case "pom.xml", "build.gradle", "build.gradle.kts", "build.sbt":
		return true
	case "build.properties":
		return relativePath == "project/build.properties" || strings.HasSuffix(relativePath, "/project/build.properties")
	default:
		return false
	}
}

func isIACRuntimePath(relativePath string) bool {
	base := filepath.Base(relativePath)
	return strings.HasSuffix(base, ".tf") || base == "terragrunt.hcl" || strings.EqualFold(base, "Chart.yaml") || strings.EqualFold(base, "Chart.yml")
}
