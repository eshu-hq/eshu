// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
)

// GroupedInventoryStore reads the service, workload, and environment grouped
// inventory over reducer-owned vulnerability impact findings (#9873). It is a
// separate port from AggregateStore on purpose: the five single-valued
// dimensions group one fact column, while these three resolve a finding to its
// runtime service, workload, or environment at read time through the same
// runtime-filter join and grant predicate the service_id, workload_id, and
// environment filters use. A bucket under group_by=service_id therefore holds
// exactly the findings the service_id=X filter returns.
//
// An implementation MUST:
//   - apply filter.AllowedRepositoryIDs and filter.AllowedScopeIDs to the
//     finding set before grouping, so a service reachable only through an
//     ungranted repository never appears. Both empty means unrestricted, so
//     the handler never calls the store for a scoped caller with no grants
//     (it answers the empty page itself); an implementation must not be
//     reached with an empty grant;
//   - report a bucket whose findings a scoped grant partly hides as
//     GroupCompletenessPartial;
//   - emit at most one unattributed bucket (Value "", Unattributed true) for
//     findings with no resolved group, never merged into a named bucket;
//   - return a finding in every group it resolves to, so bucket counts summed
//     over every page may exceed DistinctFindings;
//   - report DistinctFindings over the whole filtered finding set, never over
//     the rows of the requested page, so limit and offset (and a truncated
//     page) never change it;
//   - order rows by MaxPriorityScore descending, then Count descending, then
//     Value ascending, and honour limit and offset, where the handler asks for
//     one extra row to detect truncation (mirrors
//     AggregateStore.SupplyChainImpactInventory);
//   - answer ErrGroupedInventoryReadModelUnavailable rather than fall back to
//     another read shape when its read model is not enabled.
type GroupedInventoryStore interface {
	SupplyChainImpactGroupedInventory(
		ctx context.Context,
		filter AggregateFilter,
		dimension GroupedInventoryDimension,
		limit int,
		offset int,
	) (GroupedInventoryPage, error)
}

// ErrGroupedInventoryReadModelUnavailable is the sentinel a GroupedInventoryStore
// returns when the read model it needs is not enabled for this deployment (for
// example the canonical-winners read gate is off). The handler answers the
// existing 503 backend_unavailable envelope for it; there is no silent fallback
// to a different read shape.
var ErrGroupedInventoryReadModelUnavailable = errors.New(
	"supply chain impact grouped inventory read model unavailable",
)

// GroupedInventoryDimension names one grouped-inventory dimension served by
// GroupedInventoryStore. Its closed set is disjoint from InventoryDimension.
type GroupedInventoryDimension string

const (
	// GroupedInventoryByService groups findings by the resolved service id.
	GroupedInventoryByService GroupedInventoryDimension = "service_id"
	// GroupedInventoryByWorkload groups findings by the resolved workload id,
	// unioning every workload fact kind the workload_id filter unions.
	GroupedInventoryByWorkload GroupedInventoryDimension = "workload_id"
	// GroupedInventoryByEnvironment groups findings by the resolved
	// deployment environment. Its unattributed bucket means no current
	// deploy evidence, a different gap from an unattributed service.
	GroupedInventoryByEnvironment GroupedInventoryDimension = "environment"
)

// ParseGroupedInventoryDimension maps a raw group_by value to a grouped
// dimension. It reports false for every value outside the closed set,
// including the five InventoryDimension values.
func ParseGroupedInventoryDimension(raw string) (GroupedInventoryDimension, bool) {
	switch dimension := GroupedInventoryDimension(raw); dimension {
	case GroupedInventoryByService, GroupedInventoryByWorkload, GroupedInventoryByEnvironment:
		return dimension, true
	default:
		return "", false
	}
}

// GroupCompleteness is the per-bucket completeness verdict of a grouped
// inventory row. It is deliberately not named readiness_state, to avoid
// confusion with the six-state finding readiness envelope.
type GroupCompleteness string

