// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/shared"
)

const (
	hostedReadinessReady    = "ready"
	hostedReadinessNotReady = "not_ready"

	hostedReadinessCheckPass = "pass"
	hostedReadinessCheckFail = "fail"
	// hostedReadinessCheckUnknown marks a check that gave no verdict on this
	// deployment (not run, or not complete). It adds no failure class.
	hostedReadinessCheckUnknown = "unknown"
)

type hostedReadinessReport struct {
	Version         string         `json:"version"`
	State           string         `json:"state"`
	Ready           bool           `json:"ready"`
	Summary         string         `json:"summary"`
	GeneratedAt     string         `json:"generated_at"`
	FailureClasses  []string       `json:"failure_classes"`
	RepositoryCount int            `json:"repository_count"`
	Queue           map[string]any `json:"queue"`
	// ActiveWorkSource says whether the queue came from the stored summary or
	// the live statement (#7009); absent when the reader reports none.
	ActiveWorkSource *status.ActiveWorkSourceJSON `json:"active_work_source,omitempty"`
	Coordinator      map[string]any               `json:"coordinator"`
	Checks           []hostedReadinessCheck       `json:"checks"`
	DiagnosticPaths  []hostedDiagnosticTarget     `json:"diagnostic_paths"`
}

type hostedReadinessCheck struct {
	Name           string `json:"name"`
	State          string `json:"state"`
	FailureClass   string `json:"failure_class,omitempty"`
	Detail         string `json:"detail"`
	NextDiagnostic string `json:"next_diagnostic,omitempty"`
}

type hostedDiagnosticTarget struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// hostedReadinessStatusReadFailedMessage is the fixed body of a failed status
// snapshot read on GET /api/v0/status/hosted-readiness (#7674). The status
// snapshot reads through the guarded PostgreSQL reader, so a stale or
// timed-out reader answers the retryable 503 under
// hostedReadinessStatusCapability, which the error envelope names (#8227). The
// reader error goes on the request span, never into the body.
const hostedReadinessStatusReadFailedMessage = "hosted readiness status read failed"

// hostedReadinessStatusCapability is the capability-matrix key a reader-fence
// 503 on GET /api/v0/status/hosted-readiness names in error.capability.
const hostedReadinessStatusCapability = "operations.hosted_readiness_status"

func (h *StatusHandler) getHostedReadiness(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	raw, report, err := loadStatusReportFiltered(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions(), operatorStatusSelection())
	if err != nil {
		if WriteGraphReadError(w, r, err, hostedReadinessStatusCapability) {
			return
		}
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, hostedReadinessStatusReadFailedMessage)
		return
	}

	repositoryCount, graphErr := h.readHostedRepositoryCount(r.Context())
	var leftovers workloadLeftovers
	if graphErr == nil {
		leftovers = h.readWorkloadLeftovers(r.Context())
	}
	payload := buildHostedReadinessReport(raw, report, repositoryCount, graphErr, leftovers, scopedAuthContext(r.Context()))
	if QueryParam(r, "format") == "text" {
		writeHostedReadinessText(w, payload)
		return
	}
	WriteJSON(w, http.StatusOK, payload)
}

func (h *StatusHandler) readHostedRepositoryCount(ctx context.Context) (int, error) {
	if h.Neo4j == nil {
		return 0, fmt.Errorf("graph query is not configured")
	}
	row, err := h.Neo4j.RunSingle(ctx, "MATCH (r:Repository) RETURN count(r) as count", nil)
	if err != nil {
		return 0, fmt.Errorf("repository readback failed")
	}
	if row == nil {
		return 0, nil
	}
	return IntVal(row, "count"), nil
}

