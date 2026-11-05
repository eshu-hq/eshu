// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"path"
	"sort"
	"strings"

	codegraphv1 "github.com/eshu-hq/eshu-sdk/factschema/codegraph/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// DeltaScopeUnusableReason names why a delta-generation repository's changed
// paths could not be qualified into a code-call file scope (#10023). The
// values are a closed set so they can label a metric without unbounded
// cardinality.
type DeltaScopeUnusableReason string

const (
	// DeltaScopeUnusableMissingLocalPath means the repository fact carried
	// no local checkout path (absent or whitespace-only), so no changed path
	// can be joined into the absolute file path the retract binds.
	DeltaScopeUnusableMissingLocalPath DeltaScopeUnusableReason = "missing_local_path"
	// DeltaScopeUnusableUnnormalizablePath means at least one changed or
	// deleted path was not a clean repository-relative path (absolute,
	// escaping the root, or not in canonical form). One bad path makes the
	// whole repository's scope unusable, because a partial scope would leave
	// that file's stale edges behind.
	DeltaScopeUnusableUnnormalizablePath DeltaScopeUnusableReason = "unnormalizable_path"
	// DeltaScopeUnusableNoChangedPaths means the delta generation carried
	// no changed or deleted paths at all, so there is nothing to scope to.
	DeltaScopeUnusableNoChangedPaths DeltaScopeUnusableReason = "no_changed_paths"
)

// deltaScopeUnusableRank orders the reasons so a repository with several
// repository facts reports one deterministic reason: the most basic defect
// wins.
func deltaScopeUnusableRank(reason DeltaScopeUnusableReason) int {
	switch reason {
	case DeltaScopeUnusableMissingLocalPath:
		return 3
	case DeltaScopeUnusableUnnormalizablePath:
		return 2
	case DeltaScopeUnusableNoChangedPaths:
		return 1
	default:
		return 0
	}
}

// codeCallDeltaScopes is everything the delta-generation repository facts of
// one batch say about code-call file scoping.
type codeCallDeltaScopes struct {
	// scopesByRepoID holds the usable delta file scopes.
	scopesByRepoID map[string]DeltaFileScope
	// repositoryIDs holds every repository whose repository fact carried
	// delta_generation and decoded, whether or not its scope was usable.
	repositoryIDs map[string]struct{}
	// unusableByRepoID holds the delta repositories whose scope is unusable,
	// with the reason. It is disjoint from scopesByRepoID.
	unusableByRepoID map[string]DeltaScopeUnusableReason
}

func buildCodeCallDeltaFilePathsByRepoID(envelopes []facts.Envelope) map[string][]string {
	scopesByRepoID := buildCodeCallDeltaFileScopesByRepoID(envelopes)
	if len(scopesByRepoID) == 0 {
		return nil
	}

	pathsByRepoID := make(map[string][]string, len(scopesByRepoID))
	for repositoryID, scope := range scopesByRepoID {
		pathsByRepoID[repositoryID] = append([]string(nil), scope.filePaths...)
	}
	return pathsByRepoID
}

// buildCodeCallDeltaFileScopesByRepoID returns the usable delta file scopes
// only. See collectCodeCallDeltaScopes for the full result.
func buildCodeCallDeltaFileScopesByRepoID(envelopes []facts.Envelope) map[string]DeltaFileScope {
	return collectCodeCallDeltaScopes(envelopes).scopesByRepoID
}

