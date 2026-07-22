// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"time"
)

// RepositoryRef is one source-backed repository branch/ref head.
type RepositoryRef struct {
	Name       string
	Kind       string
	HeadSHA    string
	Default    bool
	ObservedAt time.Time
	IndexedAt  time.Time
}

type repositoryRefLister interface {
	ListRepositoryRefs(context.Context, string) ([]RepositoryRef, error)
}

func repositoryRefs(ctx context.Context, store ContentStore, repoID string) ([]RepositoryRef, error) {
	if store == nil {
		return nil, nil
	}
	lister, ok := store.(repositoryRefLister)
	if !ok {
		return nil, nil
	}
	return lister.ListRepositoryRefs(ctx, repoID)
}

func repositoryRefsDefaultBranch(refs []RepositoryRef) string {
	for _, ref := range refs {
		if ref.Default {
			return strings.TrimSpace(ref.Name)
		}
	}
	return ""
}

// repositoryRefEntry builds the wire entry for one repository ref.
// includeDefault controls whether the is_default field appears;
// branches always include it (legacy contract), tags never include it.
func repositoryRefEntry(ref RepositoryRef, includeDefault bool) map[string]any {
	entry := map[string]any{
		"name":     ref.Name,
		"kind":     ref.Kind,
		"head_sha": ref.HeadSHA,
	}
	if includeDefault {
		entry["is_default"] = ref.Default
	}
	if !ref.ObservedAt.IsZero() {
		entry["observed_at"] = formatCoverageTimestamp(ref.ObservedAt)
	}
	if !ref.IndexedAt.IsZero() {
		entry["last_indexed_at"] = formatCoverageTimestamp(ref.IndexedAt)
	}
	return entry
}

func validateSelectedRepositoryRef(
	ctx context.Context,
	store ContentStore,
	repoID string,
	requestedRef string,
	indexedCommit string,
) (int, string, error) {
	requestedRef = strings.TrimSpace(requestedRef)
	if requestedRef == "" {
		return 0, "", nil
	}
	if indexedCommit != "" && requestedRef == indexedCommit {
		return 0, "", nil
	}

	refs, err := repositoryRefs(ctx, store, repoID)
	if err != nil {
		return 0, "", err
	}
	if len(refs) == 0 {
		if indexedCommit != "" && requestedRef == indexedCommit {
			return 0, "", nil
		}
		return 409, "repository branch metadata unavailable; selected ref cannot be verified", nil
	}

	for _, ref := range refs {
		if requestedRef != ref.Name && requestedRef != ref.HeadSHA {
			continue
		}
		if ref.HeadSHA == indexedCommit {
			return 0, "", nil
		}
		return 409, "selected ref is not indexed", nil
	}
	return 404, "ref not found", nil
}
