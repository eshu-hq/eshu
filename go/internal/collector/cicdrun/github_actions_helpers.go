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
// repository ID. Precedence:
//  1. repository.HTMLURL (validated: must parse with a scheme and host).
//  2. Constructed https://<host>/<FullName> where host comes from
//     repositoryHost with an api. prefix stripped, and FullName is the
//     repository's provider-level name.
//
// Never hashes a per-run SourceURI verbatim — that would embed the run ID
// or an api. host and mint a different canonical id per run, permanently
// breaking the backbone join with the git collector.
func repositoryCanonicalURL(repository githubRepository, ctx FixtureContext) string {
	// Tier 1: repository.HTMLURL (the canonical provider URL).
	if trimmed := strings.TrimSpace(repository.HTMLURL); trimmed != "" {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" && parsed.Scheme != "" {
			return trimmed
		}
	}
	// Tier 2: construct from host + FullName.
	host := repositoryHost(repository, ctx)
	host = canonicalGitHubHost(host)
	fullName := strings.Trim(strings.TrimSpace(repository.FullName), "/")
	if fullName == "" || host == "" {
		return ""
	}
	return "https://" + host + "/" + fullName
}

// canonicalGitHubHost maps the api. subdomain prefix to the canonical host
// for github.com and GitHub Enterprise Cloud (ghe.com) data-residency
// tenants only. All other hosts — including legitimate non-GitHub api.*
// enterprise hosts — pass through unchanged.
//
// The mapping is narrow by design: an unconditional prefix strip would
// silently break the cross-collector join for any enterprise whose git
// host legitimately starts with "api.". GHES self-hosted instances serve
// the API at /api/v3 on the SAME host, so they never carry an api.
// subdomain and are already unchanged here.
//
// Host comparison is case-insensitive (hosts are case-insensitive per
// RFC 3986 §6.2.2.1).
func canonicalGitHubHost(host string) string {
	// Strip port so api.github.com:8443 maps to github.com.
	hostname, _, _ := strings.Cut(host, ":")
	lower := strings.ToLower(hostname)
	switch {
	case lower == "api.github.com":
		return "github.com"
	case strings.HasSuffix(lower, ".ghe.com") && strings.HasPrefix(lower, "api."):
		return lower[len("api."):]
	default:
		return hostname
	}
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
