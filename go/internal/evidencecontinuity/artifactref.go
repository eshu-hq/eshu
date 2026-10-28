// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencecontinuity

import (
	"strings"
)

// Artifact-reference proof helpers.
//
// This file holds the proof-ref classifiers shared by the contract
// validators in contract.go: whether a ref is proof-like, whether an
// "artifact:" ref names a resolvable run output, and how to parse the
// exact-test form of "go test -run" refs.

func proofLike(ref string) bool {
	ref = strings.TrimSpace(ref)
	lower := strings.ToLower(ref)
	if strings.HasPrefix(ref, "go test ") {
		_, tests, ok := parseGoTestRef(ref)
		return ok && len(tests) > 0
	}
	// A workflow run that recorded the evidence satisfies proof without a
	// committed record (#9315): the ref must be a resolvable run pointer --
	// a numeric run-id/artifact-name or a full run URL, the doc-citations
	// bar (scripts/lib/doc-citation-artifacts.sh). A bare slash is not a
	// namespace, so a slash-bearing non-pointer such as "see/above" is not
	// proof. The committed test/script/golden paths below stay accepted
	// unchanged.
	if rest, ok := strings.CutPrefix(ref, "artifact:"); ok {
		return artifactRefWellformed(rest)
	}
	return strings.HasPrefix(ref, "scripts/") ||
		strings.HasPrefix(ref, "bash scripts/") ||
		strings.Contains(lower, "golden")
}

// artifactURLWellformed requires a resolvable run URL: a registry host
// (a dot in the host) plus /actions/runs/ or a numeric run segment, per
// docs/internal/measurement-ledger.md. Rejects bare hosts such as http://x.
func artifactURLWellformed(url string) bool {
	var hostpath string
	if rest, ok := strings.CutPrefix(url, "http://"); ok {
		hostpath = rest
	} else if rest, ok := strings.CutPrefix(url, "https://"); ok {
		hostpath = rest
	} else {
		return false
	}
	host, _, _ := strings.Cut(hostpath, "/")
	if !strings.Contains(host, ".") {
		return false
	}
	if strings.Contains(url, "/actions/runs/") {
		return true
	}
	if _, path, ok := strings.Cut(hostpath, "/"); ok {
		p := "/" + path
		for i := 0; i+1 < len(p); i++ {
			if p[i] == '/' && p[i+1] >= '0' && p[i+1] <= '9' {
				return true
			}
		}
	}
	return false
}

// artifactRefWellformed reports whether the ref after "artifact:" names a
// resolvable run output: a numeric run id plus an artifact name, or a full
// http(s) run URL -- the doc-citations bar
// (scripts/lib/doc-citation-artifacts.sh). Anything else, including a
// slash-bearing non-pointer such as "see/above" or a bare http(s) host
// with no run segment such as "http://x", is not a pointer.
func artifactRefWellformed(rest string) bool {
	rest = strings.TrimSpace(rest)
	if rest == "http://" || rest == "https://" {
		return false
	}
	if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
		return artifactURLWellformed(rest)
	}
	run, name, ok := strings.Cut(rest, "/")
	if !ok || name == "" {
		return false
	}
	for _, r := range run {
		if r < '0' || r > '9' {
			return false
		}
	}
	return run != ""
}

func parseGoTestRef(ref string) ([]string, []string, bool) {
	tokens := strings.Fields(ref)
	if len(tokens) < 4 || tokens[0] != "go" || tokens[1] != "test" {
		return nil, nil, false
	}
	var packages []string
	var run string
	for i := 2; i < len(tokens); i++ {
		token := strings.Trim(tokens[i], `"'`)
		switch {
		case token == "-run":
			i++
			if i >= len(tokens) {
				return nil, nil, false
			}
			run = strings.Trim(tokens[i], `"'`)
		case strings.HasPrefix(token, "-run="):
			run = strings.Trim(strings.TrimPrefix(token, "-run="), `"'`)
		case strings.HasPrefix(token, "-"):
			continue
		default:
			packages = append(packages, token)
		}
	}
	tests, ok := exactRunTests(run)
	if len(packages) == 0 || !ok {
		return nil, nil, false
	}
	return packages, tests, true
}

func exactRunTests(pattern string) ([]string, bool) {
	pattern = strings.Trim(strings.TrimSpace(pattern), `"'`)
	if pattern == "" {
		return nil, false
	}
	if strings.HasPrefix(pattern, "^(") && strings.HasSuffix(pattern, ")$") {
		pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "^("), ")$")
	} else if strings.HasPrefix(pattern, "^") && strings.HasSuffix(pattern, "$") {
		pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "^"), "$")
	} else {
		return nil, false
	}
	parts := strings.Split(pattern, "|")
	tests := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !isExactTestName(part) {
			return nil, false
		}
		tests = append(tests, part)
	}
	return tests, len(tests) > 0
}

func isExactTestName(value string) bool {
	if !strings.HasPrefix(value, "Test") || len(value) == len("Test") {
		return false
	}
	for _, r := range value {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			continue
		}
		return false
	}
	return true
}
