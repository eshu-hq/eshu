// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// targetedDiffCase states one fixture's owed partitions and, independently of
// either arm's output, the exact tuples the partition-scoped arm must produce.
type targetedDiffCase struct {
	owed []OwedPartition
	// outcomes is the expected per-owed outcome keyed "scope/generation";
	// nil means every owed partition is published.
	outcomes map[string]TargetedMaintenanceOutcomeKind
	// compared is the expected affected set; the arms must agree on it exactly.
	compared map[scopeGenerationPartition]struct{}
	// newEvidence is "source->target" per evidence row the targeted arm newly
	// writes, sorted.
	newEvidence []string
	// published are the partitions whose phase row the targeted arm writes.
	published map[scopeGenerationPartition]struct{}
	// reopened are the work_item_ids the targeted arm reopens, sorted.
	reopened []string
	// wholeOutside, when non-nil, is the exact sorted set of rows outside the
	// compared partitions that the whole arm changes.
	wholeOutside []string
	// batchSize forces the per-repository batch size in both arms when > 0.
	batchSize int
	// hooks returns per-arm beginner hooks; nil runs both arms unwrapped.
	hooks func(arm string, database *sql.DB) *hookBeginner
	// wantErr, when true, expects both arms to fail and compares the state
	// they leave behind.
	wantErr bool
}

// targetedDiffOutcome is what run observed, for fixtures with extra checks.
type targetedDiffOutcome struct {
	result   TargetedMaintenanceResult
	before   targetedDiffState
	whole    targetedDiffState
	targeted targetedDiffState
	report   targetedDiffReport
}

