// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package value

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/exposure"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// GraphCloudSinkTargetLoader reads graph-backed cloud sink edges for
// functions already known to the value-flow fixpoint.
type GraphCloudSinkTargetLoader struct {
	Graph GraphQueryRunner
	// Logger, when set, receives one structured line per load with the
	// candidate, dropped, and resolved counts. Nil disables it.
	Logger *slog.Logger
}

const valueFlowCloudSinkTargetBatchLimit = 500

// LoadCloudSinkTargets converts materialized Function -> CloudAction graph
// edges plus correlated principal permissions into function-level fixpoint
// targets.
//
// It runs two statements with the single-workload check between them in Go
// (#6690): CloudSinkWorkloadRowsCypher reads raw (function, action, workload)
// rows bounded by the durable Function.uid snapshot, selectCloudSinkPairs keeps
// the pairs that run in exactly one workload, and CloudSinkTargetsByPairCypher
// resolves those pairs through the workload's instances and principals to the
// cloud resources they may act on. Both statements run in batches of
// valueFlowCloudSinkTargetBatchLimit.
func (l GraphCloudSinkTargetLoader) LoadCloudSinkTargets(
	ctx context.Context,
	graphIDs map[summary.FunctionID]string,
) ([]CloudSinkTarget, error) {
	if len(graphIDs) == 0 {
		return nil, nil
	}
	if l.Graph == nil {
		return nil, fmt.Errorf("graph value-flow cloud sink loader requires graph query runner")
	}

	functionByUID, functionUIDs := functionIDsByGraphUID(graphIDs)
	if len(functionUIDs) == 0 {
		return nil, nil
	}
	var workloadRows []map[string]any
	for start := 0; start < len(functionUIDs); start += valueFlowCloudSinkTargetBatchLimit {
		end := min(start+valueFlowCloudSinkTargetBatchLimit, len(functionUIDs))
		chunkRows, err := l.Graph.Run(ctx, CloudSinkWorkloadRowsCypher, map[string]any{
			"function_uids": functionUIDs[start:end],
		})
		if err != nil {
			return nil, fmt.Errorf("load value-flow cloud action workloads: %w", err)
		}
		workloadRows = append(workloadRows, chunkRows...)
	}

	pairs, stats := selectCloudSinkPairs(workloadRows)
	var sinkRows []map[string]any
	for start := 0; start < len(pairs); start += valueFlowCloudSinkTargetBatchLimit {
		end := min(start+valueFlowCloudSinkTargetBatchLimit, len(pairs))
		chunkRows, err := l.Graph.Run(ctx, CloudSinkTargetsByPairCypher, map[string]any{
			"pairs": cloudSinkPairParams(pairs[start:end]),
		})
		if err != nil {
			return nil, fmt.Errorf("load graph-backed value-flow cloud sink targets: %w", err)
		}
		sinkRows = append(sinkRows, chunkRows...)
	}
	sinkRows, revalidationDropped := revalidateCloudSinkRows(sinkRows)
	targets := valueFlowCloudSinkTargetsFromRows(sinkRows, functionByUID)
	if l.Logger != nil {
		l.Logger.Info(
			"value-flow cloud sink targets loaded",
			"function_count", len(functionUIDs),
			"workload_row_count", len(workloadRows),
			"single_workload_pair_count", len(pairs),
			"multi_workload_pair_dropped_count", stats.MultiWorkloadDropped,
			"unresolved_pair_dropped_count", stats.UnresolvedDropped,
			"revalidation_pair_dropped_count", revalidationDropped,
			"sink_row_count", len(sinkRows),
			"cloud_sink_target_count", len(targets),
		)
	}
	return targets, nil
}

func functionIDsByGraphUID(graphIDs map[summary.FunctionID]string) (map[string]summary.FunctionID, []string) {
	byUID := make(map[string]summary.FunctionID, len(graphIDs))
	ambiguous := map[string]struct{}{}
	for id, uid := range graphIDs {
		uid = strings.TrimSpace(uid)
		if id == "" || uid == "" {
			continue
		}
		if existing, seen := byUID[uid]; !seen {
			byUID[uid] = id
		} else if existing != id {
			ambiguous[uid] = struct{}{}
		}
	}
	for uid := range ambiguous {
		delete(byUID, uid)
	}
	uids := make([]string, 0, len(byUID))
	for uid := range byUID {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	return byUID, uids
}

func valueFlowCloudSinkTargetsFromRows(
	rows []map[string]any,
	functionByUID map[string]summary.FunctionID,
) []CloudSinkTarget {
	targets := make([]CloudSinkTarget, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		functionID := functionByUID[strings.TrimSpace(payloadcore.AnyToString(row["function_uid"]))]
		if functionID == "" {
			continue
		}
		spec, ok := valueFlowCloudSinkSpecFromRow(row)
		if !ok {
			continue
		}
		key := string(functionID) + "\x00" + string(spec.Kind)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, CloudSinkTarget{
			FunctionID: functionID,
			Kind:       string(spec.Kind),
			Label:      spec.DisplayName,
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].FunctionID != targets[j].FunctionID {
			return targets[i].FunctionID < targets[j].FunctionID
		}
		if targets[i].Kind != targets[j].Kind {
			return targets[i].Kind < targets[j].Kind
		}
		return targets[i].Label < targets[j].Label
	})
	return targets
}

