// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facet_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/facet"
)

func TestDetectFacets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		question        string
		wantSourceTool  string
		wantLanguage    string
		wantUnknownTool string
	}{
		// --- source_tool detection ---
		{
			name:           "deploy via Helm",
			question:       "which services deploy via Helm?",
			wantSourceTool: "helm",
		},
		{
			name:           "helm lowercase",
			question:       "show me helm deployments",
			wantSourceTool: "helm",
		},
		{
			name:           "terraform keyword",
			question:       "what repos use terraform?",
			wantSourceTool: "terraform",
		},
		{
			name:           "ansible keyword",
			question:       "which services are managed by ansible?",
			wantSourceTool: "ansible",
		},
		{
			name:           "puppet keyword",
			question:       "puppet modules that install nginx",
			wantSourceTool: "puppet",
		},
		{
			name:           "kustomize keyword",
			question:       "what deployments use kustomize overlays?",
			wantSourceTool: "kustomize",
		},
		{
			name:           "argocd keyword",
			question:       "services managed by ArgoCD",
			wantSourceTool: "argocd",
		},
		{
			name:           "github actions multi-word alias",
			question:       "which repos use github actions for CI?",
			wantSourceTool: "github_actions",
		},
		{
			name:           "docker compose multi-word alias",
			question:       "services defined in docker compose",
			wantSourceTool: "docker_compose",
		},
		{
			name:           "docker single word",
			question:       "repos that publish docker images",
			wantSourceTool: "docker",
		},
		{
			// "Jenkinsfile" is the conventional filename; the alias maps it to jenkins.
			name:           "jenkins via Jenkinsfile alias",
			question:       "which repos have a Jenkinsfile?",
			wantSourceTool: "jenkins",
		},
		{
			name:           "jenkins keyword direct",
			question:       "repos with jenkins pipelines",
			wantSourceTool: "jenkins",
		},
		{
			// "go modules" fires language=go AND "gomod" fires source_tool=gomod;
			// both are correct — the question legitimately scopes on both dimensions.
			name:           "gomod keyword",
			question:       "go modules that depend on lib-common via gomod",
			wantSourceTool: "gomod",
			wantLanguage:   "go",
		},
		{
			name:           "npm keyword",
			question:       "npm packages that reference lodash",
			wantSourceTool: "npm",
		},

		// --- language detection ---
		{
			name:         "Go repos language",
			question:     "what Go repos depend on lib-common?",
			wantLanguage: "go",
		},
		{
			name:         "golang alias",
			question:     "golang services that expose gRPC",
			wantLanguage: "go",
		},
		{
			name:         "python language",
			question:     "python services in the data platform",
			wantLanguage: "python",
		},
		{
			name:         "typescript alias",
			question:     "typescript packages depending on react",
			wantLanguage: "typescript",
		},
		{
			name:         "javascript alias",
			question:     "javascript components that import lodash",
			wantLanguage: "javascript",
		},
		{
			name:         "rust language",
			question:     "rust crates that use tokio",
			wantLanguage: "rust",
		},
		{
			name:         "java language",
			question:     "Java services that implement AuthService",
			wantLanguage: "java",
		},

		// --- unknown tool mention ---
		{
			name:            "unknown tool via phrase",
			question:        "which services deploy via Frobnicator?",
			wantUnknownTool: "frobnicator",
		},
		{
			name:            "unknown tool using phrase",
			question:        "repos managed using gobblygook",
			wantUnknownTool: "gobblygook",
		},

		// --- no facet / plain question ---
		{
			name:     "plain question yields empty",
			question: "which services are in production?",
		},
		{
			name:     "empty question",
			question: "",
		},
		{
			name:     "whitespace only",
			question: "   ",
		},
		{
			name:     "generic question about deployments",
			question: "how many repos do we have?",
		},

		// --- disambiguation ---
		{
			name:            "helm beats unknown",
			question:        "deployed via helm",
			wantSourceTool:  "helm",
			wantUnknownTool: "",
		},
		{
			name:            "unknown tool does not fire on short common word",
			question:        "services deployed via a new method",
			wantUnknownTool: "",
		},

		// --- adversarial: collision-prone tokens must NOT fire without qualifier ---
		{
			name:     "go as common verb yields empty",
			question: "where should I go to find the deployment config?",
		},
		{
			name:     "salt as culinary noun yields empty",
			question: "add a pinch of salt to the recipe",
		},
		{
			name:     "chef as common noun yields empty",
			question: "ask the chef what ingredients to use",
		},
		{
			name:     "cargo as common noun yields empty",
			question: "how much cargo capacity does the ship have?",
		},
		{
			name:     "pip as common verb yields empty",
			question: "pip down the list of suggestions",
		},
		{
			name:     "npm in isolation yields empty",
			question: "send me the npm",
		},
		{
			name:     "maven in isolation yields empty",
			question: "the maven of all trades",
		},

		// --- adversarial: collision-prone tokens WITH qualifier must fire ---
		{
			name:           "salt formula qualifies salt as tool",
			question:       "services using the salt formula",
			wantSourceTool: "salt",
		},
		{
			name:           "chef cookbook qualifies chef as tool",
			question:       "which chef cookbooks install nginx?",
			wantSourceTool: "chef",
		},
		{
			name:           "cargo via qualifies cargo as tool",
			question:       "deploy via cargo",
			wantSourceTool: "cargo",
		},
		{
			name:           "pip packages qualifies pip as tool",
			question:       "pip packages that depend on requests",
			wantSourceTool: "pip",
		},
		{
			name:           "npm packages qualifies npm as tool",
			question:       "npm packages that reference lodash",
			wantSourceTool: "npm",
		},
		{
			name:           "maven dependency qualifies maven as tool",
			question:       "maven dependency on commons-lang",
			wantSourceTool: "maven",
		},
		{
			name:         "go repos qualifies go as language",
			question:     "what Go repos depend on lib-common?",
			wantLanguage: "go",
		},
		{
			name:           "go modules fires both gomod source_tool and go language",
			question:       "go modules that depend on lib-common via gomod",
			wantSourceTool: "gomod",
			wantLanguage:   "go",
		},
		{
			name:         "golang alias always resolves without qualifier",
			question:     "golang services that expose gRPC",
			wantLanguage: "go",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := facet.DetectFacets(tc.question)

			if got.SourceTool != tc.wantSourceTool {
				t.Errorf("SourceTool = %q, want %q", got.SourceTool, tc.wantSourceTool)
			}
			if got.Language != tc.wantLanguage {
				t.Errorf("Language = %q, want %q", got.Language, tc.wantLanguage)
			}
			if got.UnknownToolMention != tc.wantUnknownTool {
				t.Errorf("UnknownToolMention = %q, want %q", got.UnknownToolMention, tc.wantUnknownTool)
			}
		})
	}
}
