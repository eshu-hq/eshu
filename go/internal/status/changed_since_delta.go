// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"strings"
	"time"
)

// MaxChangedSinceSampleLimit is the hard upper bound on bounded sample handles a
// single changed-since summary returns per classification, per evidence
// category. Callers that ask for more are clamped so a broad diff cannot return
// an unbounded sample payload. The aggregate counts are always exact; only the
// sample list is capped.
const MaxChangedSinceSampleLimit = 200

// DefaultChangedSinceSampleLimit is the per-classification sample size used when
// a caller does not specify a limit.
const DefaultChangedSinceSampleLimit = 25

// ChangedSinceClassification is the delta verdict for one stable fact key when
// the prior generation's fact set is diffed against the current active
// generation's fact set. The set is closed and never collapses retired into
// unchanged or hides unavailable behind a summary.
type ChangedSinceClassification string

const (
	// ChangedSinceAdded marks a stable fact key present in the current active
	// generation but absent from the prior generation.
	ChangedSinceAdded ChangedSinceClassification = "added"
	// ChangedSinceUpdated marks a stable fact key present in both generations
	// whose payload hash differs.
	ChangedSinceUpdated ChangedSinceClassification = "updated"
	// ChangedSinceUnchanged marks a stable fact key present in both generations
	// with an identical payload hash.
	ChangedSinceUnchanged ChangedSinceClassification = "unchanged"
	// ChangedSinceRetired marks a stable fact key that was active in the prior
	// generation and carries an explicit tombstone in the current generation.
	ChangedSinceRetired ChangedSinceClassification = "retired"
	// ChangedSinceSuperseded marks a stable fact key that was active in the prior
	// generation and is absent entirely from the current generation (dropped on
	// generation rollover rather than explicitly tombstoned).
	ChangedSinceSuperseded ChangedSinceClassification = "superseded"
)

// ChangedSinceClassifications is the closed, ordered set of delta verdicts. The
// store and any projection iterate it so a new verdict cannot ship without a
// deterministic position.
var ChangedSinceClassifications = []ChangedSinceClassification{
	ChangedSinceAdded,
	ChangedSinceUpdated,
	ChangedSinceUnchanged,
	ChangedSinceRetired,
	ChangedSinceSuperseded,
}

// ChangedSinceCategory groups fact kinds into the evidence categories a
// changed-since summary reports. The repository-scope slice covers files,
// content entities, and the remaining facts; service-scope categories are
// tracked separately and are not computed by this surface yet.
type ChangedSinceCategory string

