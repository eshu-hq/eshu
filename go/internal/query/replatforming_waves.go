// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"sort"
)

// Migration-wave and blast-radius ordering for replatforming plans.
//
// The functions in this file are pure and deterministic: they take already
// composed migration packet items plus the blast-radius signals the plan already
// has (dependency paths and missing-evidence counts that the reducer attached to
// each finding) and produce ordered MigrationWave groups for staged migration and
// BlastRadiusGroup risk groups. Ordering is a planning hint only; it never implies
// automatic apply and never fabricates a dependency the plan does not already
// carry. Identical input always yields identical output regardless of slice order,
// because every grouping iterates a stable, sorted key order rather than a Go map.

// Blast-radius severity buckets. Severity is part of the contract surface that
// clients read to triage a wave's impact, so the bucket names are stable.
const (
	// replatformingBlastSeverityNone marks an item with no recorded downstream
	// dependents and no missing evidence.
	replatformingBlastSeverityNone = "none"
	// replatformingBlastSeverityLow marks an item with a small dependency
	// footprint.
	replatformingBlastSeverityLow = "low"
	// replatformingBlastSeverityMedium marks an item with a moderate dependency
	// footprint.
	replatformingBlastSeverityMedium = "medium"
	// replatformingBlastSeverityHigh marks an item with a large dependency
	// footprint whose change ripples widely.
	replatformingBlastSeverityHigh = "high"
	// replatformingBlastSeverityBlocked marks an item that is safety-gated,
	// rejected, or ambiguously owned. Such an item is grouped explicitly and
	// ordered last regardless of its dependency footprint, because its evidence,
	// not its size, is what blocks it.
	replatformingBlastSeverityBlocked = "blocked"
)

// Wave identifiers. Waves order packet items for staged migration; the IDs are
// stable so clients and docs can reference a wave by name.
const (
	// replatformingWaveEarly is the first wave: import-ready, low-blast-radius,
	// non-gated items that are the safest early migration candidates.
	replatformingWaveEarly = "wave-1-early-safe"
	// replatformingWaveReview is the middle wave: non-gated items that still need
	// review because their import is refused, their evidence is weaker, or their
	// blast radius is larger.
	replatformingWaveReview = "wave-2-review"
	// replatformingWaveBlocked is the final wave: safety-gated, rejected, or
	// ambiguously owned items that must wait for stronger evidence or a human
	// safety decision before any earlier wave can claim them.
	replatformingWaveBlocked = "wave-3-blocked"
)

// Blast-radius group identifiers, one per severity bucket. Items are grouped by
// severity so a wave's impact is explicit before any external apply.
const (
	replatformingBlastGroupNone    = "blast-none"
	replatformingBlastGroupLow     = "blast-low"
	replatformingBlastGroupMedium  = "blast-medium"
	replatformingBlastGroupHigh    = "blast-high"
	replatformingBlastGroupBlocked = "blast-blocked"
)

// replatformingItemSignal carries the blast-radius signals the plan already has
// for one item. Both counts come from reducer-owned evidence the finding already
// records; this file never re-derives dependency or impact truth from the graph.
type replatformingItemSignal struct {
	// DependencyCount is the number of dependency paths recorded for the item's
	// finding. It is a downstream-dependent proxy: more paths means a larger blast
	// radius if the resource is imported, retired, renamed, or moved.
	DependencyCount int
	// MissingEvidenceCount is the number of explicitly missing evidence entries on
	// the item's finding. Missing evidence raises blast-radius uncertainty without
	// fabricating dependents.
	MissingEvidenceCount int
}

// applyReplatformingWaves assigns deterministic migration-wave and blast-radius
// ordering onto a composed plan in place. It populates the plan's Waves and
// BlastRadiusGroups and stamps each item's WaveID and BlastRadiusGroup so a
// consumer can stage migration safely. It is read-only with respect to truth: it
// only orders evidence the plan already carries and never promotes an item past
// its safety gate. The plan stays contract-valid because membership fields are
// additive and never alter required item fields.
func applyReplatformingWaves(plan *ReplatformingPlan, signals map[string]replatformingItemSignal) {
	if plan == nil || len(plan.Items) == 0 {
		return
	}
	waves, groups := assignReplatformingWaves(plan.Items, signals)
	plan.Waves = waves
	plan.BlastRadiusGroups = groups

	waveByItem := membershipByItem(waves)
	groupByItem := blastGroupByItem(groups)
	for i := range plan.Items {
		plan.Items[i].WaveID = waveByItem[plan.Items[i].ItemID]
		plan.Items[i].BlastRadiusGroup = groupByItem[plan.Items[i].ItemID]
	}
}

