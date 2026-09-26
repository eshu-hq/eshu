// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package changedsince

import "strings"

// Service-scope changed-since (#1943, parent #1797) reuses the repository-scope
// classification, counts, sample, truncation, and unavailable shapes verbatim.
// The only differences are the scope selector (service_id, not an ingestion
// scope/repository), the generation lineage it diffs
// (service_materialization_generations, not scope_generations), and the evidence
// categories it reports. Stage 1 reports the ownership family only.

// ServiceCategories is the closed, ordered set of service evidence
// families this surface computes. Ownership (#1943), deployment (#1985), runtime
// (#1986), dependencies (#1987), docs (#1988), and incidents (#1989) ship; the
// remaining family (vulnerabilities) is a tracked follow-up that appends here as
// it lands. The delta SQL groups by evidence_family, so a new family appears
// automatically once its rows are written and its category is registered here.
var ServiceCategories = []Category{
	CategoryOwnership,
	CategoryDeployment,
	CategoryRuntime,
	CategoryDependencies,
	CategoryDocs,
	CategoryIncidents,
	CategoryVulnerabilities,
}

// MaxServiceScopeCandidates bounds the scope ids a service-scope changed-since
// answer lists when more than one admitted ingestion scope holds a lineage for
// the requested service id (#6475). The reader fetches one more than this to
// report truncation.
const MaxServiceScopeCandidates = 20

// ServiceFilter bounds a service-scope changed-since summary to one
// service id and a prior service generation. The prior reference is a service
// generation id (the only stable per-service baseline); unlike the
// repository-scope filter there is no observed-at fallback because service
// generations are produced by re-materialization, not by an external clock the
// caller can name. SampleLimit caps the bounded sample handles per
// classification per category.
//
// Since #6475 a service id can hold one lineage per ingestion scope, because a
// catalog service id is catalog-relative and two tenants may both declare it.
// ScopeID optionally selects one of those lineages. Scoped, AllowedRepositoryIDs
// and AllowedScopeIDs carry the caller's grant; the reader binds it in SQL on
// the lineage row's scope_id, so an ungranted lineage (and every unattributed
// legacy lineage, whose scope_id is NULL) resolves to no row for a scoped
// caller. An explicit ScopeID is itself subject to the grant: selecting an
// ungranted scope is indistinguishable from selecting one that holds nothing.
type ServiceFilter struct {
	ServiceID         string
	ScopeID           string
	SinceGenerationID string
	SampleLimit       int
	// Scoped reports a scoped grant rather than the shared key. When false the
	// grant arrays are ignored and every lineage, attributed or not, is visible.
	Scoped               bool
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// Normalize trims selectors and clamps SampleLimit into the supported range,
// reusing the repository-scope default and cap so both surfaces bound samples
// identically.
func (f ServiceFilter) Normalize() ServiceFilter {
	f.ServiceID = strings.TrimSpace(f.ServiceID)
	f.ScopeID = strings.TrimSpace(f.ScopeID)
	f.SinceGenerationID = strings.TrimSpace(f.SinceGenerationID)
	if f.SampleLimit <= 0 {
		f.SampleLimit = DefaultSampleLimit
	}
	if f.SampleLimit > MaxSampleLimit {
		f.SampleLimit = MaxSampleLimit
	}
	return f
}

// HasServiceSelector reports whether the filter names a service. A service-scope
// summary always requires one; the handler treats a missing selector as a bad
// request and a named-but-absent service as a not-found.
func (f ServiceFilter) HasServiceSelector() bool {
	return strings.TrimSpace(f.ServiceID) != ""
}

// HasSinceReference reports whether the filter names a prior service generation.
func (f ServiceFilter) HasSinceReference() bool {
	return strings.TrimSpace(f.SinceGenerationID) != ""
}

// ServiceSummary is the bounded service-scope changed-since answer:
// the resolved service, the prior and current service generation the diff
// compared, the per-family deltas, and the sample limit applied. Building and
// Unavailable signal that the current state could not be fully diffed; the
// handler maps them to freshness state and never to a confident empty delta. A
// service that resolved no current active generation is Unavailable, and an
// unknown service id leaves ServiceID empty for an explicit not-found.
//
// When more than one admitted ingestion scope holds a lineage for the service
// id and the filter named no ScopeID, the reader does not pick one: it returns
// ServiceID set, AmbiguousScopeIDs listing the admitted scope ids (sorted,
// bounded by MaxServiceScopeCandidates) and no diff. The handler answers that
// with a conflict so the caller re-asks with a scope selector.
type ServiceSummary struct {
	ServiceID string `json:"service_id"`
	// ScopeID is the ingestion scope of the lineage the diff read. It is empty
	// for an unattributed legacy lineage (Unattributed=true), which only an
	// unscoped caller can resolve.
	ScopeID                   string          `json:"scope_id,omitempty"`
	Unattributed              bool            `json:"unattributed,omitempty"`
	SinceGenerationID         string          `json:"since_generation_id"`
	SinceObservedAt           string          `json:"since_observed_at,omitempty"`
	CurrentActiveGenerationID string          `json:"current_active_generation_id"`
	CurrentObservedAt         string          `json:"current_observed_at,omitempty"`
	SampleLimit               int             `json:"sample_limit"`
	Categories                []CategoryDelta `json:"categories"`
	// Building is true when the service has a pending generation in flight.
	Building bool `json:"building"`
	// Unavailable is true when the diff could not be computed at all (no current
	// active generation, or the since reference resolved to no generation).
	Unavailable bool `json:"unavailable"`
	// AmbiguousScopeIDs lists the admitted ingestion scopes that each hold a
	// lineage for the service when no ScopeID selected one. Non-empty means the
	// diff was not computed. It never names a scope outside the caller's grant.
	AmbiguousScopeIDs []string `json:"ambiguous_scope_ids,omitempty"`
	// AmbiguousTruncated reports that more admitted scopes exist than
	// AmbiguousScopeIDs lists.
	AmbiguousTruncated bool `json:"ambiguous_truncated,omitempty"`
	// OutsideGrant is server-side telemetry only: the service id holds lineage
	// rows, but none the caller's grant admits. The handler answers it exactly
	// like an unknown service and records it on the span, never in the body.
	OutsideGrant bool `json:"-"`
}

// Ambiguous reports whether the answer lists candidate scopes instead of a
// diff.
func (s ServiceSummary) Ambiguous() bool {
	return len(s.AmbiguousScopeIDs) > 0
}
