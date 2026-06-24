// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

type repositoryListResponse struct {
	Repositories []repositorySelectorEntry `json:"repositories"`
	// Total is the true repository count independent of page size, added in
	// issue #3392 so callers can display the accurate total without paging
	// through the entire dataset.
	Total int `json:"total"`
}

type repositorySelectorEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	LocalPath string `json:"local_path"`
	RepoSlug  string `json:"repo_slug"`
}

func resolveRepositorySelectorFromFlags(cmd *cobra.Command, client *APIClient) (string, error) {
	selector, exact, err := readRepositorySelectorFlag(cmd)
	if err != nil {
		return "", err
	}
	if selector == "" {
		return "", nil
	}
	if exact {
		return selector, nil
	}
	return resolveRepositorySelector(cmd, client, selector)
}

func readRepositorySelectorFlag(cmd *cobra.Command) (string, bool, error) {
	if cmd == nil {
		return "", false, nil
	}
	if cmd.Flags().Lookup("repo") != nil {
		value, err := cmd.Flags().GetString("repo")
		if err != nil {
			return "", false, fmt.Errorf("read repo flag: %w", err)
		}
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), false, nil
		}
	}
	if cmd.Flags().Lookup("repo-id") != nil {
		value, err := cmd.Flags().GetString("repo-id")
		if err != nil {
			return "", false, fmt.Errorf("read repo-id flag: %w", err)
		}
		return strings.TrimSpace(value), true, nil
	}
	return "", false, nil
}

func resolveRepositorySelector(_ *cobra.Command, client *APIClient, selector string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("resolve repo selector %q: missing API client", selector)
	}

	var response repositoryListResponse
	if err := client.Get("/api/v0/repositories", &response); err != nil {
		return "", fmt.Errorf("resolve repo selector %q: %w", selector, err)
	}

	matches := make([]string, 0, 1)
	seen := make(map[string]struct{})
	matcher := newRepositorySelectorMatcher(selector)
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

func repositorySelectorMatches(repo repositorySelectorEntry, selector string) bool {
	return newRepositorySelectorMatcher(selector).matches(repo)
}

type repositorySelectorMatcher struct {
	selector        string
	cleanSelector   string
	realSelector    string
	hasRealSelector bool
}

func newRepositorySelectorMatcher(selector string) repositorySelectorMatcher {
	selector = strings.TrimSpace(selector)
	matcher := repositorySelectorMatcher{
		selector:      selector,
		cleanSelector: filepath.Clean(selector),
	}
	if selector == "" {
		return matcher
	}
	if realSelector, err := filepath.EvalSymlinks(selector); err == nil {
		matcher.realSelector = realSelector
		matcher.hasRealSelector = true
	}
	return matcher
}

func (m repositorySelectorMatcher) matches(repo repositorySelectorEntry) bool {
	if m.selector == "" {
		return false
	}
	if repo.ID == m.selector || repo.Name == m.selector || repo.RepoSlug == m.selector {
		return true
	}
	if repo.Path == m.selector || repo.LocalPath == m.selector {
		return true
	}
	return repositoryPathSelectorMatches(repo.Path, m) ||
		repositoryPathSelectorMatches(repo.LocalPath, m)
}

func repositoryPathSelectorMatches(candidate string, matcher repositorySelectorMatcher) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || matcher.selector == "" {
		return false
	}
	if filepath.Clean(candidate) == matcher.cleanSelector {
		return true
	}
	if !matcher.hasRealSelector {
		return false
	}
	candidateReal, err := filepath.EvalSymlinks(candidate)
	return err == nil && candidateReal == matcher.realSelector
}
