package main

import (
	"context"
	"database/sql"
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
				rendered := renderLocalHostProgressSnapshot(workspaceRoot, runtimeConfig, report)
				if fingerprint != lastFingerprint {
					_, _ = io.WriteString(localHostProgressWriter, rendered)
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
		return db.Close()
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

	for _, row := range report.FlowSummaries {
		fmt.Fprintf(
			&builder,
			"  %s: progress=%s | backlog=%s\n",
			localHostProgressLaneLabel(row.Lane),
			row.Progress,
			row.Backlog,
		)
	}

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

func localHostProgressAge(age time.Duration) string {
	if age <= 0 {
		return "0s"
	}
	if age < time.Second {
		return age.String()
	}
	return age.Truncate(time.Second).String()
}

func localHostProgressLaneLabel(lane string) string {
	if lane == "" {
		return "Lane"
	}
	return strings.ToUpper(lane[:1]) + lane[1:]
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
			"%s|%d|%d|%d|%d|%d|",
			row.Domain,
			row.Outstanding,
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
