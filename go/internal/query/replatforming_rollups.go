// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
)

// replatformingRollupAmbiguousKey is the explicit group key used when a finding
// carries more than one deterministic service or environment candidate. The
// rollup never guesses a single owner; conflicting ownership is counted here so
// operators can see contested attribution instead of a fabricated owner.
const replatformingRollupAmbiguousKey = "__ambiguous__"

// replatformingRollupUnattributedKey is the explicit group key used when a
// finding carries no service or environment candidate at all. Missing
// attribution is surfaced, not folded into another owner's bucket.
const replatformingRollupUnattributedKey = "__unattributed__"

// Readiness classes for the import-readiness view. They mirror the row-level
// Terraform import-plan outcomes so counts agree with that surface.
const (
	replatformingReadinessImportReady = "import_ready"
	replatformingReadinessNeedsReview = "needs_review"
	replatformingReadinessRefused     = "refused"
)

// replatformingReadinessCounts is the bounded readiness view for one rollup
// bucket. It keeps import-ready items separate from items that need human
// review and items a safety gate refused, so a rollup never presents a refused
// or unproven item as ready.
type replatformingReadinessCounts struct {
	ImportReady int `json:"import_ready"`
	NeedsReview int `json:"needs_review"`
	Refused     int `json:"refused"`
}

// replatformingRollupBucket is one rollup group (an account, environment, or
// service value) with per-source-state counts and the readiness view. Counts
// preserve every per-item source state; unsupported, stale, and unavailable are
// never flattened into a silent "clean" total.
type replatformingRollupBucket struct {
	Key               string                       `json:"key"`
	Total             int                          `json:"total"`
	SourceStateCounts map[string]int               `json:"source_state_counts"`
	Readiness         replatformingReadinessCounts `json:"readiness"`
}

// replatformingRollupResult holds the three grouped dimensions plus the
// account-wide source-state and readiness totals used by the story and
// operator-facing notes.
type replatformingRollupResult struct {
	Account     []replatformingRollupBucket
	Environment []replatformingRollupBucket
	Service     []replatformingRollupBucket
	TotalStates map[string]int
	Readiness   replatformingReadinessCounts
}

// replatformingItemRollup is the per-finding contribution computed once and
// folded into every dimension, so account, environment, and service counts are
// derived from the same resolved source state and readiness class.
type replatformingItemRollup struct {
	sourceState ReplatformingSourceState
	readiness   string
	accountKey  string
	serviceKey  string
	environKey  string
}

// buildReplatformingRollups aggregates bounded IaC management findings into
// account, environment, and service rollups over the provider-neutral
// source-state taxonomy plus an import-readiness view. Aggregation is
// deterministic: each finding contributes exactly once per dimension, buckets
// are returned in stable key order, and ambiguous or missing attribution is
// counted under explicit buckets rather than guessed.
func buildReplatformingRollups(
	findings []IaCManagementFindingRow,
	filter IaCManagementFilter,
) replatformingRollupResult {
	accounts := map[string]*replatformingRollupBucket{}
	environments := map[string]*replatformingRollupBucket{}
	services := map[string]*replatformingRollupBucket{}
	totalStates := newReplatformingStateCounts()
	var totalReadiness replatformingReadinessCounts

	for i := range findings {
		item := replatformingItemForFinding(findings[i], filter)
		addToReplatformingBucket(accounts, item.accountKey, item)
		addToReplatformingBucket(environments, item.environKey, item)
		addToReplatformingBucket(services, item.serviceKey, item)
		totalStates[string(item.sourceState)]++
		accumulateReadiness(&totalReadiness, item.readiness)
	}

	return replatformingRollupResult{
		Account:     sortedReplatformingBuckets(accounts),
		Environment: sortedReplatformingBuckets(environments),
		Service:     sortedReplatformingBuckets(services),
		TotalStates: totalStates,
		Readiness:   totalReadiness,
	}
}

