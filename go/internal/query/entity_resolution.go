package query

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

const graphEntityResolutionLimit = 50

func resolveExactGraphEntityCandidate(
	ctx context.Context,
	reader ContentStore,
	repoID string,
	name string,
) (*EntityContent, error) {
	exact, err := resolveExactGraphEntityCandidates(ctx, reader, repoID, name)
	if err != nil {
		return nil, err
	}
	return selectExactGraphEntityCandidate(repoID, name, exact)
}

func resolveExactGraphEntityCandidates(
	ctx context.Context,
	reader ContentStore,
	repoID string,
	name string,
) ([]EntityContent, error) {
	if reader == nil {
		return nil, nil
	}
	repoID = strings.TrimSpace(repoID)
	name = strings.TrimSpace(name)
	if repoID == "" || name == "" {
		return nil, nil
	}

	matches, err := reader.SearchEntitiesByName(ctx, repoID, "", name, graphEntityResolutionLimit)
	if err != nil {
		return nil, fmt.Errorf("resolve graph entity %q in repo %q: %w", name, repoID, err)
	}
	return exactEntityNameMatches(matches, name), nil
}

func selectExactGraphEntityCandidate(repoID string, name string, exact []EntityContent) (*EntityContent, error) {
	switch len(exact) {
	case 0:
		return nil, nil
	case 1:
		candidate := exact[0]
		return &candidate, nil
	}

	nonTest := nonTestEntityMatches(exact)
	if len(nonTest) == 1 {
		candidate := nonTest[0]
		return &candidate, nil
	}

	return nil, fmt.Errorf(
		"entity name %q in repository %q matched multiple entities: %s",
		name,
		repoID,
		formatAmbiguousEntityMatches(exact),
	)
}

func exactEntityNameMatches(matches []EntityContent, name string) []EntityContent {
	filtered := make([]EntityContent, 0, len(matches))
	for _, match := range matches {
		if strings.TrimSpace(match.EntityName) != name {
			continue
		}
		filtered = append(filtered, match)
	}
	return filtered
}

func nonTestEntityMatches(matches []EntityContent) []EntityContent {
	filtered := make([]EntityContent, 0, len(matches))
	for _, match := range matches {
		if isTestEntityPath(match.RelativePath) {
			continue
		}
		filtered = append(filtered, match)
	}
	return filtered
}

func isTestEntityPath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	return strings.HasSuffix(path, "_test.go")
}

func formatAmbiguousEntityMatches(matches []EntityContent) string {
	items := make([]string, 0, len(matches))
	for _, match := range matches {
		location := strings.TrimSpace(match.RelativePath)
		if location == "" {
			location = "<unknown>"
		}
		items = append(items, fmt.Sprintf("%s (%s:%d)", match.EntityID, location, match.StartLine))
	}
	slices.Sort(items)
	return strings.Join(items, ", ")
}
