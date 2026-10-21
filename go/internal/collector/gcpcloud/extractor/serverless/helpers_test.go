// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serverless

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/extractor"
)

// assertRelationship fails the test unless rels holds a relationship of the
// given type pointing at the given target with the given target asset type.
// It mirrors the same-named helper in the parent gcpcloud package so the
// moved extractor unit tests keep their assertion shape without importing
// parent test files.
func assertRelationship(t *testing.T, rels []extractor.RelationshipObservation, relType, target, targetType string) {
	t.Helper()
	for _, rel := range rels {
		if rel.RelationshipType == relType && rel.TargetFullResourceName == target {
			if rel.TargetAssetType != targetType {
				t.Errorf("relationship %q target asset type = %q, want %q", relType, rel.TargetAssetType, targetType)
			}
			return
		}
	}
	t.Errorf("missing relationship type=%q target=%q in %#v", relType, target, rels)
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

func diffAttrs(got, want map[string]any) string {
	if len(got) != len(want) {
		return "length mismatch"
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			return "missing key " + k
		}
		if stringify(gv) != stringify(wv) {
			return "value mismatch at key " + k
		}
	}
	return ""
}

func diffStringSlices(got, want []string) string {
	if len(got) != len(want) {
		return "length mismatch"
	}
	for i := range want {
		if got[i] != want[i] {
			return "value mismatch at index " + stringify(i)
		}
	}
	return ""
}

func stringify(v any) string {
	return fmt.Sprintf("%v", v)
}
