// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

const (
	// maxOrganizationFanoutRepositories caps the bounded per-repository reads
	// one org claim may issue after a truncated org read (#8805). With the
	// org read itself, one claim sends at most
	// max_pages x (1 + maxOrganizationFanoutRepositories) provider page
	// requests. Allowlisted repositories past the cap are not read; their
	// org-window facts report collectionOrgFanoutCapReachedReason and the
	// fan-out counter records outcome=skipped_cap. The cap is per claim, so
	// the next scheduled claim starts again from the top of the allowlist.
	maxOrganizationFanoutRepositories = 100

	// collectionOrgRepositoryReadFailedReason marks org-window facts of a
	// repository whose per-repository read failed (not found, auth denied,
	// transient provider error).
	collectionOrgRepositoryReadFailedReason = "org_repository_read_failed"
	// collectionOrgRepositoryRateLimitedReason marks org-window facts of a
	// repository the fan-out did not read because the provider rate limited
	// it or an earlier repository read.
	collectionOrgRepositoryRateLimitedReason = "org_repository_read_rate_limited"
	// collectionOrgFanoutCapReachedReason marks org-window facts of a
	// repository past maxOrganizationFanoutRepositories.
	collectionOrgFanoutCapReachedReason = "org_repository_fanout_cap_reached"
)

// collectTarget turns the target's first provider read into the merged
// collection. Only an org read that hit max_pages fans out; every other read
// is returned as is.
func (s *ClaimedSource) collectTarget(
	ctx context.Context,
	observeSpan trace.Span,
	target targetRuntime,
	result securityalerts.GitHubDependabotAlertResult,
) (alertCollection, error) {
	if result.Truncated {
		s.recordTruncatedRead(ctx, target.config, readOperation(target.config))
	}
	if !target.config.IsOrganizationScope() || !result.Truncated {
		return singleReadCollection(result), nil
	}
	return s.fanOutOrganization(ctx, observeSpan, target, result)
}

// fanOutOrganization issues one bounded per-repository read, in allowlist
// order, for each allowlisted repository of the target organization, up to
// maxOrganizationFanoutRepositories. Reads run sequentially on purpose: they
// share one provider rate-limit budget, and the first rate-limited read stops
// the fan-out instead of spending more of it. A repository read that fails for
// any other reason is reported and the fan-out continues. Only a cancelled
// claim context fails the whole claim.
func (s *ClaimedSource) fanOutOrganization(
	ctx context.Context,
	observeSpan trace.Span,
	target targetRuntime,
	org securityalerts.GitHubDependabotAlertResult,
) (alertCollection, error) {
	repositories := organizationFanoutRepositories(target.config)
	orgCoverage := repositoryAlertCollectionCoverageFromResult(org)
	collection := alertCollection{
		observedAt:         org.ObservedAt,
		pagesFetched:       orgCoverage.pagesFetched,
		coverage:           orgCoverage,
		repositoryCoverage: make(map[string]repositoryAlertCollectionCoverage, len(repositories)),
		fanoutOutcomes:     make(map[string]string, len(repositories)),
	}
	reads := make(map[string]securityalerts.GitHubDependabotAlertResult, len(repositories))
	requested, incomplete, rateLimited := 0, 0, false
	for i, repository := range repositories {
		var outcome string
		var coverage repositoryAlertCollectionCoverage
		switch {
		case i >= maxOrganizationFanoutRepositories:
			outcome = contract.SecurityAlertFanoutOutcomeSkippedCap
		case rateLimited:
			outcome = contract.SecurityAlertFanoutOutcomeRateLimited
		default:
			requested++
			result, err := s.fetchFanoutRepository(ctx, target, repository)
			switch {
			case err != nil && ctx.Err() != nil:
				return alertCollection{}, err
			case err != nil && classifiedProviderFailure(err).FailureClass() == FailureRateLimited:
				rateLimited = true
				outcome = contract.SecurityAlertFanoutOutcomeRateLimited
			case err != nil:
				outcome = contract.SecurityAlertFanoutOutcomeFailed
			default:
				reads[repository] = result
				coverage = repositoryAlertCollectionCoverageFromResult(result)
				collection.pagesFetched += coverage.pagesFetched
				outcome = contract.SecurityAlertFanoutOutcomeComplete
				if result.Truncated {
					outcome = contract.SecurityAlertFanoutOutcomeTruncated
					s.recordTruncatedRead(ctx, target.config, dependabotRepositoryAlertsOperation)
				}
			}
		}
		if reason := fanoutOutcomeReason(outcome); reason != "" {
			coverage = orgCoverage
			coverage.extraReasons = []string{reason}
		}
		if outcome != contract.SecurityAlertFanoutOutcomeComplete {
			incomplete++
		}
		collection.repositoryCoverage[repository] = coverage
		collection.fanoutOutcomes[repository] = outcome
		s.recordFanoutOutcome(ctx, target.config, outcome)
	}
	collection.truncated = incomplete > 0
	collection.alerts = mergeFanoutAlerts(target.config, repositories, reads, collection.fanoutOutcomes, org.Alerts)
	if observeSpan != nil {
		observeSpan.SetAttributes(
			attribute.Int(contract.AttrSecurityAlertFanoutRepositories, len(repositories)),
			attribute.Int(contract.AttrSecurityAlertFanoutRequested, requested),
			attribute.Int(contract.AttrSecurityAlertFanoutIncomplete, incomplete),
			attribute.Bool(contract.AttrSecurityAlertFanoutRateLimited, rateLimited),
		)
	}
	return collection, nil
}

