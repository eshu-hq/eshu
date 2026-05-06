package contentrefs

import (
	"regexp"
	"sort"
	"strings"
)

var serviceNamePattern = regexp.MustCompile(`\b[a-z0-9]+(?:-[a-z0-9]+){1,}\b`)

// ServiceNames returns normalized lower-case service-like names that are useful
// as cross-repository references without indexing every plain word in content.
func ServiceNames(content string) []string {
	seen := map[string]struct{}{}
	serviceNames := make([]string, 0)
	for _, line := range strings.Split(content, "\n") {
		if !lineLikelyContainsServiceName(line) {
			continue
		}
		for _, match := range serviceNamePattern.FindAllString(strings.ToLower(line), -1) {
			candidate := strings.TrimSpace(match)
			if candidate == "" || isLikelyFalsePositiveServiceName(candidate) {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			serviceNames = append(serviceNames, candidate)
		}
	}
	sort.Strings(serviceNames)
	return serviceNames
}

func isLikelyFalsePositiveServiceName(candidate string) bool {
	if len(candidate) < 5 || len(candidate) > 100 {
		return true
	}
	if strings.Contains(candidate, "--") {
		return true
	}
	parts := strings.Split(candidate, "-")
	if len(parts) < 3 {
		return true
	}
	for _, part := range parts {
		if len(part) == 0 {
			return true
		}
	}
	_, blocked := falsePositiveServiceNames[candidate]
	return blocked
}

func lineLikelyContainsServiceName(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	for _, keyword := range serviceNameLineKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

var serviceNameLineKeywords = []string{
	"app:",
	"application",
	"argocd",
	"base_url",
	"baseurl",
	"chart",
	"depends",
	"deploy",
	"docker",
	"endpoint",
	"helm",
	"host:",
	"hostname",
	"image:",
	"ingress",
	"kustomize",
	"module",
	"name:",
	"public_url",
	"publicurl",
	"repo",
	"service",
	"source",
	"terraform",
	"url:",
	"workflow",
}

var falsePositiveServiceNames = map[string]struct{}{
	"content-type":       {},
	"max-old-space-size": {},
	"pull-requests":      {},
}
