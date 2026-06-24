// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

const (
	hostnameClassificationExact             = "exact_hostname"
	hostnameClassificationRejectedConfigKey = "rejected_config_key"
	hostnameClassificationRejectedFieldPath = "rejected_field_path"
	hostnameClassificationAmbiguous         = "ambiguous"
)

// HostnameCandidate classifies one dotted candidate found in hostname-like
// content context. Exact candidates can be promoted to hostname lookup rows;
// rejected and ambiguous candidates are supporting evidence only.
type HostnameCandidate struct {
	Value          string
	Classification string
	Reason         string
}

// Hostnames returns normalized hostnames that look like runtime or API
// endpoints rather than code property chains or static file names.
func Hostnames(content string) []string {
	candidates := HostnameCandidates(content)
	seen := map[string]struct{}{}
	hostnames := make([]string, 0)
	for _, candidate := range candidates {
		if candidate.Classification != hostnameClassificationExact {
			continue
		}
		if _, ok := seen[candidate.Value]; ok {
			continue
		}
		seen[candidate.Value] = struct{}{}
		hostnames = append(hostnames, candidate.Value)
	}
	sort.Strings(hostnames)
	return hostnames
}

// HostnameCandidates returns every hostname-shaped token found in hostname-like
// content context, classified as exact, rejected, or ambiguous evidence.
func HostnameCandidates(content string) []HostnameCandidate {
	seen := map[string]struct{}{}
	candidates := make([]HostnameCandidate, 0)
	for _, line := range strings.Split(content, "\n") {
		if !lineLikelyContainsHostname(line) {
			continue
		}
		matches := hostnamePattern.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			rawMatch := strings.TrimSpace(match[0])
			rawHostname := strings.TrimSpace(match[1])
			hostname := strings.ToLower(rawHostname)
			if hostname == "" {
				continue
			}
			candidate := classifyHostnameCandidate(rawMatch, rawHostname, hostname)
			key := candidate.Value + "\x00" + candidate.Classification + "\x00" + candidate.Reason
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Value != candidates[j].Value {
			return candidates[i].Value < candidates[j].Value
		}
		if candidates[i].Classification != candidates[j].Classification {
			return candidates[i].Classification < candidates[j].Classification
		}
		return candidates[i].Reason < candidates[j].Reason
	})
	return candidates
}

func classifyHostnameCandidate(rawMatch, rawHostname, hostname string) HostnameCandidate {
	classification, reason := classifyHostnameEvidence(rawMatch, rawHostname, hostname)
	return HostnameCandidate{
		Value:          hostname,
		Classification: classification,
		Reason:         reason,
	}
}

func classifyHostnameEvidence(rawMatch, rawHostname, hostname string) (string, string) {
	lastDot := strings.LastIndex(hostname, ".")
	if lastDot < 0 {
		return hostnameClassificationRejectedFieldPath, "missing_dot"
	}
	tld := hostname[lastDot+1:]
	if _, blocked := falsePositiveTLDs[tld]; blocked {
		return hostnameClassificationRejectedFieldPath, "code_or_file_extension"
	}

	for _, keyword := range codeCompoundKeywords {
		if strings.Contains(tld, keyword) {
			return hostnameClassificationRejectedFieldPath, "code_identifier_suffix"
		}
	}

	for _, segment := range strings.Split(rawHostname, ".") {
		if containsCamelCase(segment) {
			return hostnameClassificationRejectedFieldPath, "camel_case_field_path"
		}
	}

	for _, segment := range strings.Split(hostname, ".") {
		if _, blocked := falsePositiveSegments[segment]; blocked {
			return hostnameClassificationRejectedFieldPath, "code_property_segment"
		}
	}

	parts := strings.Split(hostname, ".")
	for _, part := range parts {
		if len(part) == 0 {
			return hostnameClassificationRejectedFieldPath, "empty_hostname_label"
		}
	}
	if looksLikeFieldPathHostname(parts) {
		return hostnameClassificationRejectedFieldPath, "dotted_field_path"
	}
	if strings.Contains(strings.ToLower(rawMatch), "://") {
		return hostnameClassificationExact, "url_hostname_reference"
	}
	if looksLikeConfigKeyHostname(parts) {
		return hostnameClassificationRejectedConfigKey, "dotted_config_key"
	}
	if len(parts) == 2 && isExactTwoLabelPublicTLD(tld) {
		return hostnameClassificationExact, "hostname_key_reference"
	}
	if len(parts[0]) <= 1 && len(parts) <= 2 {
		return hostnameClassificationAmbiguous, "short_two_label_hostname_candidate"
	}
	if len(parts) <= 2 {
		return hostnameClassificationAmbiguous, "two_label_hostname_candidate"
	}
	return hostnameClassificationExact, "hostname_key_reference"
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

var falsePositiveFieldPathSegments = map[string]struct{}{
	"attribute": {}, "attributes": {}, "body": {}, "data": {},
	"field": {}, "fields": {}, "fixture": {}, "fixtures": {},
	"item": {}, "items": {}, "metadata": {}, "payload": {},
	"request": {}, "response": {}, "result": {}, "results": {},
}

var falsePositiveConfigKeyTerminals = map[string]struct{}{
	"count": {}, "enabled": {}, "ids": {}, "key": {},
	"level": {}, "limit": {}, "mode": {}, "ms": {},
	"path": {}, "paths": {}, "port": {}, "ports": {}, "prefix": {},
	"retries": {}, "retry": {}, "seconds": {}, "size": {},
	"suffix": {}, "timeout": {}, "ttl": {}, "type": {},
	"types": {}, "value": {}, "values": {}, "version": {},
}

var exactTwoLabelPublicTLDs = map[string]struct{}{
	"id": {}, "name": {},
}

func looksLikeFieldPathHostname(parts []string) bool {
	for _, part := range parts {
		if _, ok := falsePositiveFieldPathSegments[part]; ok {
			return true
		}
	}
	return false
}

func looksLikeConfigKeyHostname(parts []string) bool {
	if len(parts) == 0 {
		return false
	}
	last := parts[len(parts)-1]
	if _, ok := falsePositiveConfigKeyTerminals[last]; ok {
		return true
	}
	return false
}

func isExactTwoLabelPublicTLD(tld string) bool {
	_, ok := exactTwoLabelPublicTLDs[tld]
	return ok
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
