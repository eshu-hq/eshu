package terraformstate

import "strings"

func candidateWithBackendFilters(
	candidate DiscoveryCandidate,
	filters []DiscoveryBackendFilter,
) (DiscoveryCandidate, bool) {
	if len(filters) == 0 {
		return candidate, true
	}
	for _, filter := range filters {
		if !backendFilterMatchesCandidate(filter, candidate) {
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
	return DiscoveryCandidate{}, false
}

func backendFilterMatchesCandidate(filter DiscoveryBackendFilter, candidate DiscoveryCandidate) bool {
	if filter.BackendKind != "" && candidate.State.BackendKind != filter.BackendKind {
		return false
	}
	if filter.Region != "" && strings.TrimSpace(candidate.Region) != filter.Region {
		return false
	}
	if filter.Bucket == "" {
		return true
	}
	bucket, ok := s3LocatorBucket(candidate.State.Locator)
	return ok && bucket == filter.Bucket
}

func s3LocatorBucket(locator string) (string, bool) {
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

func normalizedBackendFilters(filters []DiscoveryBackendFilter) []DiscoveryBackendFilter {
	normalized := make([]DiscoveryBackendFilter, 0, len(filters))
	seen := map[DiscoveryBackendFilter]struct{}{}
	for _, filter := range filters {
		item := DiscoveryBackendFilter{
			TargetScopeID: strings.TrimSpace(filter.TargetScopeID),
			BackendKind:   BackendKind(strings.ToLower(strings.TrimSpace(string(filter.BackendKind)))),
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
		normalized = append(normalized, item)
	}
	return normalized
}
