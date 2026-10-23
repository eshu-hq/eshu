// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"sort"
	"strconv"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// writeCollectorTerminalFailureMetrics renders how many registered collector
// instances currently report an unresolved terminal provider failure, grouped
// by collector kind and bounded failure class (#8803). It is a point-in-time
// gauge derived from the status snapshot, not a counter: a status scrape
// cannot count failure events without double counting, and an instance that
// recovers must drop out of the series. The shared collector runtime readback
// supplies the matching and the bounded class, so a persisted class outside
// the allowlist reads as "unknown" and an unregistered instance never appears.
func writeCollectorTerminalFailureMetrics(
	writeGauge func(name string, labels map[string]string, value string),
	serviceName string,
	snapshot *statuspkg.CoordinatorSnapshot,
) {
	if snapshot == nil || len(snapshot.CollectorTerminalFailures) == 0 {
		return
	}
	type key struct{ kind, class string }
	counts := map[key]int{}
	for _, row := range collector.RuntimeStatuses(collector.Evidence{
		Instances:        snapshot.CollectorInstances,
		TerminalFailures: snapshot.CollectorTerminalFailures,
	}) {
		if !row.LastFailureTerminal || row.Health != "degraded" {
			continue
		}
		counts[key{row.CollectorKind, row.LastFailureClass}]++
	}
	keys := make([]key, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].kind != keys[j].kind {
			return keys[i].kind < keys[j].kind
		}
		return keys[i].class < keys[j].class
	})
	for _, k := range keys {
		writeGauge(
			"eshu_runtime_coordinator_collector_terminal_failure_instances",
			map[string]string{
				"service_name":   serviceName,
				"collector_kind": k.kind,
				"failure_class":  k.class,
			},
			strconv.Itoa(counts[k]),
		)
	}
}