const (
	// GroupCompletenessComplete means the bucket holds every finding that
	// resolves to its value under the caller's grants.
	GroupCompletenessComplete GroupCompleteness = "complete"
	// GroupCompletenessPartial means the bucket may be missing findings: the
	// page was truncated, or a scoped grant does not admit some finding mapped
	// to the bucket (decided independent of the request filters, so it is no
	// oracle on hidden findings; counts never include those findings).
	GroupCompletenessPartial GroupCompleteness = "partial"
	// GroupCompletenessUnattributed marks the bucket of findings with no
	// resolved group. It is never complete.
	GroupCompletenessUnattributed GroupCompleteness = "unattributed"
	// GroupCompletenessReadModelUnavailable means the evidence needed to
	// resolve the bucket is not readable right now.
	GroupCompletenessReadModelUnavailable GroupCompleteness = "read_model_unavailable"
)

// Valid reports whether c is one of the four closed completeness values.
func (c GroupCompleteness) Valid() bool {
	switch c {
	case GroupCompletenessComplete,
		GroupCompletenessPartial,
		GroupCompletenessUnattributed,
		GroupCompletenessReadModelUnavailable:
		return true
	default:
		return false
	}
}

// Missing-evidence reasons a grouped bucket can carry. The unattributed bucket
// always carries the reason matching its dimension.
const (
	// GroupedMissingEvidenceServiceMapping marks findings whose repository
	// resolves to no service.
	GroupedMissingEvidenceServiceMapping = "service_mapping"
	// GroupedMissingEvidenceWorkloadMapping marks findings whose repository
	// resolves to no workload.
	GroupedMissingEvidenceWorkloadMapping = "workload_mapping"
	// GroupedMissingEvidenceDeployEvidence marks findings with no current
	// deploy evidence naming an environment.
	GroupedMissingEvidenceDeployEvidence = "current_deploy_evidence"
)

// UnattributedMissingEvidence returns the missing-evidence reason an
// unattributed bucket of dimension carries.
func UnattributedMissingEvidence(dimension GroupedInventoryDimension) string {
	switch dimension {
	case GroupedInventoryByService:
		return GroupedMissingEvidenceServiceMapping
	case GroupedInventoryByWorkload:
		return GroupedMissingEvidenceWorkloadMapping
	default:
		return GroupedMissingEvidenceDeployEvidence
	}
}

// GroupedInventoryRow is one grouped bucket. The unattributed bucket has Value
// "" and Unattributed true; every named bucket has a non-empty Value.
type GroupedInventoryRow struct {
	Dimension        GroupedInventoryDimension `json:"dimension"`
	Value            string                    `json:"value"`
	Unattributed     bool                      `json:"unattributed"`
	Count            int                       `json:"count"`
	ByImpactStatus   map[string]int            `json:"by_impact_status"`
	BySeverity       map[string]int            `json:"by_severity"`
	MaxPriorityScore int                       `json:"max_priority_score"`
	Completeness     GroupCompleteness         `json:"completeness"`
	MissingEvidence  []string                  `json:"missing_evidence"`
}

// GroupedInventoryFreshness reports how fresh the evidence behind a grouped
// page is, using the readiness freshness labels (FreshnessLabelFresh,
// FreshnessLabelStale, FreshnessLabelUnknown).
type GroupedInventoryFreshness struct {
	State            string `json:"state"`
	LatestObservedAt string `json:"latest_observed_at,omitempty"`
}

// GroupedInventoryPage is one page of grouped buckets, ordered by
// MaxPriorityScore descending, then Count descending, then Value ascending.
// DistinctFindings counts each finding once across the whole filtered set,
// independent of limit and offset, so it normally equals the total_findings
// of the count route for the same filters on every page. The winners-sourced
// store can differ under a scope-id grant, while the winners are stale, and
// when copies of one finding differ on a field outside its identity (see
// PostgresGroupedInventoryStore). Because a finding counts in
// every group it resolves to, bucket counts summed over every page are at
// least DistinctFindings; a single page may sum to less.
type GroupedInventoryPage struct {
	Rows             []GroupedInventoryRow
	DistinctFindings int
	Freshness        GroupedInventoryFreshness
}