func replatformingItemForFinding(
	finding IaCManagementFindingRow,
	filter IaCManagementFilter,
) replatformingItemRollup {
	state := ResolveReplatformingSourceState(finding.ManagementStatus, finding.SafetyGate.ReviewRequired)
	return replatformingItemRollup{
		sourceState: state,
		readiness:   replatformingReadinessForFinding(finding, filter),
		accountKey:  replatformingAccountKey(finding, filter),
		serviceKey:  replatformingSingleOwnerKey(finding.ServiceCandidates),
		environKey:  replatformingSingleOwnerKey(finding.EnvironmentCandidates),
	}
}

// replatformingReadinessForFinding derives the readiness class by reusing the
// row-level Terraform import-plan classifier, so the rollup's import_ready count
// matches the import-plan surface for the same findings. A safety-gate refusal
// is reported as refused; any other non-ready outcome is needs_review.
func replatformingReadinessForFinding(finding IaCManagementFindingRow, filter IaCManagementFilter) string {
	candidate := terraformImportPlanCandidateForFinding(finding, filter)
	if candidate.Status == "ready" {
		return replatformingReadinessImportReady
	}
	if finding.SafetyGate.ReviewRequired {
		return replatformingReadinessRefused
	}
	return replatformingReadinessNeedsReview
}

// replatformingAccountKey returns the finding's account ID, falling back to the
// filter account, then to the unattributed bucket. It never fabricates an
// account.
func replatformingAccountKey(finding IaCManagementFindingRow, filter IaCManagementFilter) string {
	if id := strings.TrimSpace(finding.AccountID); id != "" {
		return id
	}
	if id := strings.TrimSpace(filter.AccountID); id != "" {
		return id
	}
	scope := terraformImportScopeParts(filter.ScopeID)
	if scope.accountID != "" {
		return scope.accountID
	}
	return replatformingRollupUnattributedKey
}

// replatformingSingleOwnerKey returns the lone deterministic candidate when
// exactly one distinct non-empty value exists. Zero candidates map to the
// unattributed bucket and multiple distinct candidates map to the ambiguous
// bucket; neither case guesses an owner.
func replatformingSingleOwnerKey(candidates []string) string {
	seen := map[string]struct{}{}
	var distinct []string
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		distinct = append(distinct, candidate)
	}
	switch len(distinct) {
	case 0:
		return replatformingRollupUnattributedKey
	case 1:
		return distinct[0]
	default:
		return replatformingRollupAmbiguousKey
	}
}

func addToReplatformingBucket(
	buckets map[string]*replatformingRollupBucket,
	key string,
	item replatformingItemRollup,
) {
	bucket := buckets[key]
	if bucket == nil {
		bucket = &replatformingRollupBucket{
			Key:               key,
			SourceStateCounts: newReplatformingStateCounts(),
		}
		buckets[key] = bucket
	}
	bucket.Total++
	bucket.SourceStateCounts[string(item.sourceState)]++
	accumulateReadiness(&bucket.Readiness, item.readiness)
}

func accumulateReadiness(counts *replatformingReadinessCounts, readiness string) {
	switch readiness {
	case replatformingReadinessImportReady:
		counts.ImportReady++
	case replatformingReadinessRefused:
		counts.Refused++
	default:
		counts.NeedsReview++
	}
}

// newReplatformingStateCounts returns a zeroed map with every taxonomy state
// present, so the response always exposes the full source-state vocabulary and
// an unsupported, stale, or unavailable state never disappears as an absent
// key.
func newReplatformingStateCounts() map[string]int {
	counts := make(map[string]int, len(allReplatformingSourceStates))
	for _, state := range allReplatformingSourceStates {
		counts[string(state)] = 0
	}
	return counts
}

func sortedReplatformingBuckets(buckets map[string]*replatformingRollupBucket) []replatformingRollupBucket {
	out := make([]replatformingRollupBucket, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, *bucket)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out
}
