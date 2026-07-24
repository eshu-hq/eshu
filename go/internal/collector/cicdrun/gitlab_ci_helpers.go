// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// payloadStringField reads a string-valued field a prior gitlabSharedPayload
// call placed on the fixture payload map (currently always "run_id" or
// "run_attempt", both set as plain strings — see gitlabSharedPayload in
// gitlab_ci_fixture.go). It returns a clear typed error instead of letting an
// unchecked type assertion panic, so a future refactor that changes
// gitlabSharedPayload's value types (e.g. to a typed wrapper) fails loudly at
// the call site instead of crashing the reducer/collector process.
func payloadStringField(payload map[string]any, key string) (string, error) {
	value, ok := payload[key].(string)
	if !ok {
		return "", fmt.Errorf("gitlab ci payload %s: expected string, got %T", key, payload[key])
	}
	return value, nil
}

// gitlabRepositoryID returns the canonical repository identifier
// (repository:r_<hex>) derived from the pipeline's web_url or the fixture's
// ScopeID, matching the join contract the git collector and
// repositoryidentity.CanonicalRepositoryID already enforce.
func gitlabRepositoryID(pipeline gitlabPipeline, ctx FixtureContext) string {
	canonicalURL := gitlabRepositoryCanonicalURL(pipeline, ctx)
	if canonicalURL == "" {
		return ""
	}
	id, err := repositoryidentity.CanonicalRepositoryID(canonicalURL, "")
	if err != nil {
		return ""
	}
	return id
}

// gitlabRepositoryCanonicalURL returns the URL used to derive the canonical
// repository ID. Precedence:
//  1. pipeline.web_url with GitLab's "/-/..." sub-resource suffix stripped.
//     GitLab always renders pipeline pages under the project root at
//     "/-/pipelines/<id>" (https://docs.gitlab.com/ee/api/pipelines.html), so
//     the prefix before "/-/" is always the project's canonical URL -- unlike
//     GitHub, GitLab's pipeline payload carries no separate repository object
//     with its own full_name/html_url fields.
//  2. ctx.ScopeID in "gitlab-ci://<host>/<path>" form, the collector's own
//     scope-identity encoding of the project, used when web_url is absent.
//
// Never hashes a per-pipeline SourceURI verbatim -- that would embed the
// pipeline ID and mint a different canonical id per run, permanently
// breaking the backbone join with the git collector.
func gitlabRepositoryCanonicalURL(pipeline gitlabPipeline, ctx FixtureContext) string {
	if projectURL := gitlabProjectURLFromWebURL(pipeline.WebURL); projectURL != "" {
		return projectURL
	}
	return gitlabProjectURLFromScopeID(ctx.ScopeID)
}

// gitlabProjectURLFromWebURL strips GitLab's "/-/<resource>" suffix from a
// pipeline or job web_url, leaving the project root URL. Returns "" when the
// URL is blank, unparseable, or does not contain the "/-/" separator GitLab
// always inserts before a sub-resource path.
func gitlabProjectURLFromWebURL(webURL string) string {
	trimmed := strings.TrimSpace(webURL)
	if trimmed == "" {
		return ""
	}
	idx := strings.Index(trimmed, "/-/")
	if idx <= 0 {
		return ""
	}
	projectURL := trimmed[:idx]
	parsed, err := url.Parse(projectURL)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return ""
	}
	return projectURL
}

// gitlabProjectURLFromScopeID reconstructs the project's HTTPS URL from a
// "gitlab-ci://<host>/<path>" scope ID. Returns "" when the scope ID does not
// carry the gitlab-ci scheme this collector always assigns.
func gitlabProjectURLFromScopeID(scopeID string) string {
	const prefix = "gitlab-ci://"
	trimmed := strings.TrimSpace(scopeID)
	if !strings.HasPrefix(trimmed, prefix) {
		return ""
	}
	rest := strings.Trim(strings.TrimPrefix(trimmed, prefix), "/")
	if rest == "" {
		return ""
	}
	return "https://" + rest
}

// gitlabProviderRepositoryID returns the raw provider-level repository
// locator (e.g. "gitlab.com/eshu-hq/gitlab-demo-service"), preserved as
// provenance alongside the canonical repository_id.
func gitlabProviderRepositoryID(pipeline gitlabPipeline, ctx FixtureContext) string {
	projectURL := gitlabRepositoryCanonicalURL(pipeline, ctx)
	if projectURL == "" {
		return ""
	}
	parsed, err := url.Parse(projectURL)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Host + parsed.Path
}

// gitlabArtifactType returns the artifact's declared file_type, defaulting to
// "generic" when GitLab omitted it -- mirrors defaultArtifactType's fallback
// for GitHub Actions artifacts.
func gitlabArtifactType(artifact gitlabArtifact) string {
	if trim(artifact.FileType) != "" {
		return trim(artifact.FileType)
	}
	return "generic"
}
