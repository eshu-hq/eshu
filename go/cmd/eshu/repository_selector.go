package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

type repositoryListResponse struct {
	Repositories []repositorySelectorEntry `json:"repositories"`
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
	for _, repo := range response.Repositories {
		if !repositorySelectorMatches(repo, selector) {
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
	switch selector {
	case repo.ID, repo.Name, repo.Path, repo.LocalPath, repo.RepoSlug:
		return true
	default:
		return false
	}
}
