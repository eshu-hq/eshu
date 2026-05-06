package query

import (
	"context"
	"errors"
	"fmt"
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
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", nil
	}
	if looksCanonicalRepositoryID(selector) {
		return selector, nil
	}

	if content != nil {
		entries, err := content.MatchRepositories(ctx, selector)
		if err != nil {
			return "", fmt.Errorf("match repositories: %w", err)
		}
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
		rows, err := graph.Run(ctx, `
			MATCH (r:Repository)
			WHERE r.id = $repo_selector OR r.name = $repo_selector
			RETURN r.id as id
			ORDER BY r.id
		`, map[string]any{"repo_selector": selector})
		if err != nil {
			return "", fmt.Errorf("query graph repository selector: %w", err)
		}
		switch len(rows) {
		case 0:
			row, err := graph.RunSingle(ctx, `
				MATCH (r:Repository)
				WHERE r.id = $repo_selector OR r.name = $repo_selector
				RETURN r.id as id
			`, map[string]any{"repo_selector": selector})
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

func isRepositorySelectorNotFound(err error) bool {
	var target repositorySelectorNotFoundError
	return errors.As(err, &target)
}

func looksCanonicalRepositoryID(selector string) bool {
	return strings.HasPrefix(selector, "repo-") || strings.HasPrefix(selector, "repository:")
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
