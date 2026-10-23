// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"time"

	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
)

const (
	// hostedReadinessWorkloadLeftoversTimeout bounds the detection reads so a
	// slow graph cannot hold the readiness route open.
	hostedReadinessWorkloadLeftoversTimeout = 10 * time.Second
	// hostedReadinessWorkloadLeftoversExamined caps the candidates one
	// readiness read evaluates. The reported count is then a lower bound.
	hostedReadinessWorkloadLeftoversExamined = 5000
	// hostedReadinessWorkloadLeftoversPage is how many candidates one page
	// evaluates.
	hostedReadinessWorkloadLeftoversPage = 500
)

// workloadLeftovers is the outcome of the name-only Workload detection read
// for the hosted readiness report.
type workloadLeftovers struct {
	// Checked is false when the read was not attempted or failed; the report
	// then carries no check, because query_readback already reports a
	// graph that cannot be read.
	Checked bool
	// Count is the name-only Workload nodes that still have a scoped twin and
	// no instance (pre-#5385 leftovers).
	Count int
	// Truncated says Count is a lower bound: the read hit its examined cap.
	Truncated bool
}

// readWorkloadLeftovers counts the pre-#5385 name-only Workload nodes through
// the shared detection, so the readiness check and the retirement dry run
// cannot disagree.
func (h *StatusHandler) readWorkloadLeftovers(ctx context.Context) workloadLeftovers {
	if h.Neo4j == nil {
		return workloadLeftovers{}
	}
	ctx, cancel := context.WithTimeout(ctx, hostedReadinessWorkloadLeftoversTimeout)
	defer cancel()
	report, err := legacyworkload.Detect(ctx, h.Neo4j, legacyworkload.Options{
		MaxExamined: hostedReadinessWorkloadLeftoversExamined,
		PageSize:    hostedReadinessWorkloadLeftoversPage,
	})
	if err != nil {
		return workloadLeftovers{}
	}
	return workloadLeftovers{Checked: true, Count: report.WithScopedTwin(), Truncated: report.Truncated}
}

// addWorkloadLeftoversCheck fails readiness while name-only Workload nodes
// with a scoped twin remain: the resolver sees two candidates for each and
// refuses to guess. The vocabulary is pass or fail only.
func (b *hostedReadinessBuilder) addWorkloadLeftoversCheck(leftovers workloadLeftovers) {
	if !leftovers.Checked {
		return
	}
	if leftovers.Count == 0 {
		b.addPass("workload_identity_leftovers", "no name-only Workload nodes with a scoped twin")
		return
	}
	count := fmt.Sprintf("%d", leftovers.Count)
	if leftovers.Truncated {
		count += "+"
	}
	b.addFail(
		"workload_identity_leftovers",
		"legacy_workload_nodes",
		fmt.Sprintf("%s name-only Workload nodes with a scoped twin (pre-#5385 leftovers)", count),
		`POST /admin/retire-legacy-workloads {"dry_run":true} on the reducer admin port; see the leftover retirement runbook`,
	)
}
