// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func (r TerraformStateBackendFactReader) filteredTerraformStateCandidates(
	ctx context.Context,
	filters []terraformstate.DiscoveryBackendFilter,
	seen map[string]struct{},
) ([]terraformstate.DiscoveryCandidate, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	if seen == nil {
		seen = map[string]struct{}{}
	}
	filterJSON, err := terraformStateBackendFiltersJSON(filters)
	if err != nil {
		return nil, err
	}

	var candidates []terraformstate.DiscoveryCandidate
	rows, err := r.DB.QueryContext(ctx, listTerraformBackendFactsByFilterQuery, filterJSON)
	if err != nil {
		return nil, fmt.Errorf("list filtered terraform state backend facts: %w", err)
	}
	rowCandidates, err := scanTerraformBackendCandidateRows(rows)
	if err != nil {
		return nil, fmt.Errorf("list filtered terraform state backend facts: %w", err)
	}
	candidates = appendMatchingTerraformStateCandidates(candidates, seen, rowCandidates, filters)

	rows, err = r.DB.QueryContext(ctx, listTerragruntRemoteStateFactsByFilterQuery, filterJSON)
	if err != nil {
		return nil, fmt.Errorf("list filtered terragrunt remote_state facts: %w", err)
	}
	rowCandidates, err = scanTerragruntRemoteStateCandidateRows(rows)
	if err != nil {
		return nil, fmt.Errorf("list filtered terragrunt remote_state facts: %w", err)
	}
	candidates = appendMatchingTerraformStateCandidates(candidates, seen, rowCandidates, filters)
	return candidates, nil
}

type terraformStateBackendFilterQueryItem struct {
	BackendKind string `json:"backend_kind"`
	Bucket      string `json:"bucket"`
	Key         string `json:"key"`
	Region      string `json:"region"`
}

func terraformStateBackendFiltersJSON(filters []terraformstate.DiscoveryBackendFilter) (string, error) {
	items := make([]terraformStateBackendFilterQueryItem, 0, len(filters))
	for _, filter := range filters {
		items = append(items, terraformStateBackendFilterQueryItem{
			BackendKind: string(filter.BackendKind),
			Bucket:      filter.Bucket,
			Key:         filter.Key,
			Region:      filter.Region,
		})
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("encode terraform state backend filters: %w", err)
	}
	return string(raw), nil
}

