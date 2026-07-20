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
			// Case fragmentation would matter the moment any consumer treats
			// this field as a join key, so it must be canonicalized to
			// lowercase now.
			name:    "environments_directory_signal_is_lowercased",
			relPath: "environments/Prod/values.yaml",
			wantEnv: "prod",
		},
		{
			// P2-1 accuracy defect (round-2 independent review): the FIRST
			// "environments" marker ("modules/environments/") satisfies the
			// directory guard (a "scripts" segment follows it), so a
			// first-marker-wins scan wrongly stops there and returns "scripts".
			// The LAST marker ("scripts/environments/prod/") is the more
			// specific, closest-to-the-file declaration and must win instead.
			name:    "later_more_specific_environments_marker_wins_over_an_earlier_valid_one",
			relPath: "modules/environments/scripts/environments/prod/values.yaml",
			wantEnv: "prod",
		},
		{
			// Same defect class, reviewer's second repro: the FIRST marker's
			// captured segment happens to be the literal string "environments"
			// (the name of the SECOND marker directory), which is a
			// syntactically plausible but wrong answer -- exactly the "worse
			// than empty" case the fix must eliminate.
			name:    "later_environments_marker_wins_even_when_earlier_capture_looks_like_a_real_word",
			relPath: "charts/environments/environments/prod/values.yaml",
			wantEnv: "prod",
		},
		{
			// Three nested "environments" markers: the LAST one (closest to
			// the file) must win, not the first or the middle one.
			name:    "three_nested_environments_markers_last_one_wins",
			relPath: "a/environments/b/environments/c/environments/prod/values.yaml",
			wantEnv: "prod",
		},
		{
			// Judgment call (round-2 independent review, documented here per
			// the coordinator's request): the LAST "environments" marker sits
			// directly against the file (charts/environments/values.yaml
			// shape, fails the directory guard on its own), but an EARLIER
			// marker ("environments/prod/") is a fully valid, independent
			// directory declaration. The earlier valid declaration is real
			// information -- an author genuinely laid this repo out with a
			// "prod" environment directory -- while the later invalid
			// occurrence carries none (it fails the same guard that rejects
			// environments/values.yaml on its own, so it must be skipped, not
			// blindly preferred for being "closer to the file"). The correct
			// behavior is therefore "last VALID marker wins", which falls
			// back to this earlier one rather than emitting "" and discarding
			// a real author declaration.
			name:    "last_marker_fails_the_guard_earlier_valid_marker_still_wins",
			relPath: "environments/prod/subdir/environments/values.yaml",
			wantEnv: "prod",
		},
		{
			// P2 accuracy defect (round-3 independent review): the captured
			// <env> segment can itself BE the marker keyword "environments"
			// when two markers sit back to back -- at index 0 the directory
			// guard passes because parts[1] is the START of the next marker,
			// not a real environment name. That phantom value must not
			// survive as the final answer; there is no other valid marker
			// here, so the correct result is "".
			name:    "adjacent_environments_markers_capture_the_keyword_itself_is_not_an_environment",
			relPath: "environments/environments/values.yaml",
			wantEnv: "",
		},
		{
			// Same defect, nested: proves the keyword-capture rejection
			// applies regardless of where in the path the adjacent markers
			// sit, not just at index 0.
			name:    "nested_adjacent_environments_markers_capture_the_keyword_itself_is_not_an_environment",
			relPath: "a/environments/environments/values.yaml",
			wantEnv: "",
		},
		{
			// The important case: with the keyword-capture correctly
			// rejected, the path signal is empty and
			// helmImageOverrideEnvironment must fall through to the
			// values-<env>.yaml filename inference, which fires correctly.
			// Before the fix, the phantom "environments" path value
			// suppressed this entirely -- a wrong answer masking a right
			// one, worse than either alone.
			name:    "adjacent_environments_markers_do_not_suppress_the_filename_fallback",
			relPath: "environments/environments/values-prod.yaml",
			wantEnv: "prod",
		},
		{
			// Three ADJACENT "environments" markers followed by a real
			// environment directory: the first two captures are each the
			// keyword itself (rejected), the third capture is "prod" (a
			// real directory, since a file segment follows it) and wins.
			name:    "three_adjacent_environments_markers_then_a_real_environment",
			relPath: "environments/environments/environments/prod/values.yaml",
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

// TestImageOverrideDirectoryEnvironmentRejectsInvalidCapturedSegments is a
// defense-in-depth regression guard (issue #5440 round-4 review) for
// imageOverrideDirectoryEnvironment's general validity check on a captured
// <env> segment: it must be non-empty after trimming and must not be
// composed solely of dots (a generalization of the existing marker-keyword
// rejection, same skip-don't-clear semantics).
//
// This is NOT a fix for a reachable production defect. Every real path
// reaching this function comes from the collector's file discovery, which
// cleans every path via filepath.ToSlash(filepath.Clean(...)) in
// go/internal/collector/discovery (verified: 8 call sites) before the
// parser ever sees it, so "//" (an empty segment) and "."/".." segments
// cannot occur in a real collector-produced path. This test calls
// imageOverrideDirectoryEnvironment directly with raw, uncleaned path
// strings specifically to bypass that cleaning -- writeYAMLTestFile's
// filepath.Join (used by the table above) would clean "." and ".." away
// before they ever reached the function, the same as the real collector
// does, so this cannot be tested through the normal Parse() pipeline. The
// point is robustness for any future caller that does NOT pre-clean, not
// proof of a bug in the current pipeline.
func TestImageOverrideDirectoryEnvironmentRejectsInvalidCapturedSegments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		path    string
		wantEnv string
	}{
		{
			name:    "dot_dot_segment_is_not_a_valid_environment",
			path:    "environments/../values.yaml",
			wantEnv: "",
		},
		{
			name:    "dot_segment_is_not_a_valid_environment",
			path:    "environments/./values.yaml",
			wantEnv: "",
		},
		{
			// The important case: an invalid LATER marker (an empty segment
			// from a double slash) must not CLEAR an earlier valid one --
			// skip-don't-clear semantics, same as the existing guard-failing
			// and keyword-collision cases.
			name:    "empty_segment_from_a_double_slash_does_not_clear_an_earlier_valid_marker",
			path:    "environments/prod/environments//values.yaml",
			wantEnv: "prod",
		},
		{
			// Not part of the rejected class: a segment that merely LOOKS
			// like a filename is still a structurally valid directory
			// capture. This function only checks structural plausibility
			// (non-empty, not dot-only), never semantic plausibility --
			// judging whether a string "looks like" a real environment name
			// is issue #5444's broader-detection scope, not this guard's.
			name:    "a_filename_shaped_directory_segment_is_still_a_valid_capture",
			path:    "environments/config.yaml/subdir/values.yaml",
			wantEnv: "config.yaml",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := imageOverrideDirectoryEnvironment(tc.path); got != tc.wantEnv {
				t.Fatalf("imageOverrideDirectoryEnvironment(%q) = %q, want %q", tc.path, got, tc.wantEnv)
			}
		})
	}
}

// TestParseHelmValuesImageOverridesEnvironmentCaseAgreement is a direct
// regression guard for the P1-b case-fragmentation defect (independent
// review): the ".../environments/<env>/..." path signal and the
// values-<env>.yaml filename signal must resolve the SAME declared
// environment to the SAME string, not two different-cased strings that
// would silently fragment one environment into two the moment any consumer
// treats this field as a join key.
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
