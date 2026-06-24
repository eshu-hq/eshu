// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/exposure"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// GraphValueFlowCloudSinkTargetLoader reads graph-backed cloud sink edges for
// functions already known to the value-flow fixpoint.
type GraphValueFlowCloudSinkTargetLoader struct {
	Graph GraphQueryRunner
}

const valueFlowCloudSinkTargetBatchLimit = 500

// LoadCloudSinkTargets converts materialized Function -> CloudAction graph
// edges plus correlated principal permissions into function-level fixpoint
// targets. The query is bounded by the durable Function.uid snapshot and follows
// the materialized INVOKES_CLOUD_ACTION bridge before matching IAM reachability.
func (l GraphValueFlowCloudSinkTargetLoader) LoadCloudSinkTargets(
	ctx context.Context,
	graphIDs map[summary.FunctionID]string,
) ([]ValueFlowCloudSinkTarget, error) {
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
	var rows []map[string]any
	for start := 0; start < len(functionUIDs); start += valueFlowCloudSinkTargetBatchLimit {
		end := min(start+valueFlowCloudSinkTargetBatchLimit, len(functionUIDs))
		chunkRows, err := l.Graph.Run(ctx, valueFlowCloudSinkTargetsCypher, map[string]any{
			"function_uids": functionUIDs[start:end],
		})
		if err != nil {
			return nil, fmt.Errorf("load graph-backed value-flow cloud sink targets: %w", err)
		}
		rows = append(rows, chunkRows...)
	}
	return valueFlowCloudSinkTargetsFromRows(rows, functionByUID), nil
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
) []ValueFlowCloudSinkTarget {
	targets := make([]ValueFlowCloudSinkTarget, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		functionID := functionByUID[strings.TrimSpace(anyToString(row["function_uid"]))]
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
		targets = append(targets, ValueFlowCloudSinkTarget{
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
	rel := strings.TrimSpace(anyToString(row["sink_rel"]))
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
			if s := strings.TrimSpace(anyToString(value)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := strings.TrimSpace(anyToString(raw)); s != "" {
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

const valueFlowCloudSinkTargetsCypher = `MATCH (fn:Function)-[:INVOKES_CLOUD_ACTION]->(action:CloudAction)
WHERE fn.uid IN $function_uids
MATCH (fn)-[:RUNS_IN]->(workload:Workload)
WITH fn, action, collect(DISTINCT workload) AS workloads
WHERE size(workloads) = 1
WITH fn, action, workloads[0] AS workload
MATCH (workload)<-[:INSTANCE_OF]-(instance:WorkloadInstance)-[:USES]->(principal:CloudResource)
MATCH (principal)-[sinkRel:CAN_PERFORM]->(sinkNode:CloudResource)
WHERE action.action IN sinkRel.actions
RETURN fn.uid AS function_uid,
       type(sinkRel) AS sink_rel,
       labels(sinkNode) AS sink_labels,
       sinkNode.is_internet AS sink_is_internet
ORDER BY function_uid, sink_rel`