// assignReplatformingWaves computes the ordered migration waves and blast-radius
// groups for a set of items. It is pure and deterministic: it sorts items into
// classifications, then emits only the non-empty waves and groups in fixed order.
// An empty item set yields no waves and no groups rather than a fabricated wave.
func assignReplatformingWaves(
	items []MigrationPacketItem,
	signals map[string]replatformingItemSignal,
) ([]MigrationWave, []BlastRadiusGroup) {
	if len(items) == 0 {
		return nil, nil
	}

	waveMembers := map[string][]string{}
	groupMembers := map[string][]string{}
	for i := range items {
		item := items[i]
		signal := signals[item.ItemID]
		severity := replatformingBlastSeverity(item, signal)
		waveID := replatformingWaveForSeverity(severity, item)
		groupID := replatformingBlastGroupForSeverity(severity)
		waveMembers[waveID] = append(waveMembers[waveID], item.ItemID)
		groupMembers[groupID] = append(groupMembers[groupID], item.ItemID)
	}

	return buildReplatformingWaves(waveMembers), buildReplatformingBlastGroups(groupMembers)
}

// replatformingBlastSeverity buckets an item into a blast-radius severity. A
// safety-gated, rejected, or ambiguously owned item is always blocked: its
// evidence blocks it regardless of dependency footprint. Otherwise the dependency
// path count plus missing-evidence count drives the bucket.
func replatformingBlastSeverity(item MigrationPacketItem, signal replatformingItemSignal) string {
	if replatformingItemBlocked(item) {
		return replatformingBlastSeverityBlocked
	}
	weight := signal.DependencyCount + signal.MissingEvidenceCount
	switch {
	case weight <= 0:
		return replatformingBlastSeverityNone
	case weight <= 2:
		return replatformingBlastSeverityLow
	case weight <= 5:
		return replatformingBlastSeverityMedium
	default:
		return replatformingBlastSeverityHigh
	}
}

// replatformingItemBlocked reports whether an item must wait for a human safety
// decision or stronger evidence. Rejected source state, an ambiguous owner
// conflict, or a safety gate that requires review all block an item from any
// earlier wave. This is the only place an item is excluded from early staging,
// and it is driven only by evidence the plan already carries.
func replatformingItemBlocked(item MigrationPacketItem) bool {
	switch item.SourceState {
	case ReplatformingSourceStateRejected, ReplatformingSourceStateAmbiguous:
		return true
	}
	return item.SafetyGate.ReviewRequired
}

// replatformingWaveForSeverity maps a severity and item into its staged wave.
// Blocked items go to the final wave. Non-blocked items go to the early wave only
// when they have a ready import candidate and a small blast radius; everything
// else still needs review.
func replatformingWaveForSeverity(severity string, item MigrationPacketItem) string {
	if severity == replatformingBlastSeverityBlocked {
		return replatformingWaveBlocked
	}
	if replatformingImportReady(item) &&
		(severity == replatformingBlastSeverityNone || severity == replatformingBlastSeverityLow) {
		return replatformingWaveEarly
	}
	return replatformingWaveReview
}

// replatformingImportReady reports whether an item carries a ready Terraform
// import candidate. Only such items are early-wave eligible because they have a
// concrete, safety-approved next step.
func replatformingImportReady(item MigrationPacketItem) bool {
	return item.ImportCandidate != nil &&
		item.ImportCandidate.Status == ReplatformingImportStatusReady
}

// replatformingBlastGroupForSeverity maps a severity bucket to its stable group
// identifier.
func replatformingBlastGroupForSeverity(severity string) string {
	switch severity {
	case replatformingBlastSeverityBlocked:
		return replatformingBlastGroupBlocked
	case replatformingBlastSeverityHigh:
		return replatformingBlastGroupHigh
	case replatformingBlastSeverityMedium:
		return replatformingBlastGroupMedium
	case replatformingBlastSeverityLow:
		return replatformingBlastGroupLow
	default:
		return replatformingBlastGroupNone
	}
}

// replatformingWaveOrder is the fixed staging order of the three waves. Earlier
// waves are safer; the blocked wave is always last.
var replatformingWaveOrder = []struct {
	id        string
	rationale string
}{
	{
		id:        replatformingWaveEarly,
		rationale: "import-ready, low blast-radius, non-gated items: safest early migration candidates",
	},
	{
		id:        replatformingWaveReview,
		rationale: "non-gated items needing review: refused import, weaker evidence, or larger blast radius",
	},
	{
		id:        replatformingWaveBlocked,
		rationale: "safety-gated, rejected, or ambiguously owned items: must wait for stronger evidence or a human safety decision",
	},
}

// buildReplatformingWaves emits the non-empty waves in fixed staging order with a
// contiguous 1-based Order, sorted item IDs, and an explicit rationale. Empty
// waves are dropped so a plan never carries a fabricated empty stage.
func buildReplatformingWaves(members map[string][]string) []MigrationWave {
	waves := make([]MigrationWave, 0, len(replatformingWaveOrder))
	order := 0
	for _, spec := range replatformingWaveOrder {
		ids := members[spec.id]
		if len(ids) == 0 {
			continue
		}
		sort.Strings(ids)
		order++
		waves = append(waves, MigrationWave{
			ID:        spec.id,
			Order:     order,
			Rationale: spec.rationale,
			ItemIDs:   ids,
		})
	}
	if len(waves) == 0 {
		return nil
	}
	return waves
}