// run executes both arms from the identical seeded state and asserts the
// differential: in the compared partitions the committed evidence, phase,
// memo and work-item rows are set-equal (0/0 both directions); outside them
// the targeted arm changed nothing; and the targeted arm's writes equal the
// fixture's independently stated tuples.
func (p *targetedDiffPair) run(name string, c targetedDiffCase) targetedDiffOutcome {
	p.t.Helper()
	var outcome targetedDiffOutcome
	outcome.before = p.capture(p.targeted)
	if mismatch := firstSeedMismatch(stripXmin(p.capture(p.whole)), stripXmin(outcome.before)); mismatch != "" {
		p.t.Fatalf("%s: arms were not seeded identically: %s", name, mismatch)
	}

	wholeStore := targetedDiffStore(p.whole, targetedDiffArmsAt)
	targetedStore := targetedDiffStore(p.targeted, targetedDiffArmsAt)
	if c.batchSize > 0 {
		wholeStore.maintenanceBatchSize = c.batchSize
		targetedStore.maintenanceBatchSize = c.batchSize
	}
	if c.hooks != nil {
		if hook := c.hooks("whole", p.whole); hook != nil {
			hook.inner = wholeStore.beginner
			wholeStore.beginner = hook
		}
		if hook := c.hooks("targeted", p.targeted); hook != nil {
			hook.inner = targetedStore.beginner
			targetedStore.beginner = hook
		}
	}
	wholeErr := wholeStore.RunDeferredRelationshipMaintenance(p.ctx, nil, nil)
	result, targetedErr := targetedStore.RunDeferredRelationshipMaintenanceForPartitions(p.ctx, nil, nil, c.owed)
	outcome.result = result
	if c.wantErr {
		if wholeErr == nil || targetedErr == nil {
			p.t.Fatalf("%s: want both arms to fail, whole=%v targeted=%v", name, wholeErr, targetedErr)
		}
	} else if wholeErr != nil || targetedErr != nil {
		p.t.Fatalf("%s: whole=%v targeted=%v", name, wholeErr, targetedErr)
	}
	if !c.wantErr {
		if got := owedSet(result.Affected); !reflect.DeepEqual(got, c.compared) {
			p.t.Fatalf("%s: affected partitions = %v, want %v", name, result.Affected, sortedPartitions(c.compared))
		}
	}
	wantOutcomes := c.outcomes
	if wantOutcomes == nil {
		wantOutcomes = map[string]TargetedMaintenanceOutcomeKind{}
		for _, owed := range c.owed {
			wantOutcomes[owed.ScopeID+"/"+owed.GenerationID] = TargetedMaintenancePublished
		}
	}
	gotOutcomes := map[string]TargetedMaintenanceOutcomeKind{}
	for _, outcome := range result.Outcomes {
		gotOutcomes[outcome.Partition.ScopeID+"/"+outcome.Partition.GenerationID] = outcome.Kind
	}
	if !reflect.DeepEqual(gotOutcomes, wantOutcomes) {
		p.t.Fatalf("%s: per-owed outcomes = %v, want %v", name, gotOutcomes, wantOutcomes)
	}

	outcome.whole = p.capture(p.whole)
	outcome.targeted = p.capture(p.targeted)
	outcome.report = diffStates(outcome.before, outcome.whole, outcome.targeted, c.compared)
	report := outcome.report
	if len(report.wholeOnlyInScope) > 0 || len(report.targetedOnlyInScope) > 0 {
		p.t.Fatalf("%s: compared partitions differ: whole-only=%v targeted-only=%v",
			name, report.wholeOnlyInScope, report.targetedOnlyInScope)
	}
	if len(report.targetedChangedOutside) > 0 {
		p.t.Fatalf("%s: targeted arm changed rows outside the compared partitions: %v", name, report.targetedChangedOutside)
	}
	if c.wholeOutside != nil && !reflect.DeepEqual(report.wholeChangedOutside, c.wholeOutside) {
		p.t.Fatalf("%s: whole arm changed outside = %v, want %v", name, report.wholeChangedOutside, c.wholeOutside)
	}

	if got := p.newEvidenceEdges(p.targeted, changedRows(outcome.before, outcome.targeted, "evidence")); !sameStringMultiset(got, c.newEvidence) {
		p.t.Fatalf("%s: targeted new evidence = %v, want %v", name, got, c.newEvidence)
	}
	gotPublished := map[scopeGenerationPartition]struct{}{}
	for key, row := range outcome.targeted {
		if row.table != "phase" {
			continue
		}
		if prior, ok := outcome.before[key]; !ok || prior.value != row.value {
			gotPublished[row.partition] = struct{}{}
		}
	}
	if !reflect.DeepEqual(gotPublished, nonNilSet(c.published)) {
		p.t.Fatalf("%s: targeted published = %v, want %v", name, sortedPartitions(gotPublished), sortedPartitions(c.published))
	}
	if got := changedRows(outcome.before, outcome.targeted, "work"); !sameStringMultiset(got, c.reopened) {
		p.t.Fatalf("%s: targeted reopened = %v, want %v", name, got, c.reopened)
	}
	assertPhaseCommittedAfterEvidence(p.t, "whole", outcome.before, outcome.whole)
	assertPhaseCommittedAfterEvidence(p.t, "targeted", outcome.before, outcome.targeted)

	summary, _ := json.Marshal(map[string]any{
		"fixture":                  name,
		"compared":                 len(c.compared),
		"whole_only_in_scope":      len(report.wholeOnlyInScope),
		"targeted_only_in_scope":   len(report.targetedOnlyInScope),
		"targeted_changed_outside": len(report.targetedChangedOutside),
		"whole_changed_outside":    report.wholeChangedOutside,
		"targeted_new_evidence":    c.newEvidence,
		"targeted_reopened":        len(c.reopened),
		"loaded":                   result.Loaded,
		"promoted":                 result.Promoted,
		"outcomes":                 gotOutcomes,
	})
	p.t.Logf("DIFFERENTIAL %s", summary)
	return outcome
}