func buildHostedReadinessReport(
	raw status.RawSnapshot,
	report status.Report,
	repositoryCount int,
	graphErr error,
	leftovers workloadLeftovers,
	scoped bool,
) hostedReadinessReport {
	builder := hostedReadinessBuilder{}
	builder.addPass("process_health", "API served the hosted readiness route")
	builder.addPass("dependency_readiness", "Postgres status snapshot loaded successfully")
	builder.addQueueChecks(report)
	builder.addCollectorChecks(report.Coordinator)
	builder.addSharedProjectionChecks(raw.DomainBacklogs)
	builder.addQueryReadbackCheck(repositoryCount, graphErr)
	builder.addWorkloadLeftoversCheck(leftovers)
	builder.addEmptyStateCheck(raw, repositoryCount)

	state := hostedReadinessReady
	if len(builder.failureClasses) > 0 {
		state = hostedReadinessNotReady
	}
	failureClasses := builder.failureClasses
	if failureClasses == nil {
		failureClasses = []string{}
	}
	coordinator := coordinatorToMap(report.Coordinator)
	if scoped {
		coordinator = scopedCoordinatorToMap(report.Coordinator)
	}
	return hostedReadinessReport{
		Version:          buildinfo.AppVersion(),
		State:            state,
		Ready:            state == hostedReadinessReady,
		Summary:          hostedReadinessSummary(state, failureClasses),
		GeneratedAt:      report.AsOf.UTC().Format(time.RFC3339),
		FailureClasses:   failureClasses,
		RepositoryCount:  repositoryCount,
		Queue:            queueToMap(report.Queue),
		ActiveWorkSource: report.ActiveWorkSource.JSON(),
		Coordinator:      coordinator,
		Checks:           builder.checks,
		DiagnosticPaths: []hostedDiagnosticTarget{
			{Name: "admin_status", Path: "/admin/status"},
			{Name: "index_status", Path: "/api/v0/status/index"},
			{Name: "collector_status", Path: "/api/v0/status/collectors"},
			{Name: "repository_coverage", Path: "/api/v0/repositories/{repo_id}/coverage"},
		},
	}
}

type hostedReadinessBuilder struct {
	checks         []hostedReadinessCheck
	failureSeen    map[string]bool
	failureClasses []string
}

func (b *hostedReadinessBuilder) addPass(name, detail string) {
	b.checks = append(b.checks, hostedReadinessCheck{
		Name:   name,
		State:  hostedReadinessCheckPass,
		Detail: detail,
	})
}

func (b *hostedReadinessBuilder) addUnknown(name, detail string) {
	b.checks = append(b.checks, hostedReadinessCheck{Name: name, State: hostedReadinessCheckUnknown, Detail: detail})
}

func (b *hostedReadinessBuilder) addFail(name, class, detail, next string) {
	if b.failureSeen == nil {
		b.failureSeen = make(map[string]bool)
	}
	if !b.failureSeen[class] {
		b.failureSeen[class] = true
		b.failureClasses = append(b.failureClasses, class)
	}
	b.checks = append(b.checks, hostedReadinessCheck{
		Name:           name,
		State:          hostedReadinessCheckFail,
		FailureClass:   class,
		Detail:         detail,
		NextDiagnostic: next,
	})
}

func (b *hostedReadinessBuilder) addQueueChecks(report status.Report) {
	queue := report.Queue
	collectorDeadLetters := report.CollectorGenerationDeadLetters
	if collectorDeadLetters.DeadLetter+collectorDeadLetters.ReplayRequested > 0 {
		b.addFail(
			"collector_generation_replay",
			"collector_generation_dead_lettered",
			fmt.Sprintf(
				"collector generation dead letters=%d replay_requested=%d oldest=%s",
				collectorDeadLetters.DeadLetter,
				collectorDeadLetters.ReplayRequested,
				collectorDeadLetters.OldestDeadLetterAge,
			),
			"request source-level replay through /admin/replay-collector-generations",
		)
	}
	if queue.DeadLetter > 0 || queue.Failed > 0 {
		b.addFail(
			"queue_drain",
			"dead_lettered_work",
			fmt.Sprintf("queue has dead_letter=%d failed=%d", queue.DeadLetter, queue.Failed),
			"inspect /admin/status latest_failure and queue_blockages",
		)
	}
	if queue.Outstanding > 0 && queue.InFlight == 0 && queue.OldestOutstandingAge >= status.DefaultOptions().StallAfter {
		b.addFail(
			"queue_drain",
			"queue_stalled",
			fmt.Sprintf("queue has %d outstanding items with no in-flight work for %s", queue.Outstanding, queue.OldestOutstandingAge),
			"inspect queue age, overdue claims, and reducer logs",
		)
	}
	if queue.Outstanding > 0 || queue.InFlight > 0 || queue.Retrying > 0 {
		b.addFail(
			"queue_drain",
			"queue_not_drained",
			fmt.Sprintf("queue outstanding=%d in_flight=%d retrying=%d", queue.Outstanding, queue.InFlight, queue.Retrying),
			"wait for queue zero or inspect /api/v0/status/index",
		)
		return
	}
	if queue.DeadLetter == 0 && queue.Failed == 0 {
		b.addPass("queue_drain", "queue has no outstanding, retrying, failed, or dead-lettered work")
	}
}

