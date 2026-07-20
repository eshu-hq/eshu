// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// repositoryID returns the canonical repository identifier (repository:r_<hex>)
// derived from the repository's HTML URL, the fixture's SourceURI, or a
// host/FullName fallback, matching the join contract the git collector and
// repositoryidentity.CanonicalRepositoryID already enforce.
func repositoryID(repository githubRepository, ctx FixtureContext) string {
	canonicalURL := repositoryCanonicalURL(repository, ctx)
	if canonicalURL == "" {
		return ""
	}
	id, err := repositoryidentity.CanonicalRepositoryID(canonicalURL, "")
	if err != nil {
		return ""
	}
	return id
}

// repositoryCanonicalURL returns the URL string used to derive the canonical
// repository ID. It prefers the repository's HTML URL, then the fixture
// SourceURI, then falls back to the host/FullName URL pattern.
func repositoryCanonicalURL(repository githubRepository, ctx FixtureContext) string {
	// Prefer repository.HTMLURL (the canonical GitHub API URL).
	if trimmed := strings.TrimSpace(repository.HTMLURL); trimmed != "" {
		if _, err := url.Parse(trimmed); err == nil {
			return trimmed
		}
	}
	// Next: SourceURI (the API URL the fixture or runtime harvested from).
	if trimmed := strings.TrimSpace(ctx.SourceURI); trimmed != "" {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
			return trimmed
		}
	}
	// Last: host + "/" + FullName fallback (preserving repositoryHost semantics).
	host := repositoryHost(repository, ctx)
	fullName := strings.Trim(strings.TrimSpace(repository.FullName), "/")
	if fullName == "" {
		return ""
	}
	// Build an HTTPS URL from host + FullName. This is the same pattern
	// repositoryID used before canonicalization: host + "/" + FullName.
	return "https://" + host + "/" + fullName
}

// providerRepositoryID returns the raw provider-level repository locator
// (e.g. "github.com/eshu-hq/eshu"), preserved as provenance alongside the
// canonical repository_id.
func providerRepositoryID(repository githubRepository, ctx FixtureContext) string {
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