// fetchFanoutRepository runs one bounded per-repository read under its own
// fetch span and records it on the shared provider request, duration, and
// rate-limit instruments.
func (s *ClaimedSource) fetchFanoutRepository(
	ctx context.Context,
	target targetRuntime,
	repository string,
) (securityalerts.GitHubDependabotAlertResult, error) {
	startedAt := time.Now()
	fetchCtx, fetchSpan := s.startFetch(ctx)
	defer fetchSpan.End()
	result, err := target.client.ListRepositoryAlertsPages(fetchCtx, repository, target.config.MaxPages)
	if err != nil {
		failure := classifiedProviderFailure(err)
		s.recordFetch(ctx, target.config, failure.FailureClass(), startedAt)
		s.recordRateLimit(ctx, target.config, failure)
		recordSpanError(fetchSpan, failure)
		return securityalerts.GitHubDependabotAlertResult{}, failure
	}
	s.recordFetch(ctx, target.config, "success", startedAt)
	return result, nil
}

// mergeFanoutAlerts merges per-repository reads with the org window. A
// repository's own read wins: its alerts are emitted first, and org-window
// alerts for a repository whose own read was complete are dropped because
// that read is the newer, whole truth for it. Org-window alerts of every other
// allowlisted repository are kept so a failed, rate-limited, or skipped read
// never loses facts the org window already held. Duplicates collapse on
// (repository, alert number), the same identity the stable fact key uses.
func mergeFanoutAlerts(
	target TargetConfig,
	repositories []string,
	reads map[string]securityalerts.GitHubDependabotAlertResult,
	outcomes map[string]string,
	orgAlerts []securityalerts.GitHubDependabotAlert,
) []securityalerts.GitHubDependabotAlert {
	type alertKey struct {
		repository string
		number     int
	}
	seen := map[alertKey]struct{}{}
	merged := make([]securityalerts.GitHubDependabotAlert, 0, len(orgAlerts))
	add := func(repository string, alert securityalerts.GitHubDependabotAlert) {
		key := alertKey{repository: repository, number: alert.Number}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		merged = append(merged, alert)
	}
	for _, repository := range repositories {
		read, ok := reads[repository]
		if !ok {
			continue
		}
		for _, alert := range read.Alerts {
			// The per-repository endpoint omits the repository object, so stamp
			// the requested repository: the envelope then derives the same
			// repository_id, and so the same stable fact key, as the org path.
			alert.Repository = fanoutRepositoryObject(repository)
			add(repository, alert)
		}
	}
	for _, alert := range orgAlerts {
		repository := normalizeOrganizationAlertRepositoryFullName(alert)
		if repository == "" || !target.RepositoryAllowed(repository) {
			continue
		}
		if outcomes[repository] == contract.SecurityAlertFanoutOutcomeComplete {
			continue
		}
		add(repository, alert)
	}
	return merged
}

// organizationFanoutRepositories returns the allowlisted repositories owned by
// the target organization, in allowlist order. An allowlisted repository of
// another owner can never appear in this org's stream, so an org target does
// not read it.
func organizationFanoutRepositories(target TargetConfig) []string {
	prefix := target.Organization + "/"
	repositories := make([]string, 0, len(target.AllowedRepositories))
	for _, repository := range target.AllowedRepositories {
		if strings.HasPrefix(repository, prefix) {
			repositories = append(repositories, repository)
		}
	}
	return repositories
}

func fanoutRepositoryObject(repository string) securityalerts.GitHubDependabotRepository {
	owner, name, _ := strings.Cut(repository, "/")
	return securityalerts.GitHubDependabotRepository{
		FullName: repository,
		Name:     name,
		Owner:    securityalerts.GitHubDependabotRepositoryOwner{Login: owner},
	}
}

// fanoutOutcomeReason returns the incomplete reason for a repository the
// fan-out could not read, or "" when the repository's own read applies.
func fanoutOutcomeReason(outcome string) string {
	switch outcome {
	case contract.SecurityAlertFanoutOutcomeFailed:
		return collectionOrgRepositoryReadFailedReason
	case contract.SecurityAlertFanoutOutcomeRateLimited:
		return collectionOrgRepositoryRateLimitedReason
	case contract.SecurityAlertFanoutOutcomeSkippedCap:
		return collectionOrgFanoutCapReachedReason
	default:
		return ""
	}
}

func readOperation(target TargetConfig) string {
	if target.IsOrganizationScope() {
		return dependabotOrganizationAlertsOperation
	}
	return dependabotRepositoryAlertsOperation
}
