// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// MustMapField returns parent[key] as a map[string]any, failing the test when
// the key is absent or holds a different type. It is used to walk decoded
// OpenAPI and JSON envelope documents one level at a time, so that a failure
// names the exact key that broke rather than surfacing a nil-map panic several
// frames later.
func MustMapField(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := parent[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	typed, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("key %q type = %T, want map[string]any", key, value)
	}
	return typed
}

// RequireStringAnySlice returns parent[key] as a []any, failing the test
// when the key is absent or holds a different type. It moved here for #6060
// lane B B3 because the repository authz tests moved to the repository family
// package while the stats-limits tests stay in root; a _test.go declaration
// in either package is unreachable from the other.
func RequireStringAnySlice(t *testing.T, parent map[string]any, key string) []any {
	t.Helper()

	value, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("%s type = %T, want []any", key, parent[key])
	}
	return value
}

// AnySliceContains reports whether any string element of values equals want.
// Non-string elements never match. It moved here for #6060 lane B B3 because
// the repository authz tests moved to the repository family package while the
// stats-limits tests stay in root; a _test.go declaration in either package
// is unreachable from the other.
func AnySliceContains(values []any, want string) bool {
	for _, value := range values {
		if s, ok := value.(string); ok && s == want {
			return true
		}
	}
	return false
}

// StringSliceEqual reports whether got is a []string equal to want,
// element-wise and in order. A non-[]string got never matches. It moved here
// for #6060 lane B B3 because the artifacts tests moved to the
// repositoryartifacts family package while the correlation-DSL fixture test
// stays in root.
func StringSliceEqual(got any, want []string) bool {
	typed, ok := got.([]string)
	if !ok {
		return false
	}
	if len(typed) != len(want) {
		return false
	}
	for i := range want {
		if typed[i] != want[i] {
			return false
		}
	}
	return true
}

// RepositoryStatsCatalogEntry is the shared order-service catalog fixture
// coverage, envelope, branch, tree, and stats tests build content-store rows
// from. It moved here for #6060 lane B B3 because the story-coverage tests
// moved to the repository family package while eleven sibling test files stay
// in root; a _test.go declaration in either package is unreachable from the
// other.
func RepositoryStatsCatalogEntry() querycontract.RepositoryCatalogEntry {
	return querycontract.RepositoryCatalogEntry{
		ID:        "repo-1",
		Name:      "order-service",
		Path:      "/repos/order-service",
		LocalPath: "/repos/order-service",
		RemoteURL: "https://github.com/org/order-service",
		RepoSlug:  "org/order-service",
		HasRemote: true,
	}
}

// RepositoryStatsGraphRow is the shared order-service graph row fixture
// stats-adjacent tests feed graph doubles. It moved here for #6060 lane B B3
// alongside RepositoryStatsCatalogEntry: the story-coverage tests moved to the
// repository family package while six sibling test files stay in root.
func RepositoryStatsGraphRow() map[string]any {
	return map[string]any{
		"id":         "repo-1",
		"name":       "order-service",
		"path":       "/repos/order-service",
		"local_path": "/repos/order-service",
		"remote_url": "https://github.com/org/order-service",
		"repo_slug":  "org/order-service",
		"has_remote": true,
	}
}

// RequireStringSlice asserts parent[key] is a []any of strings equal to want,
// element-wise and in order. It moved here for #6060 lane B B3 alongside
// RequireStringAnySlice: the story-coverage tests moved to the repository
// family package while the stats tests stay in root.
func RequireStringSlice(t *testing.T, parent map[string]any, key string, want []string) {
	t.Helper()

	values, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("%s type = %T, want []any", key, parent[key])
	}
	got := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("%s item type = %T, want string", key, value)
		}
		got = append(got, text)
	}
	if len(got) != len(want) {
		t.Fatalf("%s = %#v, want %#v", key, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
}

// SegmentByName returns the first segment whose "name" decodes to name, or
// nil. It moved here for #6060 lane B B4 because the service trace-path
// tests moved to the service family package while the ci_cd story parity
// test stays in root; a _test.go declaration in either package is
// unreachable from the other.
func SegmentByName(segments []map[string]any, name string) map[string]any {
	for _, segment := range segments {
		if querycontract.StringVal(segment, "name") == name {
			return segment
		}
	}
	return nil
}
