// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func repositoryID(repository githubRepository, ctx FixtureContext) string {
	fullName := strings.Trim(strings.TrimSpace(repository.FullName), "/")
	if fullName == "" {
		return ""
	}
	return repositoryHost(repository, ctx) + "/" + fullName
}

func repositoryHost(repository githubRepository, ctx FixtureContext) string {
	for _, rawURL := range []string{repository.HTMLURL, ctx.SourceURI, ctx.ScopeID} {
		parsed, err := url.Parse(rawURL)
		if err == nil && parsed.Host != "" {
			return parsed.Host
		}
	}
	return "github.com"
}

func defaultArtifactType(artifact githubArtifact) string {
	if trim(artifact.ArtifactType) != "" {
		return trim(artifact.ArtifactType)
	}
	return "generic"
}

func actionReference(stepName string) string {
	stepName = strings.TrimPrefix(trim(stepName), "Run ")
	if strings.Contains(stepName, "@") && !strings.Contains(stepName, " ") {
		return stepName
	}
	return ""
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := trim(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func artifactMatchesRun(run githubRun, artifact githubArtifact) bool {
	if artifact.WorkflowRun.ID != nil {
		artifactRunID, err := providerID(artifact.WorkflowRun.ID)
		if err != nil {
			return false
		}
		runID, err := providerID(run.ID)
		if err != nil || artifactRunID != "" && artifactRunID != runID {
			return false
		}
	}
	if trim(artifact.WorkflowRun.HeadSHA) != "" && trim(run.HeadSHA) != "" && trim(artifact.WorkflowRun.HeadSHA) != trim(run.HeadSHA) {
		return false
	}
	return true
}

func deduplicateEnvelopes(envelopes []facts.Envelope) []facts.Envelope {
	seen := make(map[string]bool, len(envelopes))
	out := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		if seen[envelope.FactID] {
			continue
		}
		seen[envelope.FactID] = true
		out = append(out, envelope)
	}
	return out
}
