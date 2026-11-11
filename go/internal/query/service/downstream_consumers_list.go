// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Consumer list paging bounds. The page size is caller-chosen within the
// clamp; the internal scan bound is fixed at the ceiling so every page
// recomputes the same merged set and the keyset drop stays exact (#10535).
const (
	// ConsumerListDefaultPageSize is the page size when the caller names none.
	ConsumerListDefaultPageSize = 50
	// ConsumerListMaxPageSize caps a single page, mirroring the endpoint list.
	ConsumerListMaxPageSize = 100
)

// ConsumerListCursor is a keyset position: the last row the caller has seen.
// Rows sort by (repository, repo_id), so dropping every row at or below the
// cursor yields the next page. The cursor names only granted rows, because
// the grant filter runs before the keyset drop.
type ConsumerListCursor struct {
	Repository string
	RepoID     string
}

// ConsumerListPage is one page over the recomputed merged consumer set.
type ConsumerListPage struct {
	Consumers []map[string]any
	// Truncated reports more merged rows past this page OR an upstream bound
	// (or unpageable source) that fired on the read.
	Truncated bool
	// NextCursor is nil on a terminal page.
	NextCursor *ConsumerListCursor
	// HasNext reports whether more merged rows exist past this page.
	HasNext bool
	// Sources names which truncation bounds fired on the read that built
	// this page.
	Sources deployment.ConsumerEnrichmentTruncation
	// SearchedHostnames is the bounded hostname list actually searched.
	SearchedHostnames []string
	// EffectiveBound is the internal read bound the page was recomputed under.
	EffectiveBound int
	// LowerBound stays true even on a complete page: the set is everything
	// the bounded read returned, never a census of all consumers.
	LowerBound bool
}

// selectConsumerPage drops every row at or below the cursor and returns one
// page. When more rows remain than fit the page, the returned next cursor
// names the page's last row; the caller re-runs the recomputed read and
// drops through that cursor for the following page. An empty remainder is a
// terminal page with no cursor, and the page slice is always non-nil.
func selectConsumerPage(sorted []map[string]any, after *ConsumerListCursor, pageSize int) (page []map[string]any, nextCursor *ConsumerListCursor, hasNext bool) {
	rest := sorted
	if after != nil {
		rest = make([]map[string]any, 0, len(sorted))
		for _, row := range sorted {
			repo, id := querycontract.StringVal(row, "repository"), querycontract.StringVal(row, "repo_id")
			if repo < after.Repository || (repo == after.Repository && id <= after.RepoID) {
				continue
			}
			rest = append(rest, row)
		}
	}
	if len(rest) > pageSize {
		last := rest[pageSize-1]
		return rest[:pageSize], &ConsumerListCursor{
			Repository: querycontract.StringVal(last, "repository"),
			RepoID:     querycontract.StringVal(last, "repo_id"),
		}, true
	}
	if rest == nil {
		return []map[string]any{}, nil, false
	}
	return rest, nil, false
}

// sortConsumerRepos orders merged consumer rows by (repository, repo_id).
// Both keys are always present: graph entries carry the display name,
// content-only entries carry the repo id until backfill names them.
func sortConsumerRepos(rows []map[string]any) {
	sort.Slice(rows, func(i, j int) bool {
		leftRepo, rightRepo := querycontract.StringVal(rows[i], "repository"), querycontract.StringVal(rows[j], "repository")
		if leftRepo != rightRepo {
			return leftRepo < rightRepo
		}
		return querycontract.StringVal(rows[i], "repo_id") < querycontract.StringVal(rows[j], "repo_id")
	})
}

// ListServiceDownstreamConsumers recomputes the merged consumer set at the
// widened internal bound and returns one keyset page of it (#10535). The
// bound is fixed at the ceiling so every page recomputes the identical set
// and the cursor drop stays exact; grant filtering runs on the merged set
// before the drop and the page cut, so no page names an ungranted repo.
func ListServiceDownstreamConsumers(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	serviceRepoID, serviceName string,
	access querycontract.RepositoryAccessFilter,
	after *ConsumerListCursor,
	pageSize int,
) (ConsumerListPage, error) {
	if pageSize <= 0 {
		pageSize = ConsumerListDefaultPageSize
	}
	if pageSize > ConsumerListMaxPageSize {
		pageSize = ConsumerListMaxPageSize
	}
	bound := deployment.MaxConsumerScanBound

	evidence, err := LoadServiceQueryEvidence(ctx, content, serviceRepoID, serviceName)
	if err != nil {
		return ConsumerListPage{}, err
	}
	hostnames := serviceEvidenceHostnames(evidence)
	searchedHostnames, _ := deployment.TrimmedSearchHostnames(hostnames, serviceName, bound)
	if searchedHostnames == nil {
		// The OpenAPI contract declares searched_hostnames as a
		// non-nullable array; disclose an empty list, not null.
		searchedHostnames = []string{}
	}

	candidates, candidatesTruncated, err := deployment.QueryProvisioningRepositoryCandidatesCapped(ctx, graph, serviceRepoID, bound, bound)
	if err != nil {
		return ConsumerListPage{}, err
	}
	candidates = deployment.FilterProvisioningRepositoryCandidatesForAccess(candidates, access)

	merged, sources, err := deployment.LoadConsumerRepositoryEnrichmentWithSources(
		ctx, graph, content, serviceRepoID, serviceName, hostnames, bound,
		candidates, candidatesTruncated, evidence.filesTruncated,
	)
	if err != nil {
		return ConsumerListPage{}, err
	}
	granted := deployment.FilterRowsByRepoIDForAccess(merged, access)
	sortConsumerRepos(granted)

	// The merged set is trimmed at the bound inside the enrichment; anything
	// past it is unreachable at this bound, so a terminal page there names
	// the ceiling explicitly.
	page, next, hasNext := selectConsumerPage(granted, after, pageSize)
	out := ConsumerListPage{
		Consumers:         page,
		HasNext:           hasNext,
		NextCursor:        next,
		Sources:           sources,
		SearchedHostnames: searchedHostnames,
		EffectiveBound:    bound,
		LowerBound:        true,
	}
	out.Truncated = hasNext || sources.Any()
	return out, nil
}
