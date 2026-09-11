// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package readmodel

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Ref is one source-backed repository branch/ref head. It is an
// alias onto querycontract so the shared ContentStore double can name it from
// outside this package (#6060).
type Ref = querycontract.RepositoryRef

type repositoryRefLister interface {
	ListRepositoryRefs(context.Context, string) ([]querycontract.RepositoryRef, error)
}

func Refs(ctx context.Context, store querycontract.ContentStore, repoID string) ([]querycontract.RepositoryRef, error) {
	if store == nil {
		return nil, nil
	}
	lister, ok := store.(repositoryRefLister)
	if !ok {
		return nil, nil
	}
	return lister.ListRepositoryRefs(ctx, repoID)
}

func RefsDefaultBranch(refs []querycontract.RepositoryRef) string {
	for _, ref := range refs {
		if ref.Default {
			return strings.TrimSpace(ref.Name)
		}
	}
	return ""
}

// RefEntry builds the wire entry for one repository ref.
// includeDefault controls whether the is_default field appears;
// branches always include it (legacy contract), tags never include it.
func RefEntry(ref querycontract.RepositoryRef, includeDefault bool) map[string]any {
	entry := map[string]any{
		"name":     ref.Name,
		"kind":     ref.Kind,
		"head_sha": ref.HeadSHA,
	}
	if includeDefault {
		entry["is_default"] = ref.Default
	}
	if !ref.ObservedAt.IsZero() {
		entry["observed_at"] = querycontract.FormatCoverageTimestamp(ref.ObservedAt)
	}
	if !ref.IndexedAt.IsZero() {
		entry["last_indexed_at"] = querycontract.FormatCoverageTimestamp(ref.IndexedAt)
	}
	return entry
}

func ValidateSelectedRepositoryRef(
	ctx context.Context,
	store querycontract.ContentStore,
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

	refs, err := Refs(ctx, store, repoID)
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
