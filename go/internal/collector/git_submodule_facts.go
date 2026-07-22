// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/collector/submodule"
)

const gitSubmoduleCollectorInstanceID = "git-submodule"

// isGitmodulesCandidatePath reports whether absolutePath, made relative to
// repoPath, is the recognized ".gitmodules" location (see
// submodule.IsGitmodulesPath). Discovery's SupportedFileMatcher callback
// (see resolveNativeSnapshotFileSet) only admits a file that already has a
// registered parser.Definition, is documentation, or matches one of a small
// set of structured-but-extensionless candidates; without this hook,
// ".gitmodules" would never reach fileSet.Files at all; it would be silently
// skipped during discovery long before extractGitmodulesCandidateFiles ever
// runs, mirroring isCodeownersCandidatePath's role for CODEOWNERS.
func isGitmodulesCandidatePath(repoPath, absolutePath string) bool {
	relativePath, err := filepath.Rel(repoPath, absolutePath)
	if err != nil {
		return false
	}
	return submodule.IsGitmodulesPath(filepath.ToSlash(filepath.Clean(relativePath)))
}

// noteSubmoduleCandidate records body under the ".gitmodules" key in
// candidates when relativePath is the recognized ".gitmodules" location (see
// submodule.IsGitmodulesPath); a no-op otherwise. Called per content file in
// both streamFacts content branches so a single accumulated body is emitted
// once at the end of the content loop.
func noteSubmoduleCandidate(candidates map[string]string, relativePath, body string) {
	if submodule.IsGitmodulesPath(relativePath) {
		candidates[relativePath] = body
	}
}

// extractGitmodulesCandidateFiles removes the repo-root ".gitmodules" file
// (see submodule.IsGitmodulesPath) from files and returns its repo-relative
// path separately, mirroring extractCodeownersCandidateFiles /
// extractTerraformStateCandidates: ".gitmodules" is structured submodule
// metadata, not source code, so it is diverted before the language-parser
// pipeline (partitionNativeSnapshotFiles / engine.ParsePath) rather than
// requiring a registered parser.Definition for a file with no extension.
func extractGitmodulesCandidateFiles(
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
		if !submodule.IsGitmodulesPath(relativePath) {
			filtered = append(filtered, file)
			continue
		}
		candidateRelativePaths = append(candidateRelativePaths, relativePath)
	}
	return filtered, candidateRelativePaths
}

// gitmodulesFileMetasForPaths builds one ContentFileMeta per extracted
// ".gitmodules" candidate path (in practice zero or one, since git only ever
// reads a single repo-root ".gitmodules"), reading only a content digest
// from disk (never a language parse). Metas flow into
// snapshot.ContentFileMetas so the existing two-phase content stream
// (streamFacts) re-reads the body once at emit time, same as every other
// content file.
func gitmodulesFileMetasForPaths(repoPath string, relativePaths []string, commitSHA string) []ContentFileMeta {
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
			Language:     submodule.CollectorKind,
			ArtifactType: submodule.CollectorKind,
			CommitSHA:    commitSHA,
		})
	}
	return metas
}

// emitSubmoduleFactsForCandidates parses the accumulated ".gitmodules" body
// (see noteSubmoduleCandidate) and sends one submodule.pin fact per declared
// submodule entry. A no-op when candidates carries no ".gitmodules" body.
//
// Each entry's PinnedSHA (issue #5420 Phase 2b) is resolved against
// repoPath's HEAD tree via gitSubmoduleGitlinkSHA, threaded through
// FixtureContext.PinnedSHAResolver: reading a gitlink is a purely local
// tree read (see gitSubmoduleGitlinkSHA's doc comment), so goCtx and
// repoPath are the only git-side inputs this emit path needs — unlike the
// sync package's gitRun helpers, no RepoSyncConfig/token threading is
// required here.
func emitSubmoduleFactsForCandidates(
	goCtx context.Context,
	w factStreamWriter,
	repoID string,
	repoPath string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	candidates map[string]string,
) {
	body, ok := candidates[".gitmodules"]
	if !ok {
		return
	}
	fixtureCtx := submodule.FixtureContext{
		ScopeID:      scopeID,
		GenerationID: generationID,
		ObservedAt:   observedAt,
		SourceURI:    ".gitmodules",
	}
	instanceID := gitSubmoduleCollectorInstanceID
	fixtureCtx.CollectorInstanceID = &instanceID
	fixtureCtx.PinnedSHAResolver = func(submodulePath string) *string {
		return gitSubmoduleGitlinkSHA(goCtx, repoPath, submodulePath)
	}
	for _, envelope := range submodule.Emit(fixtureCtx, repoID, ".gitmodules", body) {
		w.send(envelope)
	}
}
