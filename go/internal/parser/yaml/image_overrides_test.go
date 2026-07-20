// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"testing"
)

// TestParseHelmValuesImageOverrides pins the image_overrides row shape a
// Helm values.yaml "image:" block produces (issue #5440): the per-image
// tag/digest version truth that helm_values[].image_repositories
// intentionally discards (collectHelmImageRepositories /
// normalizeContainerImageRepository, helm.go).
func TestParseHelmValuesImageOverrides(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		filename       string
		body           string
		wantName       string
		wantRepository string
		wantTag        string
		wantDigest     string
		wantEnv        string
	}{
		{
			name:     "sibling_tag_key",
			filename: "values.yaml",
			body: `
image:
  repository: ghcr.io/example/checkout-service
  tag: "1.2.3"
`,
			wantName:       "ghcr.io/example/checkout-service",
			wantRepository: "ghcr.io/example/checkout-service",
			wantTag:        "1.2.3",
		},
		{
			name:     "inline_repo_colon_tag",
			filename: "values.yaml",
			body: `
image:
  repository: ghcr.io/example/checkout-service:v2.0.0
`,
			wantName:       "ghcr.io/example/checkout-service:v2.0.0",
			wantRepository: "ghcr.io/example/checkout-service",
			wantTag:        "v2.0.0",
		},
		{
			name:     "inline_digest",
			filename: "values.yaml",
			body: `
image:
  repository: ghcr.io/example/checkout-service@sha256:abc123def456
`,
			wantName:       "ghcr.io/example/checkout-service@sha256:abc123def456",
			wantRepository: "ghcr.io/example/checkout-service",
			wantDigest:     "sha256:abc123def456",
		},
		{
			name:     "sibling_digest_key_plus_sibling_tag",
			filename: "values.yaml",
			body: `
image:
  repository: ghcr.io/example/checkout-service
  tag: "1.2.3"
  digest: sha256:def789abc012
`,
			wantName:       "ghcr.io/example/checkout-service",
			wantRepository: "ghcr.io/example/checkout-service",
			wantTag:        "1.2.3",
			wantDigest:     "sha256:def789abc012",
		},
		{
			name:     "filename_env_dash_form",
			filename: "values-prod.yaml",
			body: `
image:
  repository: ghcr.io/example/checkout-service
  tag: "1.2.3"
`,
			wantName:       "ghcr.io/example/checkout-service",
			wantRepository: "ghcr.io/example/checkout-service",
			wantTag:        "1.2.3",
			wantEnv:        "prod",
		},
		{
			name:     "filename_env_dot_form",
			filename: "values.staging.yaml",
			body: `
image:
  repository: ghcr.io/example/checkout-service
  tag: "1.2.3"
`,
			wantName:       "ghcr.io/example/checkout-service",
			wantRepository: "ghcr.io/example/checkout-service",
			wantTag:        "1.2.3",
			wantEnv:        "staging",
		},
		{
			name:     "no_env_signal",
			filename: "values.yaml",
			body: `
image:
  repository: ghcr.io/example/checkout-service
  tag: "1.2.3"
`,
			wantName:       "ghcr.io/example/checkout-service",
			wantRepository: "ghcr.io/example/checkout-service",
			wantTag:        "1.2.3",
			wantEnv:        "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filePath := writeYAMLTestFile(t, tc.filename, tc.body)
			got, err := Parse(filePath, false, Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}

			rows := yamlBucketForTest(t, got, "image_overrides")
			if len(rows) != 1 {
				t.Fatalf("len(image_overrides) = %d, want 1: %#v", len(rows), rows)
			}
			row := rows[0]
			assertYAMLField(t, row, "name", tc.wantName)
			assertYAMLField(t, row, "repository", tc.wantRepository)
			assertYAMLField(t, row, "tag", tc.wantTag)
			assertYAMLField(t, row, "digest", tc.wantDigest)
			assertYAMLField(t, row, "environment", tc.wantEnv)
			assertYAMLField(t, row, "source", "helm")
			assertYAMLField(t, row, "path", filePath)
			assertYAMLField(t, row, "lang", "yaml")
		})
	}
}

// TestParseHelmValuesImageOverridesEnvironmentFromPath proves the
// ".../environments/<env>/..." directory signal (environmentFromPath,
// observability_helpers.go) takes priority over the values-<env>.yaml
// filename inference, and that a bare directory segment with no
// "environments" marker is NOT treated as an environment signal. Issue #5440
// stays conservative on purpose; broader keyword-based environment detection
// is issue #5444's scope.
func TestParseHelmValuesImageOverridesEnvironmentFromPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		relPath string
		wantEnv string
	}{
		{
			name:    "environments_directory_signal",
			relPath: "environments/staging/values.yaml",
			wantEnv: "staging",
		},
		{
			name:    "environments_directory_overrides_filename_inference",
			relPath: "environments/staging/values-prod.yaml",
			wantEnv: "staging",
		},
		{
			name:    "bare_directory_name_is_not_an_environment_signal",
			relPath: "prod/values.yaml",
			wantEnv: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filePath := writeYAMLTestFile(t, tc.relPath, `
image:
  repository: ghcr.io/example/checkout-service
  tag: "1.2.3"
`)
			got, err := Parse(filePath, false, Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}
			rows := yamlBucketForTest(t, got, "image_overrides")
			if len(rows) != 1 {
				t.Fatalf("len(image_overrides) = %d, want 1: %#v", len(rows), rows)
			}
			assertYAMLField(t, rows[0], "environment", tc.wantEnv)
		})
	}
}

// TestParseHelmValuesImageOverridesEmptyWhenNoImages proves a values file
// with no "image:" block yields an empty image_overrides bucket rather than
// a phantom row.
func TestParseHelmValuesImageOverridesEmptyWhenNoImages(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "values.yaml", `
replicaCount: 2
service:
  port: 8080
`)
	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	rows := yamlBucketForTest(t, got, "image_overrides")
	if len(rows) != 0 {
		t.Fatalf("image_overrides = %#v, want empty bucket, no phantom rows", rows)
	}
}

// TestParseHelmValuesImageOverridesDoesNotChangeImageRepositories is an
// explicit regression guard (issue #5440): adding the image_overrides bucket
// must not change helm_values[].image_repositories, which strips tag/digest
// and has existing downstream consumers that depend on its byte-identical
// output.
func TestParseHelmValuesImageOverridesDoesNotChangeImageRepositories(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "values.yaml", `
image:
  repository: ghcr.io/example/checkout-service:v2.0.0
sidecar:
  image:
    repository: ghcr.io/example/envoy@sha256:abc123
`)
	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	values := yamlBucketForTest(t, got, "helm_values")
	if len(values) != 1 {
		t.Fatalf("len(helm_values) = %d, want 1: %#v", len(values), values)
	}
	assertYAMLField(t, values[0], "image_repositories", "ghcr.io/example/checkout-service,ghcr.io/example/envoy")

	overrides := yamlBucketForTest(t, got, "image_overrides")
	if len(overrides) != 2 {
		t.Fatalf("len(image_overrides) = %d, want 2: %#v", len(overrides), overrides)
	}
}
