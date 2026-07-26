// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"strings"
	"testing"
)

// source_uri and repository feed two DIFFERENT canonical-repository-id
// derivations that the reducer compares: a run's id comes from the API's
// repository.html_url, a ci.deployment_event's from source_uri. When they
// disagree, attachDeploymentEventsToRuns skips every event for the run, and
// the deployment_unanchored warning cannot report it because that warning keys
// on sha rather than repository.
//
// validateTargetURL constrains scheme, host and credentials but never the path,
// and the default only applies when source_uri is empty, so each config below
// was previously accepted and produced silent, total, per-run event loss on
// plain github.com with no rename and no enterprise host.
func TestValidateTargetRejectsSourceURIThatDoesNotNameTheRepository(t *testing.T) {
	t.Parallel()

	for name, sourceURI := range map[string]string{
		"different owner":      "https://github.com/acme-inc/api",
		"different repository": "https://github.com/acme/api-legacy",
		"extra path segments":  "https://github.com/acme/api/tree/main",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := validateTarget(baseDeploymentTarget(sourceURI))
			if err == nil {
				t.Fatalf("validateTarget(source_uri=%q) = nil error, want rejection: "+
					"a source_uri that does not name the repository silently drops every "+
					"deployment event for the run", sourceURI)
			}
			if !strings.Contains(err.Error(), "source_uri") {
				t.Fatalf("error = %v, want it to name source_uri", err)
			}
		})
	}
}

// The spellings NormalizeRemoteURL absorbs must still be accepted, so the
// validator tightens a real ambiguity without rejecting working configs.
func TestValidateTargetAcceptsEquivalentSourceURISpellings(t *testing.T) {
	t.Parallel()

	for name, sourceURI := range map[string]string{
		"exact":           "https://github.com/acme/api",
		"trailing slash":  "https://github.com/acme/api/",
		"dot git suffix":  "https://github.com/acme/api.git",
		"upper case path": "https://github.com/ACME/API",
		"empty defaults":  "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := validateTarget(baseDeploymentTarget(sourceURI)); err != nil {
				t.Fatalf("validateTarget(source_uri=%q) = %v, want accepted", sourceURI, err)
			}
		})
	}
}

func baseDeploymentTarget(sourceURI string) TargetConfig {
	return TargetConfig{
		ScopeID:             "ci_cd_run:github_actions:acme:api",
		Repository:          "acme/api",
		Token:               "t",
		AllowedRepositories: []string{"acme/api"},
		SourceURI:           sourceURI,
		MaxJobs:             1,
		MaxArtifacts:        1,
	}
}