func valueFlowCloudSinkSpecFromRow(row map[string]any) (exposure.SinkSpec, bool) {
	rel := strings.TrimSpace(payloadcore.AnyToString(row["sink_rel"]))
	labels := valueFlowStringSlice(row["sink_labels"])
	props := map[string]string{}
	if value, ok := valueFlowScalarString(row["sink_is_internet"]); ok {
		props["is_internet"] = value
	}
	for _, label := range labels {
		if spec, ok := exposure.MatchSink(rel, label, props); ok {
			return spec, true
		}
	}
	return exposure.SinkSpec{}, false
}

func valueFlowStringSlice(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s := strings.TrimSpace(payloadcore.AnyToString(value)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := strings.TrimSpace(payloadcore.AnyToString(raw)); s != "" {
			return []string{s}
		}
		return nil
	}
}

func valueFlowScalarString(raw any) (string, bool) {
	switch value := raw.(type) {
	case nil:
		return "", false
	case string:
		if strings.TrimSpace(value) == "" {
			return "", false
		}
		return value, true
	case bool:
		return fmt.Sprintf("%t", value), true
	case int, int8, int16, int32, int64, float32, float64:
		return fmt.Sprintf("%v", value), true
	default:
		return "", false
	}
}

// CloudSinkWorkloadRowsCypher reads the raw (function, cloud action, workload)
// rows for a batch of Function.uid values. It deliberately has no aggregation:
// the single-workload check that used to live here as
// collect(DISTINCT workload) / size(workloads) = 1 / workloads[0] returns wrong
// rows on NornicDB v1.3.3 (#6690), so selectCloudSinkPairs does it in Go.
//
// It and CloudSinkTargetsByPairCypher are exported so the backend-conformance
// corpus can pin its read cases to these exact statements by equality. See
// go/internal/backendconformance/corpus_value_flow.go.
const CloudSinkWorkloadRowsCypher = `MATCH (fn:Function)-[:INVOKES_CLOUD_ACTION]->(action:CloudAction)
WHERE fn.uid IN $function_uids
MATCH (fn)-[:RUNS_IN]->(workload:Workload)
RETURN fn.uid AS function_uid,
       action.action AS action,
       workload.id AS workload_id`

// CloudSinkTargetsByPairCypher resolves which cloud resources each
// single-workload (function, action) pair can reach: the workload's instances,
// the principals those instances use, and the resources each principal may
// perform the action on.
//
// It runs as a separate read from CloudSinkWorkloadRowsCypher, so it re-checks
// the pair against the graph as it is now rather than trusting the first read:
// it re-matches the function, its action and the claimed workload, and returns
// current_workload_id, every workload the function runs in at this moment, on
// each row. revalidateCloudSinkRows drops a pair unless all of those are the
// claimed workload. The check is rows, not a subquery or an aggregate: on
// NornicDB v1.3.3 a NOT EXISTS subquery for the same check returned no rows at
// all (#6690). It starts from UNWIND $pairs, anchors on the unique Function.uid
// and Workload.id, and keeps every hop a separate single-hop MATCH.
const CloudSinkTargetsByPairCypher = `UNWIND $pairs AS pair
MATCH (fn:Function {uid: pair.function_uid})-[:INVOKES_CLOUD_ACTION]->(:CloudAction {action: pair.action})
MATCH (fn)-[:RUNS_IN]->(workload:Workload {id: pair.workload_id})
MATCH (fn)-[:RUNS_IN]->(current:Workload)
MATCH (workload)<-[:INSTANCE_OF]-(instance:WorkloadInstance)
MATCH (instance)-[:USES]->(principal:CloudResource)
MATCH (principal)-[sinkRel:CAN_PERFORM]->(sinkNode:CloudResource)
WHERE pair.action IN sinkRel.actions
RETURN pair.function_uid AS function_uid,
       pair.action AS action,
       pair.workload_id AS workload_id,
       current.id AS current_workload_id,
       type(sinkRel) AS sink_rel,
       labels(sinkNode) AS sink_labels,
       sinkNode.is_internet AS sink_is_internet
ORDER BY function_uid, sink_rel`
