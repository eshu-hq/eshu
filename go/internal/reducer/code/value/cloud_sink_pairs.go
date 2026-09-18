// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package value

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// cloudSinkPair is one (function, cloud action) pair that runs in exactly one
// workload, ready to be resolved to cloud sinks by CloudSinkTargetsByPairCypher.
type cloudSinkPair struct {
	FunctionUID string
	Action      string
	WorkloadID  string
}

// cloudSinkPairStats counts the (function, action) pairs selectCloudSinkPairs
// dropped, so an operator can tell "no cloud sinks" apart from "every candidate
// was ambiguous".
type cloudSinkPairStats struct {
	// MultiWorkloadDropped counts pairs whose function runs in more than one
	// identified workload. Attribution to one principal would be a guess.
	MultiWorkloadDropped int
	// UnresolvedDropped counts pairs with an empty action or at least one
	// workload that has no id, which cannot be matched or anchored.
	UnresolvedDropped int
}

// selectCloudSinkPairs groups raw CloudSinkWorkloadRowsCypher rows by
// (function uid, action) and keeps the pairs that run in exactly one distinct
// workload id.
//
// This is the Go-side form of the single-statement filter
// `collect(DISTINCT workload) ... WHERE size(workloads) = 1 ... workloads[0]`,
// which NornicDB v1.3.3 misanswers (#6690). Workloads are compared by id, which
// is their unique identity (workload_id constraint). Duplicate RUNS_IN rows to
// the same workload therefore count once, and a workload with no id makes its
// pair unresolved rather than silently narrowing it to the identified one. The
// result is sorted, so the second statement's batches are deterministic.
func selectCloudSinkPairs(rows []map[string]any) ([]cloudSinkPair, cloudSinkPairStats) {
	type pairKey struct{ uid, action string }
	workloads := map[pairKey]map[string]struct{}{}
	unresolved := map[pairKey]struct{}{}
	for _, row := range rows {
		uid := strings.TrimSpace(payloadcore.AnyToString(row["function_uid"]))
		if uid == "" {
			// Unreachable from LoadCloudSinkTargets: the statement matches
			// fn.uid against the non-empty requested uids. Not a pair, so
			// not counted in the pair stats.
			continue
		}
		// The action and workload id travel to the second statement exactly
		// as the graph returned them, so its equality and IN comparisons
		// match the stored values; trimming is only the emptiness test.
		key := pairKey{uid: uid, action: payloadcore.AnyToString(row["action"])}
		workloadID := payloadcore.AnyToString(row["workload_id"])
		empty := strings.TrimSpace(workloadID) == ""
		if strings.TrimSpace(key.action) == "" || empty {
			unresolved[key] = struct{}{}
		}
		if workloads[key] == nil {
			workloads[key] = map[string]struct{}{}
		}
		if !empty {
			workloads[key][workloadID] = struct{}{}
		}
	}

	var stats cloudSinkPairStats
	pairs := make([]cloudSinkPair, 0, len(workloads))
	for key, ids := range workloads {
		if _, bad := unresolved[key]; bad {
			stats.UnresolvedDropped++
			continue
		}
		if len(ids) != 1 {
			stats.MultiWorkloadDropped++
			continue
		}
		for id := range ids {
			pairs = append(pairs, cloudSinkPair{FunctionUID: key.uid, Action: key.action, WorkloadID: id})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].FunctionUID != pairs[j].FunctionUID {
			return pairs[i].FunctionUID < pairs[j].FunctionUID
		}
		return pairs[i].Action < pairs[j].Action
	})
	return pairs, stats
}

// cloudSinkPairParams binds a batch of pairs as the $pairs list of maps that
// CloudSinkTargetsByPairCypher unwinds.
func cloudSinkPairParams(pairs []cloudSinkPair) []map[string]any {
	out := make([]map[string]any, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, map[string]any{
			"function_uid": pair.FunctionUID,
			"action":       pair.Action,
			"workload_id":  pair.WorkloadID,
		})
	}
	return out
}

// revalidateCloudSinkRows keeps the CloudSinkTargetsByPairCypher rows whose
// (function, action, workload) pair still runs in exactly that one workload,
// and reports how many pairs it dropped.
//
// The two statements run as separate reads, so RUNS_IN can change between
// them: a function can gain a second workload, or one without an id, after the
// first statement classified it as single-workload. The second statement
// therefore returns current_workload_id, every workload the function runs in
// at that moment, on each row. A pair survives only when every such id is
// non-empty and equal to the pair's own workload. That check and the sink
// resolution come from one statement, so they see one snapshot; a function
// that moved away or stopped invoking the action returns no rows at all.
func revalidateCloudSinkRows(rows []map[string]any) ([]map[string]any, int) {
	type pairKey struct{ uid, action, workloadID string }
	key := func(row map[string]any) pairKey {
		return pairKey{
			uid:        strings.TrimSpace(payloadcore.AnyToString(row["function_uid"])),
			action:     payloadcore.AnyToString(row["action"]),
			workloadID: payloadcore.AnyToString(row["workload_id"]),
		}
	}
	stale := map[pairKey]struct{}{}
	for _, row := range rows {
		k := key(row)
		current := payloadcore.AnyToString(row["current_workload_id"])
		if strings.TrimSpace(current) == "" || current != k.workloadID {
			stale[k] = struct{}{}
		}
	}
	kept := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if _, bad := stale[key(row)]; !bad {
			kept = append(kept, row)
		}
	}
	return kept, len(stale)
}
