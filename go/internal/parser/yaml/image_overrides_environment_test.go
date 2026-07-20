// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"testing"
)

// Environment-inference tests split out of image_overrides_test.go to keep
// that file under the repo's 500-line package-file cap (issue #5440,
// following the same split precedent as engine_yaml_semantics_test.go /
// engine_yaml_semantics_kustomize_test.go).

// TestParseHelmValuesImageOverridesEnvironmentFromPath proves the
// ".../environments/<env>/..." directory signal (environmentFromPath,
// observability_helpers.go, wrapped by imageOverrideDirectoryEnvironment)
// takes priority over the values-<env>.yaml filename inference, and that a
// bare directory segment with no "environments" marker is NOT treated as an
// environment signal. Issue #5440 stays conservative on purpose; broader
// keyword-based environment detection is issue #5444's scope.
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
		{
			// P1-a accuracy defect (independent review): a values file sitting
			// DIRECTLY inside a directory literally named "environments/" has no
			// author-declared environment -- environmentFromPath would otherwise
			// return the file's OWN BASENAME as the "environment". The <env>
			// segment must be a real DIRECTORY (at least one further path segment
			// must follow it), matching the identical guard already applied to
			// the values.schema.yaml filename case.
			name:    "environments_directly_containing_the_file_is_not_an_environment_signal",
			relPath: "environments/values.yaml",
			wantEnv: "",
		},
		{
			// Same P1-a defect, nested deeper: "environments/" is not the repo
			// root here either.
			name:    "nested_environments_directly_containing_the_file_is_not_an_environment_signal",
			relPath: "charts/foo/environments/values.yaml",
			wantEnv: "",
		},
		{
			// A charts/environments/values.yaml layout is the same shape as the
			// nested case above, named explicitly per the review's own probe.
			name:    "charts_environments_directly_containing_the_file_is_not_an_environment_signal",
			relPath: "charts/environments/values.yaml",
			wantEnv: "",
		},
		{
			name:    "environments_directory_signal_explicit_prod",
			relPath: "environments/prod/values.yaml",
			wantEnv: "prod",
		},
		{
			// P1-b accuracy defect (independent review): the path signal returned
			// raw case ("Prod"), fragmenting the same environment into two
			// distinct strings against the filename signal's lowercase output.
			// #5441 is about to project this field onto graph edges as a join
			// key, so it must be canonicalized to lowercase.
			name:    "environments_directory_signal_is_lowercased",
			relPath: "environments/Prod/values.yaml",
			wantEnv: "prod",
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

// TestParseHelmValuesImageOverridesEnvironmentCaseAgreement is a direct
// regression guard for the P1-b case-fragmentation defect (independent
// review): the ".../environments/<env>/..." path signal and the
// values-<env>.yaml filename signal must resolve the SAME declared
// environment to the SAME string, not two different-cased strings that
// would silently fragment one environment into two once #5441 projects this
// field onto graph edges as a join key.
func TestParseHelmValuesImageOverridesEnvironmentCaseAgreement(t *testing.T) {
	t.Parallel()

	pathSignal := helmImageOverrideEnvironment("environments/Prod/values.yaml")
	filenameSignal := helmImageOverrideEnvironment("charts/foo/values-PROD.yaml")

	if pathSignal != "prod" {
		t.Fatalf("path signal environment = %q, want %q", pathSignal, "prod")
	}
	if filenameSignal != "prod" {
		t.Fatalf("filename signal environment = %q, want %q", filenameSignal, "prod")
	}
	if pathSignal != filenameSignal {
		t.Fatalf("path signal %q != filename signal %q for the same declared environment", pathSignal, filenameSignal)
	}
}
