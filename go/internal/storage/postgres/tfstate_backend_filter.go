package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func (r TerraformStateBackendFactReader) filteredTerraformStateCandidates(
	ctx context.Context,
	query terraformstate.DiscoveryQuery,
	filters []terraformstate.DiscoveryBackendFilter,
) ([]terraformstate.DiscoveryCandidate, error) {
	seen := map[string]struct{}{}
	var candidates []terraformstate.DiscoveryCandidate
	for _, filter := range filters {
		rows, err := r.DB.QueryContext(
			ctx,
			listTerraformBackendFactsByFilterQuery,
			string(filter.BackendKind),
			filter.Bucket,
			filter.Region,
		)
		if err != nil {
			return nil, fmt.Errorf("list filtered terraform state backend facts: %w", err)
		}
		rowCandidates, err := scanTerraformBackendCandidateRows(rows)
		if err != nil {
			return nil, fmt.Errorf("list filtered terraform state backend facts: %w", err)
		}
		candidates = appendMatchingTerraformStateCandidates(candidates, seen, rowCandidates, []terraformstate.DiscoveryBackendFilter{filter})

		rows, err = r.DB.QueryContext(
			ctx,
			listTerragruntRemoteStateFactsByFilterQuery,
			string(filter.BackendKind),
			filter.Bucket,
			filter.Region,
		)
		if err != nil {
			return nil, fmt.Errorf("list filtered terragrunt remote_state facts: %w", err)
		}
		rowCandidates, err = scanTerragruntRemoteStateCandidateRows(rows)
		if err != nil {
			return nil, fmt.Errorf("list filtered terragrunt remote_state facts: %w", err)
		}
		candidates = appendMatchingTerraformStateCandidates(candidates, seen, rowCandidates, []terraformstate.DiscoveryBackendFilter{filter})
	}
	localCandidates, err := r.localStateCandidates(ctx, query, seen, filters)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, localCandidates...)
	return candidates, nil
}

func scanTerraformBackendCandidateRows(rows Rows) ([]terraformstate.DiscoveryCandidate, error) {
	defer func() { _ = rows.Close() }()
	var candidates []terraformstate.DiscoveryCandidate
	for rows.Next() {
		rowCandidates, scanErr := scanTerraformBackendFactCandidates(rows)
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
			Region:        strings.ToLower(strings.TrimSpace(filter.Region)),
		}
		if item.TargetScopeID == "" && item.BackendKind == "" && item.Bucket == "" && item.Region == "" {
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
		return true
	}
	bucket, ok := terraformStateS3LocatorBucket(candidate.State.Locator)
	return ok && bucket == filter.Bucket
}

func terraformStateS3LocatorBucket(locator string) (string, bool) {
	rest, ok := strings.CutPrefix(locator, "s3://")
	if !ok {
		return "", false
	}
	bucket, _, ok := strings.Cut(rest, "/")
	if !ok || bucket == "" {
		return "", false
	}
	return bucket, true
}
