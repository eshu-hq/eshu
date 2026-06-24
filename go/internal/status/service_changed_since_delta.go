// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import "strings"

// Service-scope changed-since (#1943, parent #1797) reuses the repository-scope
// classification, counts, sample, truncation, and unavailable shapes verbatim.
// The only differences are the scope selector (service_id, not an ingestion
// scope/repository), the generation lineage it diffs
// (service_materialization_generations, not scope_generations), and the evidence
// categories it reports. Stage 1 reports the ownership family only.

// ServiceChangedSinceCategories is the closed, ordered set of service evidence
// families this surface computes. Ownership (#1943), deployment (#1985), runtime
// (#1986), dependencies (#1987), docs (#1988), and incidents (#1989) ship; the
// remaining family (vulnerabilities) is a tracked follow-up that appends here as
// it lands. The delta SQL groups by evidence_family, so a new family appears
// automatically once its rows are written and its category is registered here.
var ServiceChangedSinceCategories = []ChangedSinceCategory{
	ChangedSinceCategoryOwnership,
	ChangedSinceCategoryDeployment,
	ChangedSinceCategoryRuntime,
	ChangedSinceCategoryDependencies,
	ChangedSinceCategoryDocs,
	ChangedSinceCategoryIncidents,
	ChangedSinceCategoryVulnerabilities,
}

// ServiceChangedSinceFilter bounds a service-scope changed-since summary to one
// service id and a prior service generation. The prior reference is a service
// generation id (the only stable per-service baseline); unlike the
// repository-scope filter there is no observed-at fallback because service
// generations are produced by re-materialization, not by an external clock the
// caller can name. SampleLimit caps the bounded sample handles per
// classification per category.
type ServiceChangedSinceFilter struct {
	ServiceID         string
	SinceGenerationID string
	SampleLimit       int
}

// Normalize trims selectors and clamps SampleLimit into the supported range,
// reusing the repository-scope default and cap so both surfaces bound samples
// identically.
func (f ServiceChangedSinceFilter) Normalize() ServiceChangedSinceFilter {
	f.ServiceID = strings.TrimSpace(f.ServiceID)
	f.SinceGenerationID = strings.TrimSpace(f.SinceGenerationID)
	if f.SampleLimit <= 0 {
		f.SampleLimit = DefaultChangedSinceSampleLimit
	}
	if f.SampleLimit > MaxChangedSinceSampleLimit {
		f.SampleLimit = MaxChangedSinceSampleLimit
	}
	return f
}

// HasServiceSelector reports whether the filter names a service. A service-scope
// summary always requires one; the handler treats a missing selector as a bad
// request and a named-but-absent service as a not-found.
func (f ServiceChangedSinceFilter) HasServiceSelector() bool {
	return strings.TrimSpace(f.ServiceID) != ""
}

// HasSinceReference reports whether the filter names a prior service generation.
func (f ServiceChangedSinceFilter) HasSinceReference() bool {
	return strings.TrimSpace(f.SinceGenerationID) != ""
}

// ServiceChangedSinceSummary is the bounded service-scope changed-since answer:
// the resolved service, the prior and current service generation the diff
// compared, the per-family deltas, and the sample limit applied. Building and
// Unavailable signal that the current state could not be fully diffed; the
// handler maps them to freshness state and never to a confident empty delta. A
// service that resolved no current active generation is Unavailable, and an
// unknown service id leaves ServiceID empty for an explicit not-found.
type ServiceChangedSinceSummary struct {
	ServiceID                 string                      `json:"service_id"`
	SinceGenerationID         string                      `json:"since_generation_id"`
	SinceObservedAt           string                      `json:"since_observed_at,omitempty"`
	CurrentActiveGenerationID string                      `json:"current_active_generation_id"`
	CurrentObservedAt         string                      `json:"current_observed_at,omitempty"`
	SampleLimit               int                         `json:"sample_limit"`
	Categories                []ChangedSinceCategoryDelta `json:"categories"`
	// Building is true when the service has a pending generation in flight.
	Building bool `json:"building"`
	// Unavailable is true when the diff could not be computed at all (no current
	// active generation, or the since reference resolved to no generation).
	Unavailable bool `json:"unavailable"`
}