func (b *hostedReadinessBuilder) addCollectorChecks(coordinator *status.CoordinatorSnapshot) {
	if coordinator == nil {
		b.addFail(
			"collector_completion",
			"collector_instances_missing",
			"workflow coordinator status is not present",
			"inspect /api/v0/status/collectors and the workflow coordinator /admin/status",
		)
		return
	}

	enabled := 0
	for _, instance := range coordinator.CollectorInstances {
		if instance.Enabled {
			enabled++
		}
	}
	if enabled == 0 {
		b.addFail(
			"collector_completion",
			"collector_instances_missing",
			"no enabled collector instances are registered",
			"inspect /api/v0/status/collectors",
		)
		return
	}
	if coordinator.OverdueClaims > 0 {
		b.addFail(
			"collector_completion",
			"collector_claims_overdue",
			fmt.Sprintf("workflow coordinator has %d overdue claims", coordinator.OverdueClaims),
			"inspect workflow coordinator /admin/status",
		)
		return
	}
	counts := namedCounts(coordinator.CompletenessCounts)
	if counts["blocked"] > 0 || counts["failed"] > 0 {
		b.addFail(
			"collector_completion",
			"collector_completion_blocked",
			fmt.Sprintf("collector completeness blocked=%d failed=%d", counts["blocked"], counts["failed"]),
			"inspect collector completeness and source-fact readback",
		)
		return
	}
	b.addPass("collector_completion", fmt.Sprintf("%d enabled collector instance(s) observed", enabled))
}

func (b *hostedReadinessBuilder) addSharedProjectionChecks(backlogs []status.DomainBacklog) {
	outstanding := 0
	retrying := 0
	deadLetter := 0
	for _, backlog := range backlogs {
		outstanding += backlog.Outstanding
		retrying += backlog.Retrying
		deadLetter += backlog.DeadLetter
	}
	if outstanding > 0 || retrying > 0 || deadLetter > 0 {
		b.addFail(
			"shared_projection",
			"shared_projection_backlog",
			fmt.Sprintf("shared projection outstanding=%d retrying=%d dead_letter=%d", outstanding, retrying, deadLetter),
			"inspect /admin/status domain_backlogs and reducer telemetry",
		)
		return
	}
	b.addPass("shared_projection", "no reducer domain backlog is outstanding")
}

func (b *hostedReadinessBuilder) addQueryReadbackCheck(repositoryCount int, graphErr error) {
	if graphErr != nil {
		b.addFail(
			"query_readback",
			"graph_unavailable",
			"bounded graph repository readback failed",
			"inspect graph backend connectivity and API query logs",
		)
		return
	}
	if repositoryCount <= 0 {
		b.addFail(
			"query_readback",
			"query_readback_missing",
			"repository readback returned zero repositories",
			"inspect /api/v0/index-status and repository coverage",
		)
		return
	}
	b.addPass("query_readback", fmt.Sprintf("API/MCP shared query path read back %d repositories", repositoryCount))
}

func (b *hostedReadinessBuilder) addEmptyStateCheck(raw status.RawSnapshot, repositoryCount int) {
	if repositoryCount > 0 || raw.Queue.Total > 0 || raw.ScopeActivity.Active > 0 || len(raw.ScopeCounts) > 0 {
		return
	}
	b.addFail(
		"first_query_truth",
		"empty_state",
		"no repositories, active scopes, or queue history were observed",
		"run bootstrap indexing or inspect ingestion scope status",
	)
}

func namedCounts(rows []shared.NamedCount) map[string]int {
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.Name] += row.Count
	}
	return counts
}

func hostedReadinessSummary(state string, failureClasses []string) string {
	if state == hostedReadinessReady {
		return "hosted readiness ready: process, dependencies, queue, collectors, projection, and query readback passed"
	}
	return "hosted readiness blocked: " + strings.Join(failureClasses, ", ")
}

func writeHostedReadinessText(w http.ResponseWriter, report hostedReadinessReport) {
	lines := []string{
		fmt.Sprintf("Hosted readiness: %s", report.State),
		"Summary: " + report.Summary,
		fmt.Sprintf("Repository count: %d", report.RepositoryCount),
		"Checks:",
	}
	for _, check := range report.Checks {
		line := fmt.Sprintf("- %s: %s", check.Name, check.State)
		if check.FailureClass != "" {
			line += " " + check.FailureClass
		}
		if check.Detail != "" {
			line += " - " + check.Detail
		}
		lines = append(lines, line)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(strings.Join(lines, "\n") + "\n"))
}

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
// cannot disagree. It runs only after query_readback passed, so a failure here
// is its own condition: it is logged, recorded on the request span, and fails
// the check.
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
