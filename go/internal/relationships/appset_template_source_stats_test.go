// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// appSetStructuredEnvelope builds the parsed_file_data form of an
// ApplicationSet in repo-gitops. destServer is the literal or templated
// destination server ("" for none).
func appSetStructuredEnvelope(generatorRepo, templateRepo, destServer string) facts.Envelope {
	return facts.Envelope{
		ScopeID: "repo-gitops",
		Payload: map[string]any{
			"artifact_type": "argocd",
			"relative_path": "applicationsets/svc.yaml",
			"parsed_file_data": map[string]any{
				"argocd_applicationsets": []any{
					map[string]any{
						"name":                   "svc",
						"generator_source_repos": "https://github.com/myorg/" + generatorRepo,
						"generator_source_paths": "argocd/svc/overlays/*/config.yaml",
						"template_source_repos":  "https://github.com/myorg/" + templateRepo,
						"template_source_paths":  "deploy",
						"dest_server":            destServer,
						"dest_namespace":         "svc",
					},
				},
			},
		},
	}
}

// TestApplicationSetTemplateSourceStatsCountsEachOutcome pins the exact tally
// for each outcome, for the YAML and the structured path.
func TestApplicationSetTemplateSourceStatsCountsEachOutcome(t *testing.T) {
	t.Parallel()

	const literal = "https://kubernetes.default.svc"
	const templated = "{{ .server }}"
	cases := []struct {
		name      string
		envelopes []facts.Envelope
		want      ApplicationSetTemplateSourceStats
	}{
		{
			name: "yaml deploy source",
			envelopes: []facts.Envelope{
				appSetConfigRepoSet("central-config", appSetLiteralDestination),
				appSetConfigRepoConfig("repo-central", "prod", "other-service"),
			},
			want: ApplicationSetTemplateSourceStats{DeploySource: 1},
		},
		{
			name: "yaml deploy source templated destination",
			envelopes: []facts.Envelope{
				appSetConfigRepoSet("central-config", appSetTemplatedDestination),
				appSetConfigRepoConfig("repo-central", "prod", "other-service"),
			},
			want: ApplicationSetTemplateSourceStats{DeploySource: 1, SkippedTemplatedDestination: 1},
		},
		{
			name: "yaml self reference",
			envelopes: []facts.Envelope{
				appSetConfigRepoSet("app-repo", appSetLiteralDestination),
				appSetConfigRepoConfig("repo-app", "prod", "app-repo"),
			},
			want: ApplicationSetTemplateSourceStats{SelfReference: 1},
		},
		{
			name: "yaml self reference many overlays tallies once",
			envelopes: []facts.Envelope{
				appSetConfigRepoSet("app-repo", appSetTemplatedDestination),
				appSetConfigRepoConfig("repo-app", "prod", "app-repo"),
				appSetConfigRepoConfig("repo-app", "staging", "app-repo"),
			},
			want: ApplicationSetTemplateSourceStats{SelfReference: 1, SkippedTemplatedDestination: 1},
		},
		{
			name: "yaml control repo as template source",
			envelopes: []facts.Envelope{
				appSetConfigRepoSet("central-config", appSetLiteralDestination),
				appSetConfigRepoConfig("repo-central", "prod", "gitops-repo"),
				appSetConfigRepoConfig("repo-central", "staging", "gitops-repo"),
			},
			want: ApplicationSetTemplateSourceStats{SkippedControlRepo: 1},
		},
		{
			name: "yaml config only repo counts nothing",
			envelopes: []facts.Envelope{
				appSetConfigRepoSet("central-config", appSetLiteralDestination),
				appSetConfigRepoConfig("repo-central", "prod", "unindexed-repo"),
			},
			want: ApplicationSetTemplateSourceStats{},
		},
		{
			name:      "structured deploy source",
			envelopes: []facts.Envelope{appSetStructuredEnvelope("central-config", "other-service", literal)},
			want:      ApplicationSetTemplateSourceStats{DeploySource: 1},
		},
		{
			name:      "structured deploy source templated destination",
			envelopes: []facts.Envelope{appSetStructuredEnvelope("central-config", "other-service", templated)},
			want:      ApplicationSetTemplateSourceStats{DeploySource: 1, SkippedTemplatedDestination: 1},
		},
		{
			name:      "structured self reference",
			envelopes: []facts.Envelope{appSetStructuredEnvelope("app-repo", "app-repo", literal)},
			want:      ApplicationSetTemplateSourceStats{SelfReference: 1},
		},
		{
			name:      "structured self reference templated destination",
			envelopes: []facts.Envelope{appSetStructuredEnvelope("app-repo", "app-repo", templated)},
			want:      ApplicationSetTemplateSourceStats{SelfReference: 1, SkippedTemplatedDestination: 1},
		},
		{
			name:      "structured control repo as template source",
			envelopes: []facts.Envelope{appSetStructuredEnvelope("central-config", "gitops-repo", literal)},
			want:      ApplicationSetTemplateSourceStats{SkippedControlRepo: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, stats := DiscoverEvidenceWithStats(tc.envelopes, appSetConfigRepoCatalog)
			if stats.ApplicationSetTemplateSource != tc.want {
				t.Fatalf("stats = %+v, want %+v", stats.ApplicationSetTemplateSource, tc.want)
			}
		})
	}
}

// TestApplicationSetTemplateSourceStatsMatchEmittedFacts guards the per-fact
// contract: the two emit outcomes equal the number of facts of their kind.
func TestApplicationSetTemplateSourceStatsMatchEmittedFacts(t *testing.T) {
	t.Parallel()
	evidence, stats := DiscoverEvidenceWithStats([]facts.Envelope{
		appSetConfigRepoSet("app-repo", appSetLiteralDestination),
		appSetConfigRepoConfig("repo-app", "prod", "app-repo"),
		appSetConfigRepoConfig("repo-app", "staging", "app-repo"),
	}, appSetConfigRepoCatalog)

	if got, want := stats.ApplicationSetTemplateSource.SelfReference,
		len(evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetTemplateSource)); got != want {
		t.Fatalf("self_reference = %d, facts = %d", got, want)
	}
	if got, want := stats.ApplicationSetTemplateSource.DeploySource,
		len(evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource)); got != want {
		t.Fatalf("deploy_source = %d, facts = %d", got, want)
	}
}
