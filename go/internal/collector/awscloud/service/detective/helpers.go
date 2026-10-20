// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package detective

import "strings"

// memberResourceID derives a member-account resource id from the graph ARN and
// the account id. Keying on these two stable values, never on list order, keeps
// the identity constant across scans even when Detective reorders members.
func memberResourceID(graphARN string, accountID string) string {
	graphARN = strings.TrimSpace(graphARN)
	accountID = strings.TrimSpace(accountID)
	if graphARN == "" || accountID == "" {
		return ""
	}
	return graphARN + "/member/" + accountID
}

// graphName derives a stable display name from a behavior graph ARN. The
// Detective graph ARN ends in graph:<id>, so the trailing id is the most
// human-meaningful name; the full ARN is the fallback.
func graphName(graphARN string) string {
	graphARN = strings.TrimSpace(graphARN)
	if graphARN == "" {
		return ""
	}
	if idx := strings.LastIndex(graphARN, "/"); idx >= 0 && idx+1 < len(graphARN) {
		return graphARN[idx+1:]
	}
	return graphARN
}

// graphDatasourcePackages returns the sorted, de-duplicated union of the
// data-source package names enabled across a graph's members. It is an
// aggregate identity signal (for example DETECTIVE_CORE, which ingests
// GuardDuty); no usage volume or finding content is included.
func graphDatasourcePackages(members []MemberAccount) []string {
	seen := make(map[string]struct{})
	for _, member := range members {
		for _, pkg := range member.DatasourcePackages {
			trimmed := strings.TrimSpace(pkg)
			if trimmed == "" {
				continue
			}
			seen[trimmed] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	packages := make([]string, 0, len(seen))
	for pkg := range seen {
		packages = append(packages, pkg)
	}
	sortStrings(packages)
	return packages
}

// graphSourcesGuardDuty reports whether the graph is known to ingest GuardDuty
// data. It is true when a resolver supplied a GuardDuty detector id or when any
// member enables the DETECTIVE_CORE package, which ingests GuardDuty findings.
func graphSourcesGuardDuty(members []MemberAccount, detectorID string) bool {
	if strings.TrimSpace(detectorID) != "" {
		return true
	}
	for _, member := range members {
		for _, pkg := range member.DatasourcePackages {
			if strings.EqualFold(strings.TrimSpace(pkg), datasourcePackageDetectiveCore) {
				return true
			}
		}
	}
	return false
}

// datasourcePackageDetectiveCore is the Detective data-source package that
// ingests GuardDuty findings. It is matched case-insensitively so a future SDK
// casing change does not silently drop the GuardDuty-source signal.
const datasourcePackageDetectiveCore = "DETECTIVE_CORE"

// cloneStringMap copies a tag map, trimming blank keys, so the scanner does not
// alias the caller's map or carry empty keys.
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// cloneStringSlice copies a string slice, trimming blanks, so attribute values
// do not alias the source slice.
func cloneStringSlice(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		output = append(output, trimmed)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// sortStrings sorts a string slice in place with a small insertion sort, keeping
// the package free of an import for one short, deterministic ordering.
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
}
