// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"path/filepath"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/codeowners"
	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
)

const gitCodeownersCollectorInstanceID = "git-codeowners"

// isCodeownersCandidatePath reports whether absolutePath, made relative to
// repoPath, is one of the three CODEOWNERS locations GitHub honors (see
// codeowners.IsCandidatePath). Discovery's SupportedFileMatcher callbacks
// only see the absolute filesystem path, so this helper relativizes before
// delegating to the shared precedence check every other call site uses.
func isCodeownersCandidatePath(repoPath, absolutePath string) bool {
	relativePath, err := filepath.Rel(repoPath, absolutePath)
	if err != nil {
		return false
	}
	_, ok := codeowners.IsCandidatePath(filepath.ToSlash(filepath.Clean(relativePath)))
	return ok
}

// noteCodeownersCandidate records body under its canonical CODEOWNERS location
// in candidates when relativePath is one of the three recognized locations (see
// codeowners.IsCandidatePath); a no-op otherwise. Called per content file in
// both streamFacts content branches so resolution can pick one winner later.
func noteCodeownersCandidate(candidates map[string]string, relativePath, body string) {
	if candidatePath, ok := codeowners.IsCandidatePath(relativePath); ok {
		candidates[candidatePath] = body
	}
}

// extractCodeownersCandidateFiles removes any of the three recognized
// CODEOWNERS locations (see codeowners.IsCandidatePath) from files and
// returns their repo-relative paths separately, mirroring
// extractTerraformStateCandidates: CODEOWNERS is structured ownership
// metadata, not source code, so it is diverted before the language-parser
// pipeline (partitionNativeSnapshotFiles / engine.ParsePath) rather than
// requiring a registered parser.Definition for a file with no extension.
func extractCodeownersCandidateFiles(
	repoPath string,
	files []discovery.FileWithSize,
) ([]discovery.FileWithSize, []string) {
	var candidateRelativePaths []string
	filtered := files[:0]
	for _, file := range files {
		relativePath, err := filepath.Rel(repoPath, file.Path)
		if err != nil {
			filtered = append(filtered, file)
			continue
		}
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))
		if _, ok := codeowners.IsCandidatePath(relativePath); !ok {
			filtered = append(filtered, file)
			continue
		}
		candidateRelativePaths = append(candidateRelativePaths, relativePath)
	}
	return filtered, candidateRelativePaths
}

// codeownersDeltaTouchesCandidate reports whether any of the delta's
// changed/deleted relative paths is one of the three recognized CODEOWNERS
// candidate locations (see codeowners.IsCandidatePath).
func codeownersDeltaTouchesCandidate(deltaRelativePaths []string) bool {
	for _, relativePath := range deltaRelativePaths {
		if _, ok := codeowners.IsCandidatePath(relativePath); ok {
			return true
		}
	}
	return false
}

// codeownersCandidatePathsPresentOnDisk reports each of the three recognized
// CODEOWNERS locations (codeowners.CandidatePaths()) that currently exists as
// a regular file under repoPath, checked by a direct stat against repoPath
// rather than through the (possibly delta-narrowed) discovered file set.
// CODEOWNERS winner-resolution is whole-repo, so whenever a delta snapshot's
// changed/deleted paths touch any one candidate location (see
// codeownersDeltaTouchesCandidate), the collector must see every
// currently-existing candidate, not just the ones the delta happened to list
// as changed (issue #5419 P1).
func codeownersCandidatePathsPresentOnDisk(repoPath string) []string {
	var present []string
	for _, candidate := range codeowners.CandidatePaths() {
		absolutePath := filepath.Join(repoPath, filepath.FromSlash(candidate))
		if info, err := os.Stat(absolutePath); err == nil && !info.IsDir() {
			present = append(present, candidate)
		}
	}
	return present
}

// resolvedCodeownersCandidateRelativePaths returns the CODEOWNERS candidate
// relative paths a snapshot should use for this generation.
//
// extractedRelativePaths is whatever extractCodeownersCandidateFiles already
// found in the (possibly delta-narrowed) discovered file set; that is enough
// on a full snapshot, where fileSet.Files spans the whole repository.
//
// On a delta snapshot, though, fileSet.Files is narrowed to the delta's
// changed targets only (see resolveNativeSnapshotFileSetForTargets), so a
// deleted or newly added candidate can hide a sibling candidate that already
// exists on disk unchanged. Whenever isDelta is true and deltaRelativePaths
// touches any of the three known CODEOWNERS locations (see
// codeownersDeltaTouchesCandidate), this instead re-reads every
// currently-existing candidate directly from repoPath (see
// codeownersCandidatePathsPresentOnDisk), so ResolveWinner sees the true,
// current whole-repo candidate set rather than only the ones the delta
// happened to list as changed (issue #5419 P1).
func resolvedCodeownersCandidateRelativePaths(
	repoPath string,
	isDelta bool,
	deltaRelativePaths []string,
	extractedRelativePaths []string,
) []string {
	if isDelta && codeownersDeltaTouchesCandidate(deltaRelativePaths) {
		return codeownersCandidatePathsPresentOnDisk(repoPath)
	}
	return extractedRelativePaths
}

// codeownersFileMetasForPaths builds one ContentFileMeta per extracted
// CODEOWNERS candidate path, reading only a content digest from disk (never a
// language parse). Metas flow into snapshot.ContentFileMetas so the existing
// two-phase content stream (streamFacts) re-reads the body once at emit time,
// same as every other content file.
func codeownersFileMetasForPaths(repoPath string, relativePaths []string, commitSHA string) []ContentFileMeta {
	metas := make([]ContentFileMeta, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		absolutePath := filepath.Join(repoPath, filepath.FromSlash(relativePath))
		digest, ok := documentationDigestForFile(absolutePath)
		if !ok {
			continue
		}
		metas = append(metas, ContentFileMeta{
			RelativePath: relativePath,
			Digest:       digest,
			Language:     codeowners.CollectorKind,
			ArtifactType: codeowners.CollectorKind,
			CommitSHA:    commitSHA,
		})
	}
	return metas
}

// emitCodeownersFactsForCandidates resolves the single CODEOWNERS file GitHub
// would honor from the accumulated candidate bodies (see
// codeowners.ResolveWinner) and sends one codeowners.ownership fact per parsed
// rule. A no-op when candidates is empty or carries no recognized location.
func emitCodeownersFactsForCandidates(
	w factStreamWriter,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	candidates map[string]string,
) {
	sourcePath, body, ok := codeowners.ResolveWinner(candidates)
	if !ok {
		return
	}
	ctx := codeowners.FixtureContext{
		ScopeID:      scopeID,
		GenerationID: generationID,
		ObservedAt:   observedAt,
		SourceURI:    sourcePath,
	}
	instanceID := gitCodeownersCollectorInstanceID
	ctx.CollectorInstanceID = &instanceID
	for _, envelope := range codeowners.Emit(ctx, repoID, sourcePath, body) {
		w.send(envelope)
	}
}