// newEvidenceEdges returns "source->target" for the given evidence ids.
func (p *targetedDiffPair) newEvidenceEdges(database *sql.DB, evidenceIDs []string) []string {
	p.t.Helper()
	if len(evidenceIDs) == 0 {
		return nil
	}
	rows, err := database.QueryContext(p.ctx, `SELECT COALESCE(source_repo_id, '') || '->' || COALESCE(target_repo_id, '')
FROM relationship_evidence_facts WHERE evidence_id = ANY($1)`, array.StringArray(evidenceIDs))
	if err != nil {
		p.t.Fatalf("read new evidence: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var edges []string
	for rows.Next() {
		var edge string
		if err := rows.Scan(&edge); err != nil {
			p.t.Fatalf("scan new evidence: %v", err)
		}
		edges = append(edges, edge)
	}
	sort.Strings(edges)
	return edges
}

// stripXmin drops transaction ids so two arms' seeded states compare by value.
func stripXmin(state targetedDiffState) map[string]string {
	values := make(map[string]string, len(state))
	for key, row := range state {
		values[key] = row.value
	}
	return values
}

// owedPartitions builds a sorted owed list from scope/generation pairs.
func owedPartitions(pairs ...string) []OwedPartition {
	return owedPartitionsOf(sortedPartitions(partitionSet(pairs...)))
}

// owedSet converts exported owed partitions into an internal partition set.
func owedSet(owed []OwedPartition) map[scopeGenerationPartition]struct{} {
	set := make(map[scopeGenerationPartition]struct{}, len(owed))
	for _, partition := range owed {
		set[scopeGenerationPartition(partition)] = struct{}{}
	}
	return set
}

// partitionSetOf converts a partition slice into a set.
func partitionSetOf(partitions []scopeGenerationPartition) map[scopeGenerationPartition]struct{} {
	set := make(map[scopeGenerationPartition]struct{}, len(partitions))
	for _, partition := range partitions {
		set[partition] = struct{}{}
	}
	return set
}

// nonNilSet returns set, or an empty set when nil.
func nonNilSet(set map[scopeGenerationPartition]struct{}) map[scopeGenerationPartition]struct{} {
	if set == nil {
		return map[scopeGenerationPartition]struct{}{}
	}
	return set
}

// sameStringMultiset compares two string lists as sorted multisets, nil == empty.
func sameStringMultiset(got, want []string) bool {
	if len(got) == 0 && len(want) == 0 {
		return true
	}
	a := append([]string(nil), got...)
	b := append([]string(nil), want...)
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

// workIDs expands "<generation>" into the work_item_ids of the named domains,
// or of every reopen domain when domains is empty.
func workIDs(generationID string, domains ...string) []string {
	if len(domains) == 0 {
		domains = targetedDiffReopenDomains()
	}
	ids := make([]string, 0, len(domains))
	for _, domain := range domains {
		ids = append(ids, generationID+"/"+domain)
	}
	return ids
}

// correlationIDs is workIDs for the correlation domains only.
func correlationIDs(generationID string) []string {
	return workIDs(generationID, CrossScopeCorrelationReopenDomains()...)
}

// concatIDs joins id lists.
func concatIDs(lists ...[]string) []string {
	var all []string
	for _, list := range lists {
		all = append(all, list...)
	}
	return all
}

// outsideKeys builds the expected whole-arm outside-change keys from
// "table|key" fragments, sorted.
func outsideKeys(keys ...string) []string {
	out := append([]string(nil), keys...)
	sort.Strings(out)
	return out
}

// phaseKey is the capture key of a backward_evidence phase row.
func phaseKey(scopeID, generationID string) string {
	return "phase|" + strings.Join([]string{
		scopeID, scopeID, generationID, generationID,
		"cross_repo_evidence", "backward_evidence_committed",
	}, "|")
}

// memoKey is the capture key of a memo row.
func memoKey(scopeID, generationID string) string {
	return "memo|" + scopeID + "|" + generationID
}

// workKeys is the capture keys of work items.
func workKeys(ids []string) []string {
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, "work|"+id)
	}
	return keys
}

// firstSeedMismatch names the first row whose value differs between two
// seeded arms, or returns "" when they are identical.
func firstSeedMismatch(whole, targeted map[string]string) string {
	keys := make([]string, 0, len(whole)+len(targeted))
	for key := range whole {
		keys = append(keys, key)
	}
	for key := range targeted {
		if _, ok := whole[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if whole[key] != targeted[key] {
			return key + ": whole=" + whole[key] + " targeted=" + targeted[key]
		}
	}
	return ""
}
