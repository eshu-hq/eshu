package query

import (
	"sort"
	"strings"
)

const indirectEvidenceHostnameLimit = 4

var genericServiceHostnameTokens = map[string]struct{}{
	"api":     {},
	"app":     {},
	"apps":    {},
	"dev":     {},
	"http":    {},
	"https":   {},
	"node":    {},
	"prod":    {},
	"qa":      {},
	"server":  {},
	"service": {},
	"stage":   {},
	"staging": {},
	"svc":     {},
	"test":    {},
	"web":     {},
}

// boundedIndirectEvidenceHostnamesForService chooses the hostnames most likely
// to identify the service itself before spending cross-repo content searches.
// When no hostname matches a distinctive service token, it preserves the older
// first-four fallback so services with vanity or opaque domains still get
// bounded consumer evidence.
func boundedIndirectEvidenceHostnamesForService(hostnames []string, serviceName string) []string {
	unique := uniqueTrimmedHostnames(hostnames)
	if len(unique) == 0 {
		return nil
	}

	tokens := serviceHostnameAffinityTokens(serviceName)
	if len(tokens) > 0 {
		affine := make([]string, 0, len(unique))
		for _, hostname := range unique {
			if hostnameMatchesServiceToken(hostname, tokens) {
				affine = append(affine, hostname)
			}
		}
		if len(affine) > 0 {
			return capAndSortIndirectEvidenceHostnames(affine)
		}
	}

	return capAndSortIndirectEvidenceHostnames(unique)
}

func uniqueTrimmedHostnames(hostnames []string) []string {
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		hostname = strings.TrimSpace(hostname)
		if hostname == "" {
			continue
		}
		if _, ok := seen[hostname]; ok {
			continue
		}
		seen[hostname] = struct{}{}
		unique = append(unique, hostname)
	}
	return unique
}

func serviceHostnameAffinityTokens(serviceName string) []string {
	rawTokens := strings.FieldsFunc(strings.ToLower(serviceName), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	tokens := make([]string, 0, len(rawTokens))
	for _, token := range rawTokens {
		if len(token) < 4 {
			continue
		}
		if _, generic := genericServiceHostnameTokens[token]; generic {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func hostnameMatchesServiceToken(hostname string, tokens []string) bool {
	hostname = strings.ToLower(hostname)
	for _, token := range tokens {
		if strings.Contains(hostname, token) {
			return true
		}
	}
	return false
}

func capAndSortIndirectEvidenceHostnames(hostnames []string) []string {
	if len(hostnames) > indirectEvidenceHostnameLimit {
		hostnames = hostnames[:indirectEvidenceHostnameLimit]
	}
	result := append([]string(nil), hostnames...)
	sort.Strings(result)
	return result
}
