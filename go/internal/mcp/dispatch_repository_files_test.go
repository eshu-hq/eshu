// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestRepositoryFilesRouteForwardsRepoID(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_repository_files", map[string]any{
		"repo_id": "repo-payments",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_repository_files) error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/repositories/repo-payments/tree"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestRepositoryFilesRouteForwardsLanguage(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_repository_files", map[string]any{
		"repo_id":  "repo-payments",
		"language": "go",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_repository_files) error = %v, want nil", err)
	}
	if got, want := route.query["language"], "go"; got != want {
		t.Fatalf("route.query[language] = %q, want %q", got, want)
	}
}

func TestRepositoryFilesRouteOmitsLanguageWhenEmpty(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_repository_files", map[string]any{
		"repo_id": "repo-payments",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_repository_files) error = %v, want nil", err)
	}
	if got, ok := route.query["language"]; ok {
		t.Fatalf("route.query[language] = %q, want absent when no filter provided", got)
	}
}

func TestRepositoryFilesRouteForwardsPath(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_repository_files", map[string]any{
		"repo_id": "repo-payments",
		"path":    "internal/auth",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_repository_files) error = %v, want nil", err)
	}
	if got, want := route.query["path"], "internal/auth"; got != want {
		t.Fatalf("route.query[path] = %q, want %q", got, want)
	}
}

func TestRepositoryFilesRouteForwardsRecursive(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_repository_files", map[string]any{
		"repo_id":   "repo-payments",
		"recursive": true,
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_repository_files) error = %v, want nil", err)
	}
	if got, want := route.query["recursive"], "true"; got != want {
		t.Fatalf("route.query[recursive] = %q, want %q", got, want)
	}
}

func TestRepositoryFilesRouteOmitsRecursiveWhenFalse(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_repository_files", map[string]any{
		"repo_id": "repo-payments",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_repository_files) error = %v, want nil", err)
	}
	if got, ok := route.query["recursive"]; ok {
		t.Fatalf("route.query[recursive] = %q, want absent when false", got)
	}
}

func TestRepositoryFilesRouteRejectsMissingRepoID(t *testing.T) {
	t.Parallel()

	_, err := resolveRoute("list_repository_files", map[string]any{})
	if err == nil {
		t.Fatal("resolveRoute(list_repository_files) error = nil, want error for missing repo_id")
	}
}

func TestRepositoryFilesRouteForwardsRef(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_repository_files", map[string]any{
		"repo_id": "repo-payments",
		"ref":     "abc1234",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_repository_files) error = %v, want nil", err)
	}
	if got, want := route.query["ref"], "abc1234"; got != want {
		t.Fatalf("route.query[ref] = %q, want %q", got, want)
	}
}

func TestRepositoryFilesRouteEscapesRepoID(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_repository_files", map[string]any{
		"repo_id": "org/repo-name",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_repository_files) error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/repositories/org%2Frepo-name/tree"; got != want {
		t.Fatalf("route.path = %q, want %q (path-escaped repo_id)", got, want)
	}
}
