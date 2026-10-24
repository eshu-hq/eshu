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
// instances currently report an unresolved terminal fetch failure, grouped
// by collector kind and bounded failure class (#8803). It is a point-in-time
// gauge derived from the status snapshot, not a counter: a status scrape
// cannot count failure events without double counting, and an instance that
// recovers must drop out of the series. The shared collector runtime readback
// supplies the matching and the bounded class, so a persisted class outside
// the allowlist reads as "unknown" and an unregistered instance never appears.
//
// An instance whose failure an operator acknowledged (#8830) leaves that gauge
// and is counted in eshu_runtime_coordinator_collector_terminal_failure_acknowledged_instances
// instead, with the same bounded labels, so an alert on the first gauge pages
// only for failures nobody has said are expected.
func writeCollectorTerminalFailureMetrics(
	writeGauge func(name string, labels map[string]string, value string),
	serviceName string,
	snapshot *statuspkg.CoordinatorSnapshot,
) {
	if snapshot == nil || len(snapshot.CollectorTerminalFailures) == 0 {
		return
	}
	type key = terminalFailureSeriesKey
	counts := map[key]int{}
	acknowledged := map[key]int{}
	for _, row := range collector.RuntimeStatuses(collector.Evidence{
		Instances:        snapshot.CollectorInstances,
		TerminalFailures: snapshot.CollectorTerminalFailures,
	}) {
		switch {
		case !row.LastFailureTerminal:
		case row.LastFailureAcknowledged:
			acknowledged[key{row.CollectorKind, row.LastFailureClass}]++
		case row.Health == "degraded":
			counts[key{row.CollectorKind, row.LastFailureClass}]++
		}
	}
	writeTerminalFailureSeries(writeGauge, serviceName,
		"eshu_runtime_coordinator_collector_terminal_failure_instances", counts)
	writeTerminalFailureSeries(writeGauge, serviceName,
		"eshu_runtime_coordinator_collector_terminal_failure_acknowledged_instances", acknowledged)
}

// writeTerminalFailureSeries writes one gauge series per (kind, class) in
// sorted order, so a scrape is stable.
func writeTerminalFailureSeries(
	writeGauge func(name string, labels map[string]string, value string),
	serviceName string,
	name string,
	counts map[terminalFailureSeriesKey]int,
) {
	keys := make([]terminalFailureSeriesKey, 0, len(counts))
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
			name,
			map[string]string{
				"service_name":   serviceName,
				"collector_kind": k.kind,
				"failure_class":  k.class,
			},
			strconv.Itoa(counts[k]),
		)
	}
}

// terminalFailureSeriesKey is the bounded label pair of a terminal failure
// gauge series.
type terminalFailureSeriesKey struct{ kind, class string }