const (
	// ChangedSinceCategoryFiles covers file facts (fact_kind = 'file').
	ChangedSinceCategoryFiles ChangedSinceCategory = "files"
	// ChangedSinceCategoryContentEntities covers content entity facts
	// (fact_kind = 'content_entity').
	ChangedSinceCategoryContentEntities ChangedSinceCategory = "content_entities"
	// ChangedSinceCategoryFacts covers every remaining fact kind.
	ChangedSinceCategoryFacts ChangedSinceCategory = "facts"
	// ChangedSinceCategoryOwnership covers service-scope ownership evidence rows
	// (#1943). It is computed by the service-scope changed-since surface, not the
	// repository-scope surface, so it never appears in ChangedSinceCategories.
	ChangedSinceCategoryOwnership ChangedSinceCategory = "ownership"
	// ChangedSinceCategoryDeployment covers service-scope deployment evidence rows
	// (#1985): one row per resolved deployment relationship for the service's
	// repository. Like ownership it is a service-scope category, computed by the
	// service-scope changed-since surface, so it never appears in
	// ChangedSinceCategories.
	ChangedSinceCategoryDeployment ChangedSinceCategory = "deployment"
	// ChangedSinceCategoryRuntime covers service-scope runtime evidence rows
	// (#1986): one row per materialized runtime instance of the service's workload,
	// keyed by the durable platform/environment/workload identity. Like ownership
	// and deployment it is a service-scope category, computed by the service-scope
	// changed-since surface, so it never appears in ChangedSinceCategories.
	ChangedSinceCategoryRuntime ChangedSinceCategory = "runtime"
	// ChangedSinceCategoryDependencies covers service-scope dependency evidence rows
	// (#1987): one row per resolved dependency relationship for the service's
	// repository (DEPENDS_ON / USES_MODULE / READS_CONFIG_FROM), keyed by the
	// relationship's generation-independent natural key. Like ownership, deployment,
	// and runtime it is a service-scope category, computed by the service-scope
	// changed-since surface, so it never appears in ChangedSinceCategories.
	ChangedSinceCategoryDependencies ChangedSinceCategory = "dependencies"
	// ChangedSinceCategoryDocs covers service-scope docs evidence rows (#1988): one
	// row per documentation fact that references the service (documentation entity
	// mention, documentation claim candidate, or semantic documentation
	// observation), keyed by the fact's durable external identity. Like ownership,
	// deployment, runtime, and dependencies it is a service-scope category,
	// computed by the service-scope changed-since surface, so it never appears in
	// ChangedSinceCategories.
	ChangedSinceCategoryDocs ChangedSinceCategory = "docs"
	// ChangedSinceCategoryIncidents covers service-scope incidents evidence rows
	// (#1989): one row per exact PagerDuty incident-routing evidence row that routes
	// to the service (one per routing slot: intended / applied / live), keyed by the
	// row's durable routing identity (provider, provider_incident_id, slot,
	// evidence_kind, and the generation-independent evidence id). Like ownership,
	// deployment, runtime, dependencies, and docs it is a service-scope category,
	// computed by the service-scope changed-since surface, so it never appears in
	// ChangedSinceCategories.
	ChangedSinceCategoryIncidents ChangedSinceCategory = "incidents"
	// ChangedSinceCategoryVulnerabilities covers service-scope vulnerabilities
	// evidence rows (#1990): one row per (supply-chain advisory, affected package)
	// pair affecting a package the service depends on, keyed by the advisory's
	// durable canonical id and the affected package ecosystem/name. Like ownership,
	// deployment, runtime, dependencies, docs, and incidents it is a service-scope
	// category, computed by the service-scope changed-since surface, so it never
	// appears in ChangedSinceCategories.
	ChangedSinceCategoryVulnerabilities ChangedSinceCategory = "vulnerabilities"
)

// ChangedSinceCategories is the closed, ordered set of evidence categories this
// surface computes. Iterating it keeps category output deterministic.
var ChangedSinceCategories = []ChangedSinceCategory{
	ChangedSinceCategoryFiles,
	ChangedSinceCategoryContentEntities,
	ChangedSinceCategoryFacts,
}

// ChangedSinceFilter bounds a changed-since summary to one repository-kind scope
// and a prior reference (a generation id or an observed-at instant). Exactly one
// scope selector (ScopeID or Repository) and one prior reference
// (SinceGenerationID or SinceObservedAt) are required; the handler enforces that
// before the read. SampleLimit caps the bounded sample handles returned per
// classification per category.
type ChangedSinceFilter struct {
	ScopeID           string
	Repository        string
	SinceGenerationID string
	SinceObservedAt   time.Time
	SampleLimit       int
}

// Normalize trims selector whitespace and clamps SampleLimit into the supported
// range. A zero or negative SampleLimit becomes DefaultChangedSinceSampleLimit
// and a value above MaxChangedSinceSampleLimit is clamped to the cap.
func (f ChangedSinceFilter) Normalize() ChangedSinceFilter {
	f.ScopeID = strings.TrimSpace(f.ScopeID)
	f.Repository = strings.TrimSpace(f.Repository)
	f.SinceGenerationID = strings.TrimSpace(f.SinceGenerationID)
	if f.SampleLimit <= 0 {
		f.SampleLimit = DefaultChangedSinceSampleLimit
	}
	if f.SampleLimit > MaxChangedSinceSampleLimit {
		f.SampleLimit = MaxChangedSinceSampleLimit
	}
	return f
}

// HasScopeSelector reports whether the filter names a scope or repository. A
// changed-since summary always requires one; the handler treats a missing
// selector as a bad request and a named-but-absent scope as a not-found.
func (f ChangedSinceFilter) HasScopeSelector() bool {
	return strings.TrimSpace(f.ScopeID) != "" || strings.TrimSpace(f.Repository) != ""
}

// HasConflictingScopeSelectors reports whether both mutually exclusive scope
// selectors are present. Callers must fail closed rather than intersecting the
// two values because that can retain evidence from an obsolete UI scope.
func (f ChangedSinceFilter) HasConflictingScopeSelectors() bool {
	return strings.TrimSpace(f.ScopeID) != "" && strings.TrimSpace(f.Repository) != ""
}

