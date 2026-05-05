package contentrefs

import (
	"regexp"
	"sort"
	"strings"
)

var (
	hostnamePattern       = regexp.MustCompile(`(?i)\b(?:https?://)?((?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{1,62})\b`)
	hostnameKeyPattern    = regexp.MustCompile(`(?i)(?:^|[\s\[{,])["']?(?:host|hostname|url|origin|endpoint|ingress|server_name|base_url|baseurl|public_url|publicurl|service_url|serviceurl|api_url|apiurl)["']?\s*:`)
	hostnameEnvKeyPattern = regexp.MustCompile(`(?i)\b(?:host|hostname|url|origin|endpoint|base_url|public_url|service_url|api_url|ingress)\b\s*=`)
	camelCaseRE           = regexp.MustCompile(`[a-z][A-Z]`)
)

// Hostnames returns normalized hostnames that look like runtime or API
// endpoints rather than code property chains or static file names.
func Hostnames(content string) []string {
	seen := map[string]struct{}{}
	hostnames := make([]string, 0)
	for _, line := range strings.Split(content, "\n") {
		if !lineLikelyContainsHostname(line) {
			continue
		}
		matches := hostnamePattern.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			hostname := strings.ToLower(strings.TrimSpace(match[1]))
			if hostname == "" {
				continue
			}
			if isLikelyFalsePositiveHostname(hostname) {
				continue
			}
			if _, ok := seen[hostname]; ok {
				continue
			}
			seen[hostname] = struct{}{}
			hostnames = append(hostnames, hostname)
		}
	}
	sort.Strings(hostnames)
	return hostnames
}

// isLikelyFalsePositiveHostname rejects regex matches that look like file
// names, code property chains, or test matchers rather than real hostnames.
func isLikelyFalsePositiveHostname(hostname string) bool {
	lastDot := strings.LastIndex(hostname, ".")
	if lastDot < 0 {
		return true
	}
	tld := hostname[lastDot+1:]
	if _, blocked := falsePositiveTLDs[tld]; blocked {
		return true
	}

	for _, keyword := range codeCompoundKeywords {
		if strings.Contains(tld, keyword) {
			return true
		}
	}

	for _, segment := range strings.Split(hostname, ".") {
		if containsCamelCase(segment) {
			return true
		}
	}

	for _, segment := range strings.Split(hostname, ".") {
		if _, blocked := falsePositiveSegments[segment]; blocked {
			return true
		}
	}

	parts := strings.Split(hostname, ".")
	for _, part := range parts {
		if len(part) == 0 {
			return true
		}
	}
	return len(parts[0]) <= 1 && len(parts) <= 2
}

var codeCompoundKeywords = []string{
	"url", "uri", "prefix", "suffix", "path", "type",
	"config", "handler", "helper", "builder", "generator",
	"factory", "controller", "middleware",
}

var falsePositiveSegments = map[string]struct{}{
	"exports": {}, "module": {}, "internals": {}, "require": {},
	"prototype": {}, "constructor": {}, "this": {},
}

var falsePositiveTLDs = map[string]struct{}{
	"jpg": {}, "jpeg": {}, "png": {}, "gif": {}, "svg": {}, "ico": {},
	"webp": {}, "bmp": {}, "zip": {}, "tar": {}, "gz": {}, "pdf": {},
	"doc": {}, "docx": {}, "xls": {}, "xlsx": {}, "css": {}, "js": {},
	"ts": {}, "mjs": {}, "cjs": {}, "json": {}, "yaml": {}, "yml": {},
	"xml": {}, "html": {}, "htm": {}, "txt": {}, "log": {}, "md": {},
	"csv": {}, "sql": {}, "proto": {}, "lock": {}, "toml": {},
	"debug": {}, "info": {}, "error": {}, "warn": {}, "value": {},
	"url": {}, "includes": {}, "replace": {}, "register": {},
	"tostring": {}, "exports": {}, "equal": {}, "client": {},
	"stub": {}, "spark": {}, "img": {}, "type": {},
	"plugin": {}, "length": {}, "push": {}, "map": {},
	"filter": {}, "reduce": {}, "keys": {}, "values": {},
	"then": {}, "catch": {}, "resolve": {}, "reject": {},
	"endpoint": {}, "env": {}, "host": {}, "hostname": {},
}

func containsCamelCase(s string) bool {
	return camelCaseRE.MatchString(s)
}

func lineLikelyContainsHostname(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if strings.Contains(strings.ToLower(trimmed), "://") {
		return true
	}
	return hostnameKeyPattern.MatchString(trimmed) || hostnameEnvKeyPattern.MatchString(trimmed)
}
