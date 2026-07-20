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
		{
			// Regression guard for a P1 accuracy defect: values.schema.yaml is a
			// values-schema convention, not an environment. Any filename suffix
			// outside the known deployment-environment token allowlist must
			// resolve to "", never a fabricated environment.
			name:     "filename_suffix_schema_is_not_an_environment",
			filename: "values.schema.yaml",
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
		{
			// values.example.yaml is documentation, not an environment.
			name:     "filename_suffix_example_is_not_an_environment",
			filename: "values.example.yaml",
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
		{
			// values.template.yaml is a scaffolding template, not an environment.
			name:     "filename_suffix_template_is_not_an_environment",
			filename: "values.template.yaml",
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
		{
			// "ci" is not in the known deployment-environment token set.
			name:     "filename_suffix_ci_is_not_an_environment",
			filename: "values-ci.yaml",
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
		{
			// "local" is not in the known deployment-environment token set.
			name:     "filename_suffix_local_is_not_an_environment",
			filename: "values.local.yaml",
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
		{
			name:     "filename_env_uat",
			filename: "values-uat.yaml",
			body: `
image:
  repository: ghcr.io/example/checkout-service
  tag: "1.2.3"
`,
			wantName:       "ghcr.io/example/checkout-service",
			wantRepository: "ghcr.io/example/checkout-service",
			wantTag:        "1.2.3",
			wantEnv:        "uat",
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
		{
			// The .../environments/<env>/... path segment is an explicit author
			// declaration, not an inference -- it is intentionally UNGATED by the
			// filename allowlist and returns whatever the author wrote, even a
			// name outside the known deployment-environment token set. This is
			// the deliberate asymmetry: only the filename fallback is gated.
			name:    "environments_directory_signal_bypasses_the_filename_allowlist",
			relPath: "environments/weirdname/values.yaml",
			wantEnv: "weirdname",
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

// TestParseHelmValuesImageOverridesDedupesExactDuplicateRows decides and pins
// the duplicate-row question (issue #5440 review): when a Helm values file
// declares the SAME repository under two different "image:" blocks with an
// identical tag/digest, the two resulting rows are byte-for-byte identical --
// image_overrides carries no "declared under" field to distinguish them, so
// shipping both would be pure phantom noise. helm_values[].image_repositories
// already dedupes (deduplicateStrings, helm.go); image_overrides follows the
// same principle: dedupe exact-identical rows, but keep rows that differ in
// ANY field (a second block declaring the same repository under a different
// tag is a genuinely distinct declaration, not noise).
func TestParseHelmValuesImageOverridesDedupesExactDuplicateRows(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "values.yaml", `
serviceA:
  image:
    repository: ghcr.io/example/shared-sidecar
    tag: "1.0.0"
serviceB:
  image:
    repository: ghcr.io/example/shared-sidecar
    tag: "1.0.0"
serviceC:
  image:
    repository: ghcr.io/example/shared-sidecar
    tag: "2.0.0"
`)
	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	overrides := yamlBucketForTest(t, got, "image_overrides")
	// Exactly 2 rows: the serviceA/serviceB pair collapses to one (identical
	// repository AND tag), serviceC survives as a distinct row (same
	// repository, different tag).
	if len(overrides) != 2 {
		t.Fatalf("len(image_overrides) = %d, want 2 (exact duplicate collapsed, differing tag kept): %#v", len(overrides), overrides)
	}
	tags := map[string]int{}
	for _, row := range overrides {
		name, _ := row["name"].(string)
		if name != "ghcr.io/example/shared-sidecar" {
			t.Fatalf("unexpected row name = %q in %#v", name, overrides)
		}
		tag, _ := row["tag"].(string)
		tags[tag]++
	}
	if tags["1.0.0"] != 1 {
		t.Fatalf("tag 1.0.0 count = %d, want 1 (deduped): %#v", tags["1.0.0"], overrides)
	}
	if tags["2.0.0"] != 1 {
		t.Fatalf("tag 2.0.0 count = %d, want 1 (kept, distinct tag): %#v", tags["2.0.0"], overrides)
	}
}

// TestImageOverrideKeyStaysInSyncWithRowShape is a structural drift guard
// (issue #5440 review): dedupeImageOverrideRows detects an exact-duplicate
// row by comparing every field named in imageOverrideRowFields
// (image_overrides.go), read individually rather than formatted into a
// string. If a row builder ever grows a new field with no matching addition
// to that list, dedup would silently ignore the new field and could wrongly
// collapse two rows that actually differ. This test cannot catch a field
// RENAME, but it catches the far more common drift: a field ADDED to a row
// with no matching addition to imageOverrideRowFields, by asserting the two
// field counts stay equal.
func TestImageOverrideKeyStaysInSyncWithRowShape(t *testing.T) {
	t.Parallel()

	row := helmImageOverrideRow(
		map[string]any{"repository": "ghcr.io/example/checkout-service", "tag": "1.2.3"},
		"values.yaml",
		"prod",
	)
	if row == nil {
		t.Fatal("helmImageOverrideRow() = nil, want a row for a valid image map")
	}

	if got, want := len(imageOverrideRowFields), len(row); got != want {
		t.Fatalf(
			"imageOverrideRowFields has %d entries but an image_overrides row has %d keys -- "+
				"dedupeImageOverrideRows's field list (image_overrides.go) is out of sync with "+
				"the row shape; add the missing field to imageOverrideRowFields or a new row "+
				"field will silently escape duplicate-row comparison",
			got, want,
		)
	}
}
