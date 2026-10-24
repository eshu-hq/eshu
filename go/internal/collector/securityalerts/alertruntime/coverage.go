// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
)

// repositoryAlertCollectionCoverage is the provider-read coverage stamped onto
// every fact of one repository. extraReasons carries org fan-out reasons on
// top of the page-limit reason; it is set only on truncated coverage.
type repositoryAlertCollectionCoverage struct {
	pagesFetched int
	truncated    bool
	extraReasons []string
}

// alertCollection is the merged provider read for one claimed target. A
// single read (repository target, or an org read that was not truncated)
// stamps one coverage on every fact. After an org fan-out (#8805),
// repositoryCoverage overrides coverage per normalized repository full name
// and fanoutOutcomes records each fan-out repository outcome for the
// freshness digest.
type alertCollection struct {
	alerts             []securityalerts.GitHubDependabotAlert
	observedAt         time.Time
	pagesFetched       int
	truncated          bool
	coverage           repositoryAlertCollectionCoverage
	repositoryCoverage map[string]repositoryAlertCollectionCoverage
	fanoutOutcomes     map[string]string
}

// singleReadCollection wraps one bounded provider read without fan-out.
func singleReadCollection(result securityalerts.GitHubDependabotAlertResult) alertCollection {
	coverage := repositoryAlertCollectionCoverageFromResult(result)
	return alertCollection{
		alerts:       result.Alerts,
		observedAt:   result.ObservedAt,
		pagesFetched: result.PagesFetched,
		truncated:    result.Truncated,
		coverage:     coverage,
	}
}

// coverageFor returns the coverage that applies to one alert's facts.
func (c alertCollection) coverageFor(alert securityalerts.GitHubDependabotAlert) repositoryAlertCollectionCoverage {
	if c.repositoryCoverage != nil {
		if coverage, ok := c.repositoryCoverage[normalizeOrganizationAlertRepositoryFullName(alert)]; ok {
			return coverage
		}
	}
	return c.coverage
}

// freshnessInput builds the freshness digest input for the merged read.
func (c alertCollection) freshnessInput() securityAlertFreshnessInput {
	return securityAlertFreshnessInput{
		alerts:         c.alerts,
		pagesFetched:   c.pagesFetched,
		truncated:      c.truncated,
		fanoutOutcomes: c.fanoutOutcomes,
	}
}

func repositoryAlertCollectionCoverageFromResult(
	result securityalerts.GitHubDependabotAlertResult,
) repositoryAlertCollectionCoverage {
	pagesFetched := result.PagesFetched
	if pagesFetched <= 0 && len(result.Alerts) > 0 {
		pagesFetched = 1
	}
	return repositoryAlertCollectionCoverage{
		pagesFetched: pagesFetched,
		truncated:    result.Truncated,
	}
}

func annotateRepositoryAlertCollectionCoverage(
	payload map[string]any,
	coverage repositoryAlertCollectionCoverage,
) {
	payload["source_freshness"] = coverage.sourceFreshness()
	payload["collection_coverage_state"] = coverage.coverageState()
	payload["collection_truncated"] = coverage.truncated
	payload["collection_pages_fetched"] = int64(coverage.pagesFetched)
	payload["collection_state_filter"] = collectionOpenStateFilter
	if reasons := coverage.incompleteReasons(); len(reasons) > 0 {
		payload["collection_incomplete_reasons"] = reasons
	}
}

func (c repositoryAlertCollectionCoverage) sourceFreshness() string {
	if c.truncated {
		return securityAlertSourceFreshnessPartial
	}
	return securityAlertSourceFreshnessActive
}

func (c repositoryAlertCollectionCoverage) coverageState() string {
	if c.truncated {
		return collectionCoverageIncomplete
	}
	return collectionCoverageComplete
}

func (c repositoryAlertCollectionCoverage) incompleteReasons() []string {
	if !c.truncated {
		return nil
	}
	reasons := make([]string, 0, 1+len(c.extraReasons))
	reasons = append(reasons, collectionOpenPageLimitReachedReason)
	return append(reasons, c.extraReasons...)
}
