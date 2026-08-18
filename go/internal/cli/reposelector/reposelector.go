// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reposelector

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Getter is the one method this package needs from the CLI's HTTP API client:
// issue a GET against an Eshu API path and JSON-decode the response body into
// result.
//
// The interface is declared here, at the point of use, rather than shared from
// go/cmd/eshu: that package is `package main` and cannot be imported, and its
// *APIClient resolves the service URL, API key, and profile from cobra flags,
// the process environment, and the on-disk config file -- process state this
// package must not reach for. The cobra wrapper resolves all of it and passes
// the built client in. *APIClient satisfies Getter as written.
type Getter interface {
	// Get issues a GET to path, relative to the client's base URL, and
	// decodes the JSON response into result.
	Get(path string, result any) error
}

// ListResponse is the `/api/v0/repositories` payload as the CLI consumes it.
// Only the fields the selector and its sibling command families read are
// declared; the API sends more.
type ListResponse struct {
	Repositories []Entry `json:"repositories"`
	// Total is the true repository count independent of page size, added in
	// issue #3392 so callers can display the accurate total without paging
	// through the entire dataset.
	Total int `json:"total"`
}

// Entry is one repository as the selector matches against it. Every field is a
// selector candidate: ID, Name and RepoSlug match exactly, while Path and
// LocalPath additionally match as filesystem paths.
type Entry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	LocalPath string `json:"local_path"`
	RepoSlug  string `json:"repo_slug"`
}

// Resolve turns an operator-supplied repository selector into a canonical
// repository ID by listing repositories through client and matching each entry
// with the rules Matches documents.
//
// It is an error for the selector to match nothing, and an error for it to
// match more than one repository: an ambiguous selector names the matching IDs
// in sorted order so the operator can re-run with an exact one rather than
// silently getting whichever repository sorted first.
func Resolve(client Getter, selector string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("resolve repo selector %q: missing API client", selector)
	}

	var response ListResponse
	if err := client.Get("/api/v0/repositories", &response); err != nil {
		return "", fmt.Errorf("resolve repo selector %q: %w", selector, err)
	}

	matches := make([]string, 0, 1)
	seen := make(map[string]struct{})
	matcher := newMatcher(selector)
	for _, repo := range response.Repositories {
		if !matcher.matches(repo) {
			continue
		}
		if _, ok := seen[repo.ID]; ok {
			continue
		}
		seen[repo.ID] = struct{}{}
		matches = append(matches, repo.ID)
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("resolve repo selector %q: no matching repository", selector)
	case 1:
		return matches[0], nil
	default:
		slices.Sort(matches)
		return "", fmt.Errorf("resolve repo selector %q: multiple repositories match: %s", selector, strings.Join(matches, ", "))
	}
}

// Matches reports whether repo is named by selector. ID, Name and RepoSlug
// must match the selector byte for byte; Path and LocalPath additionally match
// when they name the same location after cleaning, or after resolving symlinks
// on both sides. An empty selector matches nothing.
//
// The asymmetry is deliberate: a repository whose Name happens to look like a
// path must not be canonicalized into matching a different path, which is what
// TestRepositorySelectorCanonicalizesOnlyPathFields pins.
func Matches(repo Entry, selector string) bool {
	return newMatcher(selector).matches(repo)
}

// matcher caches the per-selector work so a single Resolve call canonicalizes
// the selector once instead of once per repository in the listing.
type matcher struct {
	selector        string
	cleanSelector   string
	realSelector    string
	hasRealSelector bool
}

// newMatcher builds the cached forms of selector. The symlink resolution runs
// against the real filesystem and is best-effort: a selector that names no
// existing path (an ID, a name, a slug, or a path on another machine) simply
// leaves hasRealSelector false, and matching falls back to the cleaned form.
func newMatcher(selector string) matcher {
	selector = strings.TrimSpace(selector)
	m := matcher{
		selector:      selector,
		cleanSelector: filepath.Clean(selector),
	}
	if selector == "" {
		return m
	}
	if realSelector, err := filepath.EvalSymlinks(selector); err == nil {
		m.realSelector = realSelector
		m.hasRealSelector = true
	}
	return m
}

// matches applies the exact-then-path rules Matches documents.
func (m matcher) matches(repo Entry) bool {
	if m.selector == "" {
		return false
	}
	if repo.ID == m.selector || repo.Name == m.selector || repo.RepoSlug == m.selector {
		return true
	}
	if repo.Path == m.selector || repo.LocalPath == m.selector {
		return true
	}
	return pathMatches(repo.Path, m) ||
		pathMatches(repo.LocalPath, m)
}

// pathMatches compares one repository path field against the selector as a
// filesystem path: equal after filepath.Clean, or equal after resolving
// symlinks on both sides. Symlink resolution is attempted only when the
// selector itself resolved, so a selector that names nothing on disk cannot
// match a candidate merely because the candidate resolves.
func pathMatches(candidate string, m matcher) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || m.selector == "" {
		return false
	}
	if filepath.Clean(candidate) == m.cleanSelector {
		return true
	}
	if !m.hasRealSelector {
		return false
	}
	candidateReal, err := filepath.EvalSymlinks(candidate)
	return err == nil && candidateReal == m.realSelector
}
