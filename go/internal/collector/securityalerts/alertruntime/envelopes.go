// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s *ClaimedSource) envelopes(
	item workflow.WorkItem,
	target TargetConfig,
	collection alertCollection,
	observedAt time.Time,
) ([]facts.Envelope, error) {
	envs := make([]facts.Envelope, 0, len(collection.alerts))
	for _, alert := range collection.alerts {
		// ctx.ScopeID is always the target's committed generation scope so that
		// every envelope's ScopeID matches the scope committed to Postgres.
		// For per-repository targets that is the per-repo scope; for org
		// targets it is the org scope (the committed generation scope).
		//
		// For org targets ctx.RepositoryID carries the per-repo scope so that
		// payload["repository_id"] and the stableFactKey still key on the same
		// canonical per-repository value the reducer expects.
		ctx := securityalerts.EnvelopeContext{
			ScopeID:             target.ScopeID,
			GenerationID:        item.GenerationID,
			CollectorInstanceID: s.collectorInstanceID,
			FencingToken:        item.CurrentFencingToken,
			ObservedAt:          observedAt,
			SourceURI:           safeSourceURI(firstNonBlank(target.SourceURI, alert.HTMLURL)),
		}
		if target.IsOrganizationScope() {
			// Derive the per-repository scope from the alert's repository
			// object. Alerts whose repository cannot be resolved are skipped
			// rather than misattributed to the org scope.
			repoScopeID := organizationAlertScopeID(alert)
			if repoScopeID == "" {
				continue
			}
			// Honor the operator-declared allowlist: only fan out alerts for
			// explicitly allowed repositories. This preserves the private-data
			// boundary that the per-repository path enforces via its allowlist.
			if !target.RepositoryAllowed(organizationAlertFullName(alert)) {
				continue
			}
			ctx.RepositoryID = repoScopeID
		}
		env, err := securityalerts.NewGitHubDependabotAlertEnvelope(ctx, alert)
		if err != nil {
			return nil, err
		}
		if env.FactKind != facts.SecurityAlertRepositoryAlertFactKind {
			return nil, fmt.Errorf("security alert runtime refusing unsupported fact_kind %q", env.FactKind)
		}
		if target.IsOrganizationScope() {
			if name := organizationAlertRepositoryName(alert); name != "" {
				env.Payload["repository_name"] = name
			}
		}
		annotateRepositoryAlertCollectionCoverage(env.Payload, collection.coverageFor(alert))
		envs = append(envs, env)
	}
	return envs, nil
}

// organizationAlertScopeID derives the canonical per-repository security-alert
// scope ID (security-alert:github:<owner>/<repo>) from an organization alert's
// repository object. It returns "" when the repository cannot be resolved.
func organizationAlertScopeID(alert securityalerts.GitHubDependabotAlert) string {
	fullName := normalizeOrganizationAlertRepositoryFullName(alert)
	if fullName == "" {
		return ""
	}
	return "security-alert:github:" + fullName
}

// organizationAlertFullName returns the normalized "owner/repo" full name for
// an organization alert. Used for allowlist filtering.
func organizationAlertFullName(alert securityalerts.GitHubDependabotAlert) string {
	return normalizeOrganizationAlertRepositoryFullName(alert)
}

// organizationAlertRepositoryName returns the short repository name for an
// organization alert, used to populate the repository_name payload field that
// the per-repository path leaves for the reducer to derive.
func organizationAlertRepositoryName(alert securityalerts.GitHubDependabotAlert) string {
	if name := strings.TrimSpace(alert.Repository.Name); name != "" {
		return name
	}
	fullName := normalizeOrganizationAlertRepositoryFullName(alert)
	if slash := strings.LastIndex(fullName, "/"); slash >= 0 && slash+1 < len(fullName) {
		return fullName[slash+1:]
	}
	return ""
}

func normalizeOrganizationAlertRepositoryFullName(alert securityalerts.GitHubDependabotAlert) string {
	fullName := strings.ToLower(strings.Trim(strings.TrimSpace(alert.Repository.FullName), "/"))
	if fullName != "" {
		if strings.Count(fullName, "/") == 1 {
			return fullName
		}
		return ""
	}
	owner := strings.ToLower(strings.TrimSpace(alert.Repository.Owner.Login))
	name := strings.ToLower(strings.TrimSpace(alert.Repository.Name))
	if owner == "" || name == "" {
		return ""
	}
	return owner + "/" + name
}
