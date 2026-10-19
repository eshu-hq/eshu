// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
)

// gaugeWorkflowFairnessKeyDepth is the `gauge` label of the snapshot that
// feeds eshu_dp_workflow_fairness_key_depth and
// eshu_dp_workflow_fairness_key_count (#8469).
const gaugeWorkflowFairnessKeyDepth = "reducer_workflow_fairness_key_depth"

// fairness snapshot key prefixes discriminate the two series one refresh
// fetch feeds: per-key pending depth and per-family distinct-key counts.
const (
	fairnessDepthPrefix    = "fairdepth"
	fairnessDistinctPrefix = "fairdistinct"
)

// flattenFairnessDepths encodes one fairness snapshot into flat keys. Depth
// series carry kind, instance, and key; distinct-count series carry kind
// and instance under their own prefix so the two never collide.
func flattenFairnessDepths(nested map[string]map[string]telemetry.WorkflowFairnessFamilyDepth) map[string]int64 {
	flat := make(map[string]int64, len(nested))
	for kind, instances := range nested {
		for instance, family := range instances {
			flat[fairnessDistinctPrefix+snapshotKeySep+kind+snapshotKeySep+instance] = family.DistinctKeys
			for _, key := range family.Top {
				flat[fairnessDepthPrefix+snapshotKeySep+kind+snapshotKeySep+instance+snapshotKeySep+key.Key] = key.Pending
			}
		}
	}
	return flat
}

// unflattenFairnessDepths decodes a fairness snapshot back into the observer
// shape. Unknown prefixes are dropped so a future series cannot corrupt the
// served gauges.
func unflattenFairnessDepths(flat map[string]int64) map[string]map[string]telemetry.WorkflowFairnessFamilyDepth {
	nested := make(map[string]map[string]telemetry.WorkflowFairnessFamilyDepth)
	for key, count := range flat {
		prefix, rest, _ := strings.Cut(key, snapshotKeySep)
		switch prefix {
		case fairnessDistinctPrefix:
			kind, instance, _ := strings.Cut(rest, snapshotKeySep)
			instances, ok := nested[kind]
			if !ok {
				instances = map[string]telemetry.WorkflowFairnessFamilyDepth{}
				nested[kind] = instances
			}
			family := instances[instance]
			family.DistinctKeys = count
			instances[instance] = family
		case fairnessDepthPrefix:
			kind, rest, _ := strings.Cut(rest, snapshotKeySep)
			instance, fkey, _ := strings.Cut(rest, snapshotKeySep)
			instances, ok := nested[kind]
			if !ok {
				instances = map[string]telemetry.WorkflowFairnessFamilyDepth{}
				nested[kind] = instances
			}
			family := instances[instance]
			family.Top = append(family.Top, telemetry.WorkflowFairnessKeyPending{Key: fkey, Pending: count})
			instances[instance] = family
		}
	}
	return nested
}

// cachedWorkflowFairnessKeyDepthObserver serves the fairness depth and
// key-count gauges from one snapshot: one refresh fetch feeds both gauge
// callbacks, preserving the single-query-per-scrape idiom.
type cachedWorkflowFairnessKeyDepthObserver struct{ source *snapshot.Source }

func (c cachedWorkflowFairnessKeyDepthObserver) WorkflowFairnessKeyDepths(ctx context.Context) (map[string]map[string]telemetry.WorkflowFairnessFamilyDepth, error) {
	counts, err := c.source.Counts(ctx)
	if err != nil {
		return nil, err
	}
	if counts == nil {
		return nil, errPostgresGaugeSnapshotNotReady
	}
	return unflattenFairnessDepths(counts), nil
}
