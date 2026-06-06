package query

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/contentrefs"
)

var environmentAliases = []struct {
	canonical string
	aliases   []string
}{
	{canonical: "prod", aliases: []string{"prod", "production"}},
	{canonical: "qa", aliases: []string{"qa"}},
	{canonical: "stage", aliases: []string{"stage", "staging"}},
	{canonical: "dev", aliases: []string{"dev", "development"}},
	{canonical: "test", aliases: []string{"test"}},
	{canonical: "sandbox", aliases: []string{"sandbox"}},
	{canonical: "preview", aliases: []string{"preview"}},
}

func extractObservedHostnames(content string) []string {
	return contentrefs.Hostnames(content)
}

func extractObservedHostnameCandidates(content string) []contentrefs.HostnameCandidate {
	return contentrefs.HostnameCandidates(content)
}

func exactObservedHostnameCandidates(candidates []contentrefs.HostnameCandidate) []string {
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Classification != "exact_hostname" {
			continue
		}
		values = append(values, candidate.Value)
	}
	return uniqueSortedStrings(values)
}

func exactHostnameCandidateReason(candidates []contentrefs.HostnameCandidate, hostname string) string {
	for _, candidate := range candidates {
		if candidate.Value == hostname && candidate.Classification == "exact_hostname" {
			return candidate.Reason
		}
	}
	return "content_hostname_reference"
}

func inferObservedEnvironments(relativePath string, content string, hostnames []string) []string {
	seen := map[string]struct{}{}
	addMatches := func(text string) {
		for _, environment := range detectEnvironmentAliases(text) {
			seen[environment] = struct{}{}
		}
	}
	addMatches(relativePath)
	addMatches(content)
	for _, hostname := range hostnames {
		addMatches(hostname)
	}

	environments := make([]string, 0, len(seen))
	for environment := range seen {
		environments = append(environments, environment)
	}
	sort.Strings(environments)
	return environments
}

func detectEnvironmentAliases(text string) []string {
	normalized := normalizeEvidenceToken(text)
	if normalized == "" {
		return nil
	}
	seen := map[string]struct{}{}
	for _, row := range environmentAliases {
		for _, alias := range row.aliases {
			if strings.Contains(normalized, "_"+alias+"_") {
				seen[row.canonical] = struct{}{}
				break
			}
		}
	}
	environments := make([]string, 0, len(seen))
	for environment := range seen {
		environments = append(environments, environment)
	}
	sort.Strings(environments)
	return environments
}

func inferHostnameEnvironment(hostname string) string {
	matches := detectEnvironmentAliases(hostname)
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}