// collectCodeCallDeltaScopes decodes each delta-generation "repository" fact's
// outer envelope through the codegraph contracts seam
// (schemadecode.DecodeCodegraphRepository) to recover its join identity
// (RepoID) and the delta path slices, then builds the delta file scopes. The
// cheap delta_generation gate check runs on the raw payload before decode so a
// non-delta repository fact (the overwhelming majority) never pays the decode
// cost. A delta-generation repository fact whose payload is missing a required
// identity field is skipped, matching the pre-existing "skip and continue"
// shape for an absent identity.
//
// Unlike the scopes map, the result also keeps every delta repository and the
// reason any of them has no usable scope (#10023). Dropping those repositories
// silently made a delta repository with an unusable scope indistinguishable
// from a full-generation repository with no file scope, so the refresh builder
// fell back to a whole-repository retract with delta-only rows.
//
// The repository checkout path is resolved from the raw "path" key first, then
// the typed LocalPath, preserving the exact pre-Contract-System precedence.
// "path" is NOT a typed Repository field: gitcontent.RepositoryFactEnvelope
// never writes it to the payload (it routes the checkout path to
// SourceRef.SourceURI), so in production this raw read is always absent and
// LocalPath is used. It is read raw here only to preserve behavior for callers
// (and tests) that carry the checkout path under "path".
func collectCodeCallDeltaScopes(envelopes []facts.Envelope) codeCallDeltaScopes {
	result := codeCallDeltaScopes{}
	seenByRepoID := make(map[string]map[string]struct{})
	repoPathByRepoID := make(map[string]string)
	markUnusable := func(repositoryID string, reason DeltaScopeUnusableReason) {
		if result.unusableByRepoID == nil {
			result.unusableByRepoID = make(map[string]DeltaScopeUnusableReason)
		}
		if deltaScopeUnusableRank(reason) > deltaScopeUnusableRank(result.unusableByRepoID[repositoryID]) {
			result.unusableByRepoID[repositoryID] = reason
		}
	}
	for _, env := range envelopes {
		if env.FactKind != factload.FactKindRepository || !PayloadBool(env.Payload, "delta_generation") {
			continue
		}
		repository, err := schemadecode.DecodeCodegraphRepository(env)
		if err != nil {
			continue
		}
		// TrimSpace + skip-empty preserves the pre-Contract-System
		// semanticPayloadString behavior: a whitespace-only repo_id must not
		// create an entry under a non-canonical key.
		repositoryID := strings.TrimSpace(repository.RepoID)
		if repositoryID == "" {
			continue
		}
		if result.repositoryIDs == nil {
			result.repositoryIDs = make(map[string]struct{})
		}
		result.repositoryIDs[repositoryID] = struct{}{}

		repoPath := payloadcore.SemanticPayloadString(env.Payload, "path")
		if repoPath == "" && repository.LocalPath != nil {
			repoPath = *repository.LocalPath
		}
		if strings.TrimSpace(repoPath) == "" {
			markUnusable(repositoryID, DeltaScopeUnusableMissingLocalPath)
			continue
		}
		for _, relativePath := range codeCallDeltaRelativePathsFromRepository(repository) {
			cleanedRelativePath, ok := normalizeCodeCallDeltaRelativePath(relativePath)
			if !ok {
				markUnusable(repositoryID, DeltaScopeUnusableUnnormalizablePath)
				continue
			}
			seen := seenByRepoID[repositoryID]
			if seen == nil {
				seen = make(map[string]struct{})
				seenByRepoID[repositoryID] = seen
			}
			seen[cleanedRelativePath] = struct{}{}
			repoPathByRepoID[repositoryID] = repoPath
		}
	}
	for repositoryID := range result.repositoryIDs {
		if _, unusable := result.unusableByRepoID[repositoryID]; unusable {
			continue
		}
		if len(seenByRepoID[repositoryID]) == 0 {
			markUnusable(repositoryID, DeltaScopeUnusableNoChangedPaths)
		}
	}

	for repositoryID, seen := range seenByRepoID {
		if _, unusable := result.unusableByRepoID[repositoryID]; unusable {
			continue
		}
		repoPath := repoPathByRepoID[repositoryID]
		partitionPaths := make([]string, 0, len(seen))
		for relativePath := range seen {
			partitionPaths = append(partitionPaths, relativePath)
		}
		sort.Strings(partitionPaths)

		filePaths := make([]string, 0, len(partitionPaths))
		for _, relativePath := range partitionPaths {
			filePaths = append(filePaths, path.Join(repoPath, relativePath))
		}
		if result.scopesByRepoID == nil {
			result.scopesByRepoID = make(map[string]DeltaFileScope, len(seenByRepoID))
		}
		result.scopesByRepoID[repositoryID] = DeltaFileScope{
			filePaths:      filePaths,
			partitionPaths: partitionPaths,
		}
	}
	return result
}

// codeCallDeltaRelativePathsFromRepository returns the deduplicated union of
// a decoded codegraphv1.Repository's DeltaRelativePaths and
// DeltaDeletedRelativePaths: the changed and deleted file paths a delta
// generation carries. It is the typed-decode replacement for the pre-Contract-
// System raw payload["delta_relative_paths"]/["delta_deleted_relative_paths"]
// reads.
func codeCallDeltaRelativePathsFromRepository(repository codegraphv1.Repository) []string {
	seen := make(map[string]struct{})
	var paths []string
	for _, relativePath := range repository.DeltaRelativePaths {
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}
		paths = append(paths, relativePath)
	}
	for _, relativePath := range repository.DeltaDeletedRelativePaths {
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}
		paths = append(paths, relativePath)
	}
	return paths
}
