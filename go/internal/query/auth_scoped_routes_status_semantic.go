// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

func scopedFactSchemaVersionRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/api/v0/fact-schema-versions" {
		return true
	}
	const prefix = "/api/v0/fact-schema-versions/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	factKind := strings.TrimPrefix(r.URL.Path, prefix)
	return factKind != "" && !strings.Contains(factKind, "/")
}

// scopedSemanticSearchRoute reports whether the request targets the curated
// semantic-search route. The handler requires repo_id, checks scoped-token
// grants before the search-document store read, and computes result limits and
// truncation from only the authorized repository corpus.
func scopedSemanticSearchRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/search/semantic"
}

func scopedHostedGovernanceStatusRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/governance"
}

func scopedHostedReadinessRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/hosted-readiness"
}

func scopedSemanticExtractionStatusRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/semantic-extraction"
}

func scopedAnswerNarrationStatusRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/answer-narration"
}

func scopedSemanticEvidenceRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/semantic/documentation-observations",
		"/api/v0/semantic/code-hints":
		return true
	default:
		return false
	}
}

func scopedComponentExtensionRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/api/v0/component-extensions" {
		return true
	}
	const (
		prefix = "/api/v0/component-extensions/"
		suffix = "/diagnostics"
	)
	if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, suffix) {
		return false
	}
	componentID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), suffix)
	return componentID != "" && !strings.Contains(componentID, "/")
}

func scopedContextRoute(path string, prefix string) bool {
	for _, suffix := range []string{"/context", "/story"} {
		if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
			continue
		}
		selector := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
		return selector != "" && !strings.Contains(selector, "/")
	}
	return false
}