// HasSinceReference reports whether the filter names a prior generation or a
// prior observed-at instant. A changed-since summary always requires one.
func (f ChangedSinceFilter) HasSinceReference() bool {
	return strings.TrimSpace(f.SinceGenerationID) != "" || !f.SinceObservedAt.IsZero()
}

// ChangedSinceCounts holds the exact per-classification key counts for one
// evidence category. Counts are computed by aggregate over fact_records and are
// never truncated; only the sample handles are bounded.
type ChangedSinceCounts struct {
	Added      int `json:"added"`
	Updated    int `json:"updated"`
	Unchanged  int `json:"unchanged"`
	Retired    int `json:"retired"`
	Superseded int `json:"superseded"`
}

// Total returns the sum of every classification count in the category. It is the
// total number of distinct stable fact keys considered across both generations.
func (c ChangedSinceCounts) Total() int {
	return c.Added + c.Updated + c.Unchanged + c.Retired + c.Superseded
}

// ChangedSinceSample is one bounded, ordered sample handle: a stable fact key
// that fell into a classification, with its fact kind so a caller can drill into
// the underlying fact. Samples are deterministic (ordered by stable_fact_key)
// and capped at SampleLimit per classification.
type ChangedSinceSample struct {
	StableFactKey string `json:"stable_fact_key"`
	FactKind      string `json:"fact_kind"`
}

// ChangedSinceCategoryDelta is the per-category delta: exact counts, bounded
// sample handles keyed by classification, a per-classification truncation flag,
// and an Unavailable flag that is set when the category could not be diffed (for
// example the current active generation is missing or still pending), so an
// unavailable category is never silently reported as all-unchanged.
type ChangedSinceCategoryDelta struct {
	Category    ChangedSinceCategory                                `json:"category"`
	Counts      ChangedSinceCounts                                  `json:"counts"`
	Samples     map[ChangedSinceClassification][]ChangedSinceSample `json:"samples,omitempty"`
	Truncated   map[ChangedSinceClassification]bool                 `json:"truncated,omitempty"`
	Unavailable bool                                                `json:"unavailable"`
}

// ChangedSinceUnavailableReason explains why a changed-since summary could not
// be computed. The set is intentionally small and JSON-facing so callers can
// distinguish retention-expired history from a still-building active snapshot.
type ChangedSinceUnavailableReason string

const (
	// ChangedSinceUnavailableRetentionExpired means the requested prior
	// generation was once known for the scope but was pruned by the configured
	// generation retention policy.
	ChangedSinceUnavailableRetentionExpired ChangedSinceUnavailableReason = "retention_expired"
)

// ChangedSinceSummary is the bounded changed-since answer for one repository
// scope: the resolved scope identity, the prior and current generation the diff
// compared, the per-category deltas, and the sample limit applied. Building and
// Unavailable signal that the current state could not be fully diffed; the
// handler maps them to freshness state and never to a confident empty delta.
type ChangedSinceSummary struct {
	ScopeID                   string                      `json:"scope_id"`
	ScopeKind                 string                      `json:"scope_kind"`
	Repository                string                      `json:"repository,omitempty"`
	SinceGenerationID         string                      `json:"since_generation_id"`
	SinceObservedAt           string                      `json:"since_observed_at,omitempty"`
	CurrentActiveGenerationID string                      `json:"current_active_generation_id"`
	CurrentObservedAt         string                      `json:"current_observed_at,omitempty"`
	SampleLimit               int                         `json:"sample_limit"`
	Categories                []ChangedSinceCategoryDelta `json:"categories"`
	// Building is true when the scope has a pending generation in flight (the
	// current active generation may be about to change). It maps to a building
	// freshness state, not to a wrong delta.
	Building bool `json:"building"`
	// Unavailable is true when the diff could not be computed at all (no current
	// active generation, or the since reference resolved to no generation). The
	// handler treats an unavailable summary as an explicit not-found or
	// unavailable freshness, never as zero deltas.
	Unavailable bool `json:"unavailable"`
	// UnavailableReason carries the fail-closed reason when Unavailable is true
	// and the storage layer can classify the failure.
	UnavailableReason ChangedSinceUnavailableReason `json:"unavailable_reason,omitempty"`
}

// ChangedSinceTimestamp formats a database timestamp as RFC3339 UTC, or the
// empty string when the value is zero. It centralizes the timestamp shape the
// changed-since contract promises so the storage reader and any projection stay
// in lockstep with the generation lifecycle drilldown.
func ChangedSinceTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