// replatformingBlastGroupOrder is the fixed, least-to-most severe order of the
// blast-radius groups. The blocked group is always last so a consumer reads risk
// in ascending order.
var replatformingBlastGroupOrder = []struct {
	id       string
	severity string
	reason   string
}{
	{replatformingBlastGroupNone, replatformingBlastSeverityNone, "no recorded downstream dependents or missing evidence"},
	{replatformingBlastGroupLow, replatformingBlastSeverityLow, "small dependency footprint"},
	{replatformingBlastGroupMedium, replatformingBlastSeverityMedium, "moderate dependency footprint"},
	{replatformingBlastGroupHigh, replatformingBlastSeverityHigh, "large dependency footprint; change ripples widely"},
	{replatformingBlastGroupBlocked, replatformingBlastSeverityBlocked, "safety-gated, rejected, or ambiguously owned; blocked regardless of dependency footprint"},
}

// buildReplatformingBlastGroups emits the non-empty blast-radius groups in fixed
// ascending-severity order with sorted item IDs. Empty groups are dropped.
func buildReplatformingBlastGroups(members map[string][]string) []BlastRadiusGroup {
	groups := make([]BlastRadiusGroup, 0, len(replatformingBlastGroupOrder))
	for _, spec := range replatformingBlastGroupOrder {
		ids := members[spec.id]
		if len(ids) == 0 {
			continue
		}
		sort.Strings(ids)
		groups = append(groups, BlastRadiusGroup{
			ID:       spec.id,
			Severity: spec.severity,
			Reason:   spec.reason,
			ItemIDs:  ids,
		})
	}
	if len(groups) == 0 {
		return nil
	}
	return groups
}

// membershipByItem inverts waves into an item-id to wave-id lookup.
func membershipByItem(waves []MigrationWave) map[string]string {
	out := make(map[string]string)
	for _, wave := range waves {
		for _, id := range wave.ItemIDs {
			out[id] = wave.ID
		}
	}
	return out
}

// blastGroupByItem inverts blast-radius groups into an item-id to group-id lookup.
func blastGroupByItem(groups []BlastRadiusGroup) map[string]string {
	out := make(map[string]string)
	for _, group := range groups {
		for _, id := range group.ItemIDs {
			out[id] = group.ID
		}
	}
	return out
}

// replatformingSignalsForFindings builds the per-item blast-radius signal map from
// the same findings the plan composed its items from. The item ID equals the
// finding ID, so each signal is addressable by item without re-deriving impact
// truth. Findings without dependency or missing-evidence evidence yield a
// zero-weight signal, which is the none severity bucket.
func replatformingSignalsForFindings(findings []IaCManagementFindingRow) map[string]replatformingItemSignal {
	if len(findings) == 0 {
		return nil
	}
	signals := make(map[string]replatformingItemSignal, len(findings))
	for _, finding := range findings {
		signals[finding.ID] = replatformingItemSignal{
			DependencyCount:      len(finding.DependencyPaths),
			MissingEvidenceCount: len(finding.MissingEvidence),
		}
	}
	return signals
}

// replatformingWaveSummary is a bounded, deterministic per-wave count summary for
// the plan response. It lets a consumer triage staging without walking every item.
type replatformingWaveSummary struct {
	WaveID    string `json:"wave_id"`
	Order     int    `json:"order"`
	ItemCount int    `json:"item_count"`
}

// replatformingPlanWaveSummaries returns one bounded count summary per populated
// wave in staging order, for the plan response body.
func replatformingPlanWaveSummaries(plan ReplatformingPlan) []replatformingWaveSummary {
	summaries := make([]replatformingWaveSummary, 0, len(plan.Waves))
	for _, wave := range plan.Waves {
		summaries = append(summaries, replatformingWaveSummary{
			WaveID:    wave.ID,
			Order:     wave.Order,
			ItemCount: len(wave.ItemIDs),
		})
	}
	return summaries
}

// replatformingBlastRadiusSummary is a bounded per-group count summary for the
// plan response.
type replatformingBlastRadiusSummary struct {
	GroupID   string `json:"group_id"`
	Severity  string `json:"severity"`
	ItemCount int    `json:"item_count"`
}

// replatformingPlanBlastRadiusSummaries returns one bounded count summary per
// populated blast-radius group in ascending-severity order.
func replatformingPlanBlastRadiusSummaries(plan ReplatformingPlan) []replatformingBlastRadiusSummary {
	summaries := make([]replatformingBlastRadiusSummary, 0, len(plan.BlastRadiusGroups))
	for _, group := range plan.BlastRadiusGroups {
		summaries = append(summaries, replatformingBlastRadiusSummary{
			GroupID:   group.ID,
			Severity:  group.Severity,
			ItemCount: len(group.ItemIDs),
		})
	}
	return summaries
}

// replatformingWavesStorySuffix returns a short, deterministic sentence describing
// the wave/blast-radius shape for the plan story. It is empty when there are no
// waves so an empty plan keeps its existing story.
func replatformingWavesStorySuffix(plan ReplatformingPlan) string {
	if len(plan.Waves) == 0 {
		return ""
	}
	return fmt.Sprintf(
		" Ordered into %d migration wave(s) and %d blast-radius group(s).",
		len(plan.Waves),
		len(plan.BlastRadiusGroups),
	)
}
