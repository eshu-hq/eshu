// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Exact graph-entity resolution shared by the query root and the
// handler-family subpackages (#6060 lane B2).
//
// These resolve an entity by exact name within one repository, rejecting
// ambiguous names. They live here rather than in package query because the
// impact family's exposure-path handler resolves source entities and cannot
// import the root package back without an import cycle. The grant check
// composes the repository access filter from the context directly. Package
// query keeps forwarding wrappers under the original names.

// GraphEntityResolutionLimit bounds exact-name entity resolution reads.
const GraphEntityResolutionLimit = 50

// ResolveExactGraphEntityCandidate resolves one entity by exact name,
// rejecting ambiguous names.
func ResolveExactGraphEntityCandidate(
	ctx context.Context,
	reader ContentStore,
	repoID string,
	name string,
) (*EntityContent, error) {
	exact, err := ResolveExactGraphEntityCandidates(ctx, reader, repoID, name)
	if err != nil {
		return nil, err
	}
	return SelectExactGraphEntityCandidate(repoID, name, exact)
}

// ResolveExactGraphEntityCandidates lists exact-name entity candidates in
// one repository.
func ResolveExactGraphEntityCandidates(
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
	// Defense in depth. Every caller reaches this through a repository selector
	// that already resolved repoID against the grant, so a mismatch here means
	// the read was reached on a selector-free path; the candidate rows become an
	// ambiguity error that names entity ids, so it must not be that path's job
	// alone to keep them in grant.
	if !RepositoryAccessFilterFromContext(ctx).WithCanonicalScopeRepositories().AllowsRepositoryID(repoID) {
		return nil, nil
	}

	matches, err := reader.SearchEntitiesByName(ctx, repoID, "", name, GraphEntityResolutionLimit)
	if err != nil {
		return nil, fmt.Errorf("resolve graph entity %q in repo %q: %w", name, repoID, err)
	}
	return ExactEntityNameMatches(matches, name), nil
}

// SelectExactGraphEntityCandidate picks the single candidate, preferring a
// lone non-test match and rejecting ambiguity with an error.
func SelectExactGraphEntityCandidate(repoID string, name string, exact []EntityContent) (*EntityContent, error) {
	switch len(exact) {
	case 0:
		return nil, nil
	case 1:
		candidate := exact[0]
		return &candidate, nil
	}

	nonTest := NonTestEntityMatches(exact)
	if len(nonTest) == 1 {
		candidate := nonTest[0]
		return &candidate, nil
	}

	return nil, fmt.Errorf(
		"entity name %q in repository %q matched multiple entities: %s",
		name,
		repoID,
		FormatAmbiguousEntityMatches(exact),
	)
}

// ExactEntityNameMatches keeps only whitespace-exact name matches.
func ExactEntityNameMatches(matches []EntityContent, name string) []EntityContent {
	filtered := make([]EntityContent, 0, len(matches))
	for _, match := range matches {
		if strings.TrimSpace(match.EntityName) != name {
			continue
		}
		filtered = append(filtered, match)
	}
	return filtered
}

// NonTestEntityMatches drops matches from test files.
func NonTestEntityMatches(matches []EntityContent) []EntityContent {
	filtered := make([]EntityContent, 0, len(matches))
	for _, match := range matches {
		if IsTestEntityPath(match.RelativePath) {
			continue
		}
		filtered = append(filtered, match)
	}
	return filtered
}

// IsTestEntityPath reports whether path is a Go test file.
func IsTestEntityPath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	return strings.HasSuffix(path, "_test.go")
}

// FormatAmbiguousEntityMatches renders candidates for an ambiguity error.
func FormatAmbiguousEntityMatches(matches []EntityContent) string {
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