func scanTerraformBackendCandidateRows(rows Rows) ([]terraformstate.DiscoveryCandidate, error) {
	defer func() { _ = rows.Close() }()
	contexts := map[string]terraformBackendFactContext{}
	order := make([]string, 0)
	for rows.Next() {
		repoID, contextValue, scanErr := scanTerraformBackendFactContext(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if repoID == "" {
			continue
		}
		if _, seen := contexts[repoID]; !seen {
			order = append(order, repoID)
		}
		contexts[repoID] = mergeTerraformBackendFactContext(contexts[repoID], contextValue)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var candidates []terraformstate.DiscoveryCandidate
	for _, repoID := range order {
		candidates = append(candidates, terraformBackendCandidatesFromContext(repoID, contexts[repoID])...)
	}
	return candidates, nil
}

func scanTerragruntRemoteStateCandidateRows(rows Rows) ([]terraformstate.DiscoveryCandidate, error) {
	defer func() { _ = rows.Close() }()
	var candidates []terraformstate.DiscoveryCandidate
	for rows.Next() {
		rowCandidates, scanErr := scanTerragruntRemoteStateCandidates(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		candidates = append(candidates, rowCandidates...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func cleanTerraformStateBackendFilters(
	filters []terraformstate.DiscoveryBackendFilter,
) []terraformstate.DiscoveryBackendFilter {
	cleaned := make([]terraformstate.DiscoveryBackendFilter, 0, len(filters))
	seen := map[terraformstate.DiscoveryBackendFilter]struct{}{}
	for _, filter := range filters {
		item := terraformstate.DiscoveryBackendFilter{
			TargetScopeID: strings.TrimSpace(filter.TargetScopeID),
			BackendKind:   terraformstate.BackendKind(strings.ToLower(strings.TrimSpace(string(filter.BackendKind)))),
			Bucket:        strings.TrimSpace(filter.Bucket),
			Key:           strings.Trim(strings.TrimSpace(filter.Key), "/"),
			Region:        strings.ToLower(strings.TrimSpace(filter.Region)),
		}
		if item.TargetScopeID == "" && item.BackendKind == "" && item.Bucket == "" && item.Key == "" && item.Region == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		cleaned = append(cleaned, item)
	}
	return cleaned
}

func appendMatchingTerraformStateCandidates(
	out []terraformstate.DiscoveryCandidate,
	seen map[string]struct{},
	candidates []terraformstate.DiscoveryCandidate,
	filters []terraformstate.DiscoveryBackendFilter,
) []terraformstate.DiscoveryCandidate {
	for _, candidate := range candidates {
		matched, ok := terraformStateCandidateWithFilter(candidate, filters)
		if !ok {
			continue
		}
		key := matched.State.Locator + "\x00" + matched.State.VersionID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, matched)
	}
	return out
}

func appendTerraformStateCandidatesWithFilterEnrichment(
	out []terraformstate.DiscoveryCandidate,
	seen map[string]struct{},
	candidates []terraformstate.DiscoveryCandidate,
	filters []terraformstate.DiscoveryBackendFilter,
) []terraformstate.DiscoveryCandidate {
	for _, candidate := range candidates {
		if matched, ok := terraformStateCandidateWithFilter(candidate, filters); ok {
			candidate = matched
		}
		key := candidate.State.Locator + "\x00" + candidate.State.VersionID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func terraformStateCandidateWithFilter(
	candidate terraformstate.DiscoveryCandidate,
	filters []terraformstate.DiscoveryBackendFilter,
) (terraformstate.DiscoveryCandidate, bool) {
	if len(filters) == 0 {
		return candidate, true
	}
	for _, filter := range filters {
		if !terraformStateFilterMatchesCandidate(filter, candidate) {
			continue
		}
		if filter.TargetScopeID != "" {
			if candidate.TargetScopeID != "" && candidate.TargetScopeID != filter.TargetScopeID {
				continue
			}
			candidate.TargetScopeID = filter.TargetScopeID
		}
		return candidate, true
	}
	return terraformstate.DiscoveryCandidate{}, false
}

func terraformStateFilterMatchesCandidate(
	filter terraformstate.DiscoveryBackendFilter,
	candidate terraformstate.DiscoveryCandidate,
) bool {
	if filter.BackendKind != "" && candidate.State.BackendKind != filter.BackendKind {
		return false
	}
	if filter.Region != "" && strings.TrimSpace(candidate.Region) != filter.Region {
		return false
	}
	if filter.Bucket == "" {
		return terraformStateFilterKeyMatchesCandidate(filter, candidate)
	}
	bucket, _, ok := terraformStateS3LocatorBucketKey(candidate.State.Locator)
	if !ok || bucket != filter.Bucket {
		return false
	}
	return terraformStateFilterKeyMatchesCandidate(filter, candidate)
}

func terraformStateS3LocatorBucketKey(locator string) (string, string, bool) {
	rest, ok := strings.CutPrefix(locator, "s3://")
	if !ok {
		return "", "", false
	}
	bucket, key, ok := strings.Cut(rest, "/")
	if !ok || bucket == "" || key == "" {
		return "", "", false
	}
	return bucket, key, true
}

func terraformStateFilterKeyMatchesCandidate(
	filter terraformstate.DiscoveryBackendFilter,
	candidate terraformstate.DiscoveryCandidate,
) bool {
	if filter.Key == "" {
		return true
	}
	_, key, ok := terraformStateS3LocatorBucketKey(candidate.State.Locator)
	return ok && key == filter.Key
}
