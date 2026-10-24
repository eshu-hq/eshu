// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

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
	// Checked is false when the read was not attempted (no graph configured;
	// query_readback then fails readiness).
	Checked bool
	// Unvalidated is true off Neo4j, the only backend the detection was
	// tested on; the detection is not run there.
	Unvalidated bool
	// Err is the detection read failure. It fails the check closed.
	Err error
	// Count is the name-only Workload nodes that still have a scoped twin and
	// no instance (pre-#5385 leftovers).
	Count int
	// Truncated says Count is a lower bound: the read hit its examined cap.
	Truncated bool
}

// readWorkloadLeftovers counts the pre-#5385 name-only Workload nodes through
// the shared detection, so the readiness check and the retirement dry run
// classify every node the same way. The figures still differ: readiness
// counts every node with a scoped twin and no instance, including ones a
// relationship blocks from retirement, over up to 5,000 nodes, while a dry run
// reports the eligible ones over up to 1,000. It runs only after
// query_readback passed, so a failure here is its own condition: it is logged,
// recorded on the request span, and fails the check.
func (h *StatusHandler) readWorkloadLeftovers(ctx context.Context) workloadLeftovers {
	if h.Neo4j == nil {
		return workloadLeftovers{}
	}
	if h.GraphBackend != GraphBackendNeo4j {
		return workloadLeftovers{Checked: true, Unvalidated: true}
	}
	readCtx, cancel := context.WithTimeout(ctx, hostedReadinessWorkloadLeftoversTimeout)
	defer cancel()
	report, err := legacyworkload.Detect(readCtx, h.Neo4j, legacyworkload.Options{
		MaxExamined: hostedReadinessWorkloadLeftoversExamined,
		PageSize:    hostedReadinessWorkloadLeftoversPage,
	})
	if err != nil {
		span := trace.SpanFromContext(ctx)
		span.RecordError(err)
		span.SetStatus(codes.Error, hostedReadinessWorkloadDetectionFailed)
		slog.Default().WarnContext(ctx, "hosted readiness legacy workload detection failed",
			slog.String("check", "workload_identity_leftovers"),
			slog.String("failure_class", hostedReadinessWorkloadDetectionClass),
			slog.String("error", err.Error()))
		return workloadLeftovers{Checked: true, Err: err}
	}
	return workloadLeftovers{Checked: true, Count: report.WithScopedTwin(), Truncated: report.Truncated}
}

const (
	// hostedReadinessWorkloadDetectionClass is the failure class of a failed
	// detection read.
	hostedReadinessWorkloadDetectionClass = "legacy_workload_detection_unavailable"
	// hostedReadinessWorkloadDetectionFailed is the fixed span status text.
	hostedReadinessWorkloadDetectionFailed = "legacy workload detection read failed"
)

// addWorkloadLeftoversCheck fails readiness while name-only Workload nodes
// with a scoped twin remain (the resolver sees two candidates for each and
// refuses to guess) and when the detection read fails. Off Neo4j, and when the
// examined cap was reached with none found, the check is unknown: never pass.
func (b *hostedReadinessBuilder) addWorkloadLeftoversCheck(leftovers workloadLeftovers) {
	const name = "workload_identity_leftovers"
	switch {
	case !leftovers.Checked:
		return
	case leftovers.Unvalidated:
		b.addUnknown(name, "legacy workload detection is validated on Neo4j only; not run on this graph backend")
		return
	case leftovers.Err != nil:
		b.addFail(name, hostedReadinessWorkloadDetectionClass,
			"the legacy workload detection read failed, so leftovers are unknown",
			"retry; if it persists, check graph backend health and the API log line 'hosted readiness legacy workload detection failed'")
		return
	case leftovers.Count == 0 && leftovers.Truncated:
		b.addUnknown(name, fmt.Sprintf("none found in the first %d name-only Workload nodes examined; the rest were not examined", hostedReadinessWorkloadLeftoversExamined))
		return
	case leftovers.Count == 0:
		b.addPass(name, "no name-only Workload nodes with a scoped twin")
		return
	}
	count := fmt.Sprintf("%d", leftovers.Count)
	if leftovers.Truncated {
		count += "+"
	}
	b.addFail(
		name,
		"legacy_workload_nodes",
		fmt.Sprintf("%s name-only Workload nodes with a scoped twin (pre-#5385 leftovers)", count),
		`POST /admin/retire-legacy-workloads {"dry_run":true} on the reducer admin port; see the leftover retirement runbook`,
	)
}
