// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

type repositorySelectorNotFoundError struct {
	Selector string
}

func (e repositorySelectorNotFoundError) Error() string {
	return fmt.Sprintf("repository selector %q did not match any indexed repository", e.Selector)
}

type repositorySelectorAmbiguousError struct {
	Selector string
	Matches  []string
}

func (e repositorySelectorAmbiguousError) Error() string {
	return fmt.Sprintf("repository selector %q matched multiple repositories: %s", e.Selector, strings.Join(e.Matches, ", "))
}

func resolveRepositorySelectorExact(ctx context.Context, graph GraphQuery, content ContentStore, selector string) (string, error) {
	return resolveRepositorySelectorExactForAccess(ctx, graph, content, selector, repositoryAccessFilter{allScopes: true})
}

func resolveRepositorySelectorExactForAccess(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	selector string,
	access repositoryAccessFilter,
) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", nil
	}
	if looksCanonicalRepositoryID(selector) {
		if !access.allowsRepositoryID(selector) {
			return "", repositorySelectorNotFoundError{Selector: selector}
		}
		return selector, nil
	}

	if content != nil {
		entries, err := content.MatchRepositories(ctx, selector)
		if err != nil {
			return "", fmt.Errorf("match repositories: %w", err)
		}
		entries = access.filterCatalogEntries(entries)
		matches := resolveRepositoryCatalogMatches(entries, selector)
		switch len(matches) {
		case 0:
		case 1:
			return matches[0], nil
		default:
			return "", repositorySelectorAmbiguousError{Selector: selector, Matches: matches}
		}
	}

	if graph != nil {
		if access.empty() {
			return "", repositorySelectorNotFoundError{Selector: selector}
		}
		rows, err := graph.Run(ctx, `
			MATCH (r:Repository)
			WHERE (
			   r.id = $repo_selector
			   OR r.name = $repo_selector
			   OR r.path = $repo_selector
			   OR r.local_path = $repo_selector
			   OR r.remote_url = $repo_selector
			   OR r.repo_slug = $repo_selector
			)
			`+access.graphPredicate("r")+`
			RETURN r.id as id
			ORDER BY r.id
		`, access.graphParams(map[string]any{"repo_selector": selector}))
		if err != nil {
			return "", fmt.Errorf("query graph repository selector: %w", err)
		}
		switch len(rows) {
		case 0:
			row, err := graph.RunSingle(ctx, `
				MATCH (r:Repository)
				WHERE (
				   r.id = $repo_selector
				   OR r.name = $repo_selector
				   OR r.path = $repo_selector
				   OR r.local_path = $repo_selector
				   OR r.remote_url = $repo_selector
				   OR r.repo_slug = $repo_selector
				)
				`+access.graphPredicate("r")+`
				RETURN r.id as id
			`, access.graphParams(map[string]any{"repo_selector": selector}))
			if err != nil {
				return "", fmt.Errorf("query graph repository selector: %w", err)
			}
			if row != nil {
				return StringVal(row, "id"), nil
			}
		case 1:
			return StringVal(rows[0], "id"), nil
		default:
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				id := StringVal(row, "id")
				if id == "" {
					continue
				}
				ids = append(ids, id)
			}
			slices.Sort(ids)
			return "", repositorySelectorAmbiguousError{Selector: selector, Matches: ids}
		}
	}

	return "", repositorySelectorNotFoundError{Selector: selector}
}

// resolveRepositorySelectorForRequestWithAccess resolves a repository selector
// and writes the failure response itself, reporting false when it did.
//
// capability names the caller's capability for the bounded graph-read envelope.
// Selector resolution issues its own graph reads, so a backend timeout or
// outage here must surface as the same 503/504 contract every other
// graph-backed read uses. Without that mapping it fell through to the generic
// branch below and reported HTTP 400, telling the client its request was
// malformed when nothing was wrong with the request at all.
func resolveRepositorySelectorForRequestWithAccess(
	w http.ResponseWriter,
	r *http.Request,
	graph GraphQuery,
	content ContentStore,
	selector string,
	access repositoryAccessFilter,
	capability string,
) (string, bool) {
	repoID, err := resolveRepositorySelectorExactForAccess(r.Context(), graph, content, selector, access)
	if err != nil {
		if WriteGraphReadError(w, r, err, capability) {
			return "", false
		}
		status := http.StatusBadRequest
		if isRepositorySelectorNotFound(err) {
			status = http.StatusNotFound
		}
		WriteError(w, status, err.Error())
		return "", false
	}
	return repoID, true
}

func isRepositorySelectorNotFound(err error) bool {
	var target repositorySelectorNotFoundError
	return errors.As(err, &target)
}

func looksCanonicalRepositoryID(selector string) bool {
	return strings.HasPrefix(selector, "repo://") ||
		strings.HasPrefix(selector, "repo-") ||
		strings.HasPrefix(selector, "repository:")
}

func resolveRepositoryCatalogMatches(entries []RepositoryCatalogEntry, selector string) []string {
	if strings.TrimSpace(selector) == "" {
		return nil
	}
	matches := make([]string, 0, 1)
	seen := make(map[string]struct{})
	for _, entry := range entries {
		switch selector {
		case entry.ID, entry.Name, entry.Path, entry.LocalPath, entry.RemoteURL, entry.RepoSlug:
			if entry.ID == "" {
				continue
			}
			if _, ok := seen[entry.ID]; ok {
				continue
			}
			seen[entry.ID] = struct{}{}
			matches = append(matches, entry.ID)
		}
	}
	slices.Sort(matches)
	return matches
}
