// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import "testing"

func TestIsFluxGitRepository(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		apiVersion string
		kind       string
		want       bool
	}{
		{"flux v1 git repository", "source.toolkit.fluxcd.io/v1", "GitRepository", true},
		{"flux v1beta2 git repository", "source.toolkit.fluxcd.io/v1beta2", "GitRepository", true},
		{"flux group wrong kind", "source.toolkit.fluxcd.io/v1", "OCIRepository", false},
		{"generic group is not flux source", "example.com/v1", "GitRepository", false},
		{"empty apiVersion", "", "GitRepository", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isFluxGitRepository(tc.apiVersion, tc.kind); got != tc.want {
				t.Fatalf("isFluxGitRepository(%q, %q) = %v, want %v", tc.apiVersion, tc.kind, got, tc.want)
			}
		})
	}
}

func TestIsFluxOCIRepository(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		apiVersion string
		kind       string
		want       bool
	}{
		{"flux v1beta2 oci repository", "source.toolkit.fluxcd.io/v1beta2", "OCIRepository", true},
		{"flux group wrong kind", "source.toolkit.fluxcd.io/v1beta2", "GitRepository", false},
		{"generic group is not flux source", "example.com/v1", "OCIRepository", false},
		{"empty apiVersion", "", "OCIRepository", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isFluxOCIRepository(tc.apiVersion, tc.kind); got != tc.want {
				t.Fatalf("isFluxOCIRepository(%q, %q) = %v, want %v", tc.apiVersion, tc.kind, got, tc.want)
			}
		})
	}
}

func TestIsFluxBucket(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		apiVersion string
		kind       string
		want       bool
	}{
		{"flux v1beta2 bucket", "source.toolkit.fluxcd.io/v1beta2", "Bucket", true},
		{"flux group wrong kind", "source.toolkit.fluxcd.io/v1beta2", "GitRepository", false},
		{"generic group is not flux source", "example.com/v1", "Bucket", false},
		{"empty apiVersion", "", "Bucket", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isFluxBucket(tc.apiVersion, tc.kind); got != tc.want {
				t.Fatalf("isFluxBucket(%q, %q) = %v, want %v", tc.apiVersion, tc.kind, got, tc.want)
			}
		})
	}
}

func TestParseFluxGitRepositoryCapturesURLAndRef(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"spec": map[string]any{
			"url": "https://github.com/example/repo",
			"ref": map[string]any{
				"branch": "main",
			},
		},
	}
	metadata := map[string]any{
		"name":      "flux-system",
		"namespace": "flux-system",
	}

	row := parseFluxGitRepository(document, metadata, "/repo/gitrepository.yaml", 5)

	if row["name"] != "flux-system" {
		t.Fatalf("name = %#v, want flux-system", row["name"])
	}
	if row["namespace"] != "flux-system" {
		t.Fatalf("namespace = %#v, want flux-system", row["namespace"])
	}
	if row["url"] != "https://github.com/example/repo" {
		t.Fatalf("url = %#v, want the repo url", row["url"])
	}
	if row["ref_branch"] != "main" {
		t.Fatalf("ref_branch = %#v, want main", row["ref_branch"])
	}
	for _, key := range []string{"ref_tag", "ref_semver", "ref_commit"} {
		if _, present := row[key]; present {
			t.Fatalf("row[%q] = %#v, want absent (not fabricated)", key, row[key])
		}
	}
}

func TestParseFluxGitRepositoryOmitsAbsentFields(t *testing.T) {
	t.Parallel()

	document := map[string]any{"spec": map[string]any{}}
	metadata := map[string]any{"name": "bare"}

	row := parseFluxGitRepository(document, metadata, "/repo/bare.yaml", 1)

	for _, key := range []string{"url", "ref_branch", "ref_tag", "ref_semver", "ref_commit"} {
		if _, present := row[key]; present {
			t.Fatalf("row[%q] = %#v, want absent when spec has no matching field", key, row[key])
		}
	}
	// namespace is injected at apply-time far more often than it is written in
	// the manifest; an absent metadata.namespace must be OMITTED, never
	// fabricated as "<nil>" (fmt.Sprint(nil)) or an empty string.
	if _, present := row["namespace"]; present {
		t.Fatalf("namespace = %#v, want absent when metadata has no namespace (never fabricated)", row["namespace"])
	}
}

func TestParseFluxOCIRepositoryCapturesURLAndRef(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"spec": map[string]any{
			"url": "oci://ghcr.io/example/manifests",
			"ref": map[string]any{
				"tag": "latest",
			},
		},
	}
	metadata := map[string]any{
		"name":      "app-manifests",
		"namespace": "flux-system",
	}

	row := parseFluxOCIRepository(document, metadata, "/repo/ocirepository.yaml", 7)

	if row["url"] != "oci://ghcr.io/example/manifests" {
		t.Fatalf("url = %#v, want the oci url", row["url"])
	}
	if row["ref_tag"] != "latest" {
		t.Fatalf("ref_tag = %#v, want latest", row["ref_tag"])
	}
	for _, key := range []string{"ref_branch", "ref_semver", "ref_commit"} {
		if _, present := row[key]; present {
			t.Fatalf("row[%q] = %#v, want absent (not fabricated)", key, row[key])
		}
	}
}

func TestParseFluxOCIRepositoryOmitsAbsentNamespace(t *testing.T) {
	t.Parallel()

	document := map[string]any{"spec": map[string]any{}}
	metadata := map[string]any{"name": "bare"}

	row := parseFluxOCIRepository(document, metadata, "/repo/bare.yaml", 1)

	if _, present := row["namespace"]; present {
		t.Fatalf("namespace = %#v, want absent when metadata has no namespace (never fabricated)", row["namespace"])
	}
	for _, key := range []string{"url", "ref_branch", "ref_tag", "ref_semver", "ref_commit"} {
		if _, present := row[key]; present {
			t.Fatalf("row[%q] = %#v, want absent when spec has no matching field", key, row[key])
		}
	}
}

func TestParseFluxBucketCapturesBucketFields(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"spec": map[string]any{
			"bucketName": "flux-artifacts",
			"endpoint":   "minio.example.com",
			"provider":   "generic",
		},
	}
	metadata := map[string]any{
		"name":      "artifacts",
		"namespace": "flux-system",
	}

	row := parseFluxBucket(document, metadata, "/repo/bucket.yaml", 9)

	if row["bucket_name"] != "flux-artifacts" {
		t.Fatalf("bucket_name = %#v, want flux-artifacts", row["bucket_name"])
	}
	if row["endpoint"] != "minio.example.com" {
		t.Fatalf("endpoint = %#v, want minio.example.com", row["endpoint"])
	}
	if row["provider"] != "generic" {
		t.Fatalf("provider = %#v, want generic", row["provider"])
	}
}

func TestParseFluxBucketOmitsAbsentFields(t *testing.T) {
	t.Parallel()

	document := map[string]any{"spec": map[string]any{}}
	metadata := map[string]any{"name": "bare"}

	row := parseFluxBucket(document, metadata, "/repo/bare.yaml", 1)

	for _, key := range []string{"bucket_name", "endpoint", "provider"} {
		if _, present := row[key]; present {
			t.Fatalf("row[%q] = %#v, want absent when spec has no matching field", key, row[key])
		}
	}
	if _, present := row["namespace"]; present {
		t.Fatalf("namespace = %#v, want absent when metadata has no namespace (never fabricated)", row["namespace"])
	}
}
