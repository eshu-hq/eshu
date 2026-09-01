// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagesourcecore

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// Hint is one package registry source_hint fact, reduced to the fields
// package-source matching needs.
type Hint struct {
	FactID    string
	PackageID string
	VersionID string
	HintKind  string
	SourceURL string
}

// Repository is one repository fact, reduced to the fields a source hint is
// matched against.
type Repository struct {
	RepositoryID   string
	RepositoryName string
	RemoteURL      string
	Tombstone      bool
}

// ExtractRepositories collects repository facts out of envelopes. Each
// repository's ID is its graph_id, repo_id, or repository_id payload field,
// falling back to the ID embedded in its scope when none of those are set. A
// repository with no derivable ID is dropped. The result is sorted by
// RepositoryID.
func ExtractRepositories(envelopes []facts.Envelope) []Repository {
	repositories := make([]Repository, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != factload.FactKindRepository {
			continue
		}
		repositoryID := payloadcore.FirstNonBlank(
			payloadcore.PayloadStr(envelope.Payload, "graph_id"),
			payloadcore.PayloadStr(envelope.Payload, "repo_id"),
			payloadcore.PayloadStr(envelope.Payload, "repository_id"),
			RepositoryIDFromScope(envelope.ScopeID),
		)
		if repositoryID == "" {
			continue
		}
		repositories = append(repositories, Repository{
			RepositoryID:   repositoryID,
			RepositoryName: payloadcore.PayloadStr(envelope.Payload, "name"),
			RemoteURL:      payloadcore.PayloadStr(envelope.Payload, "remote_url"),
			Tombstone:      envelope.IsTombstone,
		})
	}
	sort.SliceStable(repositories, func(i, j int) bool {
		return repositories[i].RepositoryID < repositories[j].RepositoryID
	})
	return repositories
}

// RepositoryIDFromScope extracts the repository identity from a
// "git-repository-scope:<id>" reducer scope ID. Unlike
// [payloadcore.RepositoryIDFromReducerScope], it returns the whole scope ID,
// trimmed, when the scope carries no such prefix rather than "" -- this is the
// fallback ExtractRepositories uses when a repository fact carries no explicit
// ID field, so it must keep its original, looser behavior.
func RepositoryIDFromScope(scopeID string) string {
	const prefix = "git-repository-scope:"
	return strings.TrimSpace(strings.TrimPrefix(scopeID, prefix))
}

// MatchRepositories partitions repositories whose canonical remote-URL key
// equals the hint's canonical source-URL key into active and tombstoned
// (stale) matches. It returns (nil, nil) when the hint's source URL has no
// canonical key.
func MatchRepositories(
	hint Hint,
	repositories []Repository,
) ([]Repository, []Repository) {
	hintKey := CanonicalURLKey(hint.SourceURL)
	if hintKey == "" {
		return nil, nil
	}
	var activeMatches []Repository
	var staleMatches []Repository
	for _, repository := range repositories {
		if CanonicalURLKey(repository.RemoteURL) != hintKey {
			continue
		}
		if repository.Tombstone {
			staleMatches = append(staleMatches, repository)
			continue
		}
		activeMatches = append(activeMatches, repository)
	}
	return activeMatches, staleMatches
}

// CanonicalURLKey returns the canonical host/path key for a git remote URL. It
// delegates to repositoryidentity.NormalizedRemoteKey so the reducer and the
// git collector share one normalization path.
func CanonicalURLKey(raw string) string {
	return repositoryidentity.NormalizedRemoteKey(raw)
}
