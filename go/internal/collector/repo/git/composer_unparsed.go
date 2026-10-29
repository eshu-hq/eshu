// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"

	runtimefacts "github.com/eshu-hq/eshu/go/internal/collector/repo/git/runtime/declaration"
	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	parserjson "github.com/eshu-hq/eshu/go/internal/parser/json"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// noteUnparsedComposerCandidates keeps deterministic parser failures as typed
// skips and marks uncertain or changed files for a later retry. It never uses
// a reread as declaration evidence.
func noteUnparsedComposerCandidates(
	repoPath string,
	unparsedFiles []composerRuntimeUnparsed,
	candidates map[string]runtimefacts.Candidate,
	dropped *atomic.Bool,
	droppedPaths *scope.StreamDroppedPaths,
) {
	for _, unparsed := range unparsedFiles {
		if !composerUnparsedStillSame(repoPath, unparsed) {
			dropped.Store(true)
			droppedPaths.Note(unparsed.Path)
			runtimefacts.NoteUnavailableComposerCandidate(candidates, unparsed.Path, unparsed.ByteSize)
			continue
		}
		if unparsed.SkipReason == runtimedeclaration.SkipReasonByteCap {
			runtimefacts.NoteCappedComposerCandidate(candidates, unparsed.Path, unparsed.ByteSize)
		} else {
			runtimefacts.NoteUnavailableComposerCandidate(candidates, unparsed.Path, unparsed.ByteSize)
		}
	}
}

// classifyMissingComposerFile only classifies a parser miss as deterministic
// when the current bytes are invalid or over the Composer cap. It never
// accepts fallback values as declaration evidence: a valid document that the
// native parser missed stays unavailable and retries. The stream rechecks the
// observed state before treating the skip as durable.
func classifyMissingComposerFile(absolutePath, relativePath string, discoveredSize int64) composerRuntimeUnparsed {
	result := composerRuntimeUnparsed{Path: relativePath, ByteSize: max(0, int(discoveredSize))}
	info, err := os.Stat(absolutePath) // #nosec G304 -- a discovered repository file, not an external path
	if err != nil || !info.Mode().IsRegular() {
		return result
	}
	result.ByteSize = int(info.Size())
	if info.Size() > runtimedeclaration.MaxManifestBytesPerFile {
		result.SkipReason = runtimedeclaration.SkipReasonByteCap
		return result
	}
	body, err := readComposerFailureProbe(absolutePath)
	if err != nil {
		return result
	}
	result.ByteSize = len(body)
	if len(body) > runtimedeclaration.MaxManifestBytesPerFile {
		result.SkipReason = runtimedeclaration.SkipReasonByteCap
		return result
	}
	evidence, err := parserjson.ExtractComposerRuntimeEvidence(filepath.Base(relativePath), body)
	if err != nil {
		result.SkipReason = runtimedeclaration.SkipReasonManifestUnparseable
		result.Digest = evidence.Digest
	}
	return result
}

// composerUnparsedStillSame proves that an invalid file did not change after
// its failed parse. A capped file needs only remain over cap: neither version
// can emit a declaration. Every uncertain read forces the ordinary retry.
func composerUnparsedStillSame(repoPath string, unparsed composerRuntimeUnparsed) bool {
	if unparsed.SkipReason == "" {
		return false
	}
	absolutePath := filepath.Join(repoPath, filepath.FromSlash(unparsed.Path))
	info, err := os.Stat(absolutePath) // #nosec G304 -- a recorded repository path from discovery
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if unparsed.SkipReason == runtimedeclaration.SkipReasonByteCap {
		return info.Size() > runtimedeclaration.MaxManifestBytesPerFile
	}
	if unparsed.SkipReason != runtimedeclaration.SkipReasonManifestUnparseable || unparsed.Digest == "" || info.Size() > runtimedeclaration.MaxManifestBytesPerFile {
		return false
	}
	body, err := readComposerFailureProbe(absolutePath)
	return err == nil && len(body) <= runtimedeclaration.MaxManifestBytesPerFile && parserjson.ComposerRuntimeDigest(body) == unparsed.Digest
}

// readComposerFailureProbe caps a failed-only reread at one byte beyond the
// Composer limit. It never stores or logs the body.
func readComposerFailureProbe(path string) ([]byte, error) {
	file, err := os.Open(path) // #nosec G304 -- a discovered repository file, not an external path
	if err != nil {
		return nil, fmt.Errorf("open Composer failure probe: %w", err)
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(io.LimitReader(file, runtimedeclaration.MaxManifestBytesPerFile+1))
	if err != nil {
		return nil, fmt.Errorf("read Composer failure probe: %w", err)
	}
	return body, nil
}
