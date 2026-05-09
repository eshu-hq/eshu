package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const defaultLocalHostProgressPollInterval = 3 * time.Second

type localHostProgressStop func() error

var (
	localHostOpenProgressDB = func(dsn string) (*sql.DB, error) {
		return sql.Open("pgx", dsn)
	}
	localHostLoadProgressReport = func(ctx context.Context, reader statuspkg.Reader, asOf time.Time) (statuspkg.Report, error) {
		return statuspkg.LoadReport(ctx, reader, asOf, statuspkg.DefaultOptions())
	}
	localHostProgressWriter       io.Writer = os.Stderr
	localHostProgressNow                    = func() time.Time { return time.Now().UTC() }
	localHostProgressPollInterval           = defaultLocalHostProgressPollInterval
	localHostProgressIsTerminal             = localHostProgressWriterIsTerminal
)

func startLocalHostProgressReporter(
	ctx context.Context,
	workspaceRoot string,
	dsn string,
	runtimeConfig localHostRuntimeConfig,
) (localHostProgressStop, error) {
	db, err := localHostOpenProgressDB(dsn)
	if err != nil {
		return nil, fmt.Errorf("open local progress status connection: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping local progress status connection: %w", err)
	}

	reader := pgstorage.NewStatusStore(pgstorage.SQLQueryer{DB: db})
	renderer := newLocalHostProgressRenderer(
		workspaceRoot,
		runtimeConfig,
		localHostProgressMode(os.Getenv),
		localHostProgressWriter,
		localHostProgressIsTerminal,
	)
	reporterCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)

		ticker := time.NewTicker(localHostProgressPollInterval)
		defer ticker.Stop()

		lastFingerprint := ""
		for {
			report, err := localHostLoadProgressReport(reporterCtx, reader, localHostProgressNow())
			if err == nil {
				fingerprint := localHostProgressFingerprint(workspaceRoot, runtimeConfig, report)
				if fingerprint != lastFingerprint {
					_ = renderer.Render(report)
					lastFingerprint = fingerprint
				}
			}

			select {
			case <-reporterCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	return func() error {
		cancel()
		<-done
		return errors.Join(renderer.Close(), db.Close())
	}, nil
}

func renderLocalHostProgressSnapshot(
	workspaceRoot string,
	runtimeConfig localHostRuntimeConfig,
	report statuspkg.Report,
) string {
	var builder strings.Builder
	builder.WriteString("\n")
	builder.WriteString("Local progress ")
	builder.WriteString(report.AsOf.Format(time.RFC3339))
	builder.WriteString("\n")
	fmt.Fprintf(
		&builder,
		"  Owner: running | profile=%s | backend=%s | workspace=%s\n",
		runtimeConfig.Profile,
		localHostProgressBackendLabel(runtimeConfig),
		workspaceRoot,
	)
	fmt.Fprintf(&builder, "  Health: %s\n", report.Health.State)

	renderLocalHostKnownWorkTable(&builder, report)

	fmt.Fprintf(
		&builder,
		"  Queue: pending=%d in_flight=%d retrying=%d dead_letter=%d failed=%d oldest=%s\n",
		report.Queue.Pending,
		report.Queue.InFlight,
		report.Queue.Retrying,
		report.Queue.DeadLetter,
		report.Queue.Failed,
		localHostProgressAge(report.Queue.OldestOutstandingAge),
	)
	if backlog, ok := localHostProgressSharedProjectionBacklog(report); ok {
		fmt.Fprintf(&builder, "  Shared projections: %s\n", localHostProgressDomainBacklogText(backlog))
	}
	if latestFailure := localHostProgressFailureText(report.LatestQueueFailure); latestFailure != "" {
		fmt.Fprintf(&builder, "  Latest failure: %s\n", latestFailure)
	}
	return builder.String()
}

func localHostProgressBackendLabel(runtimeConfig localHostRuntimeConfig) string {
	if runtimeConfig.GraphBackend == "" {
		return "none"
	}
	return string(runtimeConfig.GraphBackend)
}

type localHostKnownWorkRow struct {
	stage     string
	done      int
	active    int
	waiting   int
	failed    int
	total     int
	workLabel string
}

func renderLocalHostKnownWorkTable(builder *strings.Builder, report statuspkg.Report) {
	rows := localHostKnownWorkRows(report)
	fmt.Fprintf(
		builder,
		"  %-10s %-11s %-15s %-6s %-7s %-8s %-7s %s\n",
		"Stage",
		"State",
		"Progress",
		"Done",
		"Active",
		"Waiting",
		"Failed",
		"Unit",
	)
	for _, row := range rows {
		if row.total <= 0 {
			fmt.Fprintf(
				builder,
				"  %-10s %-11s %-15s %-6s %-7d %-8d %-7d %s\n",
				row.stage,
				localHostProgressRowState(row),
				"-",
				"-",
				row.active,
				row.waiting,
				row.failed,
				localHostIdleWorkLabel(row),
			)
			continue
		}
		fmt.Fprintf(
			builder,
			"  %-10s %-11s %-15s %-6s %-7d %-8d %-7d %s\n",
			row.stage,
			localHostProgressRowState(row),
			localHostProgressBar(row.done, row.total),
			localHostProgressDoneText(row.done, row.total),
			row.active,
			row.waiting,
			row.failed,
			row.workLabel,
		)
	}
	if report.GenerationHistory.Superseded > 0 {
		fmt.Fprintf(builder, "  Superseded generations: %d\n", report.GenerationHistory.Superseded)
	}
}

func localHostProgressRowState(row localHostKnownWorkRow) string {
	if row.failed > 0 {
		return "attention"
	}
	if row.active > 0 {
		return "running"
	}
	if row.waiting > 0 {
		return "waiting"
	}
	if row.total > 0 && row.done >= row.total {
		return "complete"
	}
	return "idle"
}

func localHostIdleWorkLabel(row localHostKnownWorkRow) string {
	if row.stage == "Collector" {
		return "watching source"
	}
	return "no known work"
}

func localHostKnownWorkRows(report statuspkg.Report) []localHostKnownWorkRow {
	projector := localHostStageSummary(report.StageSummaries, "projector")
	reducer := localHostStageSummary(report.StageSummaries, "reducer")
	return []localHostKnownWorkRow{
		localHostCollectorKnownWorkRow(report.GenerationHistory),
		localHostStageKnownWorkRow("Projector", projector),
		localHostStageKnownWorkRow("Reducer", reducer),
	}
}

func localHostCollectorKnownWorkRow(history statuspkg.GenerationHistorySnapshot) localHostKnownWorkRow {
	current := history.Completed + history.Active
	total := current + history.Pending + history.Failed + history.Other
	return localHostKnownWorkRow{
		stage:     "Collector",
		done:      current,
		active:    0,
		waiting:   history.Pending,
		failed:    history.Failed + history.Other,
		total:     total,
		workLabel: "generations",
	}
}

func localHostStageKnownWorkRow(stage string, summary statuspkg.StageSummary) localHostKnownWorkRow {
	active := summary.Claimed + summary.Running
	waiting := summary.Pending + summary.Retrying
	failed := summary.Failed + summary.DeadLetter
	total := summary.Succeeded + active + waiting + failed
	return localHostKnownWorkRow{
		stage:     stage,
		done:      summary.Succeeded,
		active:    active,
		waiting:   waiting,
		failed:    failed,
		total:     total,
		workLabel: "work items",
	}
}

func localHostStageSummary(rows []statuspkg.StageSummary, stage string) statuspkg.StageSummary {
	for _, row := range rows {
		if row.Stage == stage {
			return row
		}
	}
	return statuspkg.StageSummary{Stage: stage}
}

func localHostProgressDoneText(done int, total int) string {
	return fmt.Sprintf("%d/%d", done, total)
}

func localHostProgressBar(done int, total int) string {
	const width = 12
	if done < 0 {
		done = 0
	}
	if total <= 0 {
		return "[" + strings.Repeat("-", width) + "]"
	}
	if done > total {
		done = total
	}
	filled := done * width / total
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}

func localHostProgressFraction(done int, total int) float64 {
	if total <= 0 {
		return 0
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	return float64(done) / float64(total)
}

func localHostProgressAge(age time.Duration) string {
	if age <= 0 {
		return "0s"
	}
	if age < time.Second {
		return age.String()
	}
	return age.Truncate(time.Second).String()
}

func localHostProgressSharedProjectionBacklog(report statuspkg.Report) (statuspkg.DomainBacklog, bool) {
	if report.Queue.Pending > 0 ||
		report.Queue.InFlight > 0 ||
		report.Queue.Retrying > 0 ||
		report.Queue.DeadLetter > 0 ||
		report.Queue.Failed > 0 {
		return statuspkg.DomainBacklog{}, false
	}
	for _, row := range report.DomainBacklogs {
		if row.Outstanding > 0 || row.InFlight > 0 || row.Retrying > 0 || row.DeadLetter > 0 || row.Failed > 0 {
			return row, true
		}
	}
	return statuspkg.DomainBacklog{}, false
}

func localHostProgressDomainBacklogText(row statuspkg.DomainBacklog) string {
	return fmt.Sprintf(
		"%s outstanding=%d in_flight=%d retrying=%d dead_letter=%d failed=%d oldest=%s",
		row.Domain,
		row.Outstanding,
		row.InFlight,
		row.Retrying,
		row.DeadLetter,
		row.Failed,
		localHostProgressAge(row.OldestAge),
	)
}

func localHostProgressFingerprint(
	workspaceRoot string,
	runtimeConfig localHostRuntimeConfig,
	report statuspkg.Report,
) string {
	var builder strings.Builder
	fmt.Fprintf(
		&builder,
		"%s|%s|%s|%s|",
		workspaceRoot,
		runtimeConfig.Profile,
		localHostProgressBackendLabel(runtimeConfig),
		report.Health.State,
	)
	appendNamedCountMap(&builder, report.ScopeTotals)
	appendNamedCountMap(&builder, report.GenerationTotals)
	for _, row := range report.StageSummaries {
		fmt.Fprintf(
			&builder,
			"%s|%d|%d|%d|%d|%d|%d|%d|",
			row.Stage,
			row.Pending,
			row.Claimed,
			row.Running,
			row.Retrying,
			row.Succeeded,
			row.DeadLetter,
			row.Failed,
		)
	}
	for _, row := range report.DomainBacklogs {
		fmt.Fprintf(
			&builder,
			"%s|%d|%d|%d|%d|%d|%d|",
			row.Domain,
			row.Outstanding,
			row.InFlight,
			row.Retrying,
			row.DeadLetter,
			row.Failed,
			localHostProgressAgeBucket(row.OldestAge),
		)
	}
	if failure := report.LatestQueueFailure; failure != nil {
		fmt.Fprintf(
			&builder,
			"%s|%s|%s|%s|%s|%s|",
			failure.Stage,
			failure.Domain,
			failure.Status,
			failure.FailureClass,
			failure.FailureMessage,
			failure.FailureDetails,
		)
	}
	fmt.Fprintf(
		&builder,
		"%d|%d|%d|%d|%d|%d",
		report.Queue.Pending,
		report.Queue.InFlight,
		report.Queue.Retrying,
		report.Queue.DeadLetter,
		report.Queue.Failed,
		localHostProgressAgeBucket(report.Queue.OldestOutstandingAge),
	)
	return builder.String()
}

func localHostProgressAgeBucket(age time.Duration) int64 {
	if age <= 0 {
		return 0
	}
	return int64(age / (30 * time.Second))
}

func appendNamedCountMap(builder *strings.Builder, counts map[string]int) {
	if len(counts) == 0 {
		builder.WriteString("none|")
		return
	}

	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(builder, "%s=%d|", key, counts[key])
	}
}

func localHostProgressFailureText(failure *statuspkg.QueueFailureSnapshot) string {
	if failure == nil {
		return ""
	}
	parts := []string{
		fmt.Sprintf("stage=%s", failure.Stage),
		fmt.Sprintf("domain=%s", failure.Domain),
		fmt.Sprintf("status=%s", failure.Status),
		fmt.Sprintf("class=%s", failure.FailureClass),
	}
	if message := localHostProgressBoundedText(failure.FailureMessage); message != "" {
		parts = append(parts, fmt.Sprintf("message=%s", strconv.Quote(message)))
	}
	if details := localHostProgressBoundedText(failure.FailureDetails); details != "" {
		parts = append(parts, fmt.Sprintf("details=%s", strconv.Quote(details)))
	}
	return strings.Join(parts, " ")
}

func localHostProgressBoundedText(value string) string {
	const limit = 240
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
