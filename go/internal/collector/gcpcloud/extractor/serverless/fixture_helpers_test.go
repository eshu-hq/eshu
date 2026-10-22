// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serverless_test

import (
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/extractor"
)

// serverlessTestBoundary returns the claim boundary the serverless offline
// fixture tests run generation under. It mirrors the parent gcpcloud
// attributesTestBoundary values so the moved end-to-end fixture tests keep the
// same scope-generation identity without importing parent test files.
func serverlessTestBoundary() extractor.Boundary {
	return extractor.Boundary{
		CollectorInstanceID: "collector-1",
		ParentScopeKind:     catalog.ParentScopeProject,
		ParentScopeID:       "demo-project",
		AssetTypeFamily:     "bigquery",
		ContentFamily:       "resource",
		LocationBucket:      "us",
		ScopeID:             "gcp:project:demo-project:bigquery:resource:us",
		GenerationID:        "gen-1",
		FencingToken:        1,
	}
}

func stringOrEmpty(v any) string {
	s, _ := v.(string)
	return s
}

func anyStringSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	if s, ok := v.([]string); ok {
		out := make([]any, 0, len(s))
		for _, item := range s {
			out = append(out, item)
		}
		return out
	}
	return nil
}

func containsString(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
