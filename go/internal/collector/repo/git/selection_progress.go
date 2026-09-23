// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const gitProgressLogInterval = 5 * time.Second

// gitSyncLogEvent carries the bounded repository-sync context safe to expose
// in hosted operator logs.
type gitSyncLogEvent struct {
	Operation       string
	RepositoryID    string
	ProviderKind    string
	RepositoryIndex int
	RepositoryCount int
	Branch          string
	StartedAt       time.Time
}

// gitProgressWriter tees git stderr into the error buffer while logging
// sanitized progress lines during long clone/fetch operations.
type gitProgressWriter struct {
	ctx     context.Context
	logger  *slog.Logger
	event   gitSyncLogEvent
	stderr  *bytes.Buffer
	now     func() time.Time
	mu      sync.Mutex
	pending strings.Builder
	lastLog time.Time
	logged  bool
}

// newGitProgressWriter builds the stderr sink used by git commands that can
// run long enough to need operator-visible progress.
func newGitProgressWriter(
	ctx context.Context,
	logger *slog.Logger,
	event gitSyncLogEvent,
	stderr *bytes.Buffer,
) *gitProgressWriter {
	return &gitProgressWriter{
		ctx:    ctx,
		logger: logger,
		event:  event,
		stderr: stderr,
		now:    time.Now,
	}
}

func (w *gitProgressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stderr != nil {
		w.stderr.Write(p)
	}
	if w.logger == nil {
		return len(p), nil
	}
	w.pending.Write(p)
	w.logCompleteProgressLines()
	return len(p), nil
}

// Flush emits the final unterminated progress line, if git exited without a
// trailing carriage return or newline.
func (w *gitProgressWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.logger == nil || w.pending.Len() == 0 {
		return
	}
	w.logProgressLine(w.pending.String())
	w.pending.Reset()
}

func (w *gitProgressWriter) logCompleteProgressLines() {
	text := w.pending.String()
	start := 0
	for i, r := range text {
		if !isGitProgressSeparator(r) {
			continue
		}
		w.logProgressLine(text[start:i])
		start = i + 1
	}
	if start == 0 {
		return
	}
	w.pending.Reset()
	w.pending.WriteString(text[start:])
}

func (w *gitProgressWriter) logProgressLine(line string) {
	message := sanitizeGitProgressMessage(strings.TrimSpace(line))
	if message == "" {
		return
	}
	now := w.now()
	if w.logged && now.Sub(w.lastLog) < gitProgressLogInterval && !gitProgressLineIsTerminal(message) {
		return
	}
	w.logged = true
	w.lastLog = now
	w.logger.InfoContext(
		w.ctx, "git repository sync progress",
		append(w.event.eventAttrs(now), slog.String("message", message))...,
	)
}

// logGitSyncStarted marks the beginning of a clone or fetch operation before
// git starts network or credential work.
func logGitSyncStarted(ctx context.Context, logger *slog.Logger, event gitSyncLogEvent) {
	if logger == nil {
		return
	}
	logger.InfoContext(ctx, "git repository sync started", event.eventAttrs(time.Now())...)
}

// logGitSyncCompleted records terminal clone/fetch success and whether the
// managed checkout changed.
func logGitSyncCompleted(ctx context.Context, logger *slog.Logger, event gitSyncLogEvent, changed bool) {
	if logger == nil {
		return
	}
	attrs := append(event.eventAttrs(time.Now()), slog.Bool("changed", changed))
	logger.InfoContext(ctx, "git repository sync completed", attrs...)
}

// logGitSyncFailed records terminal clone/fetch failure without emitting raw
// credential-bearing stderr.
func logGitSyncFailed(ctx context.Context, logger *slog.Logger, event gitSyncLogEvent, err error) {
	if logger == nil {
		return
	}
	attrs := append(
		event.eventAttrs(time.Now()),
		slog.String("error", sanitizeGitProgressMessage(err.Error())),
		telemetry.FailureClassAttr("git_sync_failure"),
	)
	logger.ErrorContext(ctx, "git repository sync failed", attrs...)
}

// resolveRepoRefsIsolated calls remoteGitRefs for one repository and isolates
// a per-repository failure (DNS timeout, auth, transient network) from the
// rest of the sync cycle (#7001): it logs and meters the failure via
// logGitSyncFailed/recordGitRepoSyncFailure and returns ok=false so the
// caller skips only this repository for the cycle, retrying it naturally on
// the next poll. Parent-context cancellation (shutdown already in flight) is
// NOT isolated — fatalErr is non-nil so the caller propagates it, matching
// syncGitRepositoriesWithLogger's existing top-of-iteration ctx.Err() check.
func resolveRepoRefsIsolated(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
	instruments *telemetry.Instruments,
) (refs []GitRef, ok bool, fatalErr error) {
	refs, refsErr := remoteGitRefs(ctx, config, repoPath, token)
	if refsErr == nil {
		return refs, true, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, false, ctxErr
	}
	logGitSyncFailed(ctx, logger, event.withOperation("list_refs"), refsErr)
	recordGitRepoSyncFailure(ctx, instruments, "list_refs")
	return nil, false, nil
}

// recordGitRepoSyncFailure increments the bounded per-repository git sync
// failure counter (#7001) so an operator can see the isolated-failure rate
// without scraping logs. A nil instruments is a no-op (unwired in tests and
// some callers). A canceled ctx is also a no-op: shutdown killing an
// in-flight clone/fetch/list_refs is not a sync failure, and counting it
// would add teardown noise to a signal meant to surface remote/DNS/auth
// trouble. Repository identity never becomes a metric label; it stays in the
// paired logGitSyncFailed log line.
func recordGitRepoSyncFailure(ctx context.Context, instruments *telemetry.Instruments, operation string) {
	if instruments == nil || instruments.GitRepoSyncFailures == nil || ctx.Err() != nil {
		return
	}
	instruments.GitRepoSyncFailures.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOperation, operation),
	))
}

func (e gitSyncLogEvent) eventAttrs(now time.Time) []any {
	attrs := []any{
		log.CollectorKind("git"),
		log.Operation(e.Operation),
		log.RepositoryID(e.RepositoryID),
		slog.Int("repository_index", e.RepositoryIndex),
		slog.Int("repository_count", e.RepositoryCount),
		log.ElapsedSeconds(now.Sub(e.StartedAt).Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseDiscovery),
	}
	if e.ProviderKind != "" {
		attrs = append(attrs, slog.String("provider_kind", e.ProviderKind))
	}
	if e.Branch != "" {
		attrs = append(attrs, slog.String("branch", e.Branch))
	}
	return attrs
}

func (e gitSyncLogEvent) withOperation(operation string) gitSyncLogEvent {
	e.Operation = operation
	e.StartedAt = time.Now()
	return e
}

func gitSyncLogEventFor(repoID string, index int, total int) gitSyncLogEvent {
	provider := gitSyncProviderKind(repoID)
	return gitSyncLogEvent{
		RepositoryID:    normalizeRepositoryID(repoID),
		ProviderKind:    provider,
		RepositoryIndex: index,
		RepositoryCount: total,
		StartedAt:       time.Now(),
	}
}

func gitSyncProviderKind(repoID string) string {
	provider, slug := repoProviderAndSlug(repoID)
	if provider != "" {
		return provider
	}
	if len(strings.Split(slug, "/")) == 2 {
		return "github"
	}
	return "unknown"
}

func isGitProgressSeparator(r rune) bool {
	return r == '\n' || r == '\r'
}

func gitProgressLineIsTerminal(message string) bool {
	lower := strings.ToLower(message)
	return strings.HasPrefix(lower, "fatal:") ||
		strings.HasPrefix(lower, "error:") ||
		strings.HasPrefix(lower, "fatal authentication failed") ||
		strings.HasPrefix(lower, "authentication failed")
}

// gitRunWithStderrWriter runs a git command scoped to repoPath, teeing stderr
// through stderrWriter (a *gitProgressWriter, or nil) so long-running
// operations can log progress while still returning the sanitized error text
// on failure. Colocated with the rest of this file's progress/stderr
// plumbing rather than in selection_cli.go's clone/fetch control flow.
func gitRunWithStderrWriter(
	ctx context.Context,
	repoPath string,
	config RepoSyncConfig,
	token string,
	stderrWriter io.Writer,
	args ...string,
) (string, error) {
	commandArgs := make([]string, 0, len(args)+2)
	commandArgs = append(commandArgs, "-C", repoPath)
	commandArgs = append(commandArgs, args...)
	command := newGitCommand(ctx, commandArgs...)
	command.Env = gitCommandEnv(config, token)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	if stderrWriter != nil {
		command.Stderr = io.MultiWriter(&stderr, stderrWriter)
	} else {
		command.Stderr = &stderr
	}
	if err := command.Run(); err != nil {
		flushProgressWriter(stderrWriter)
		return "", fmt.Errorf(
			"git %s: %w: %s",
			strings.Join(args, " "),
			err,
			sanitizeGitProgressMessage(strings.TrimSpace(stderr.String())),
		)
	}
	flushProgressWriter(stderrWriter)
	return strings.TrimSpace(stdout.String()), nil
}

func flushProgressWriter(writer io.Writer) {
	flusher, ok := writer.(interface{ Flush() })
	if ok {
		flusher.Flush()
	}
}

// sanitizeGitProgressMessage redacts URL userinfo from git stderr before the
// text appears in logs or wrapped errors.
func sanitizeGitProgressMessage(message string) string {
	fields := strings.Fields(message)
	for i, field := range fields {
		fields[i] = sanitizeGitProgressField(field)
	}
	return strings.Join(fields, " ")
}

func sanitizeGitProgressField(field string) string {
	leadingLen := len(field) - len(strings.TrimLeft(field, "'\"([{<"))
	trailingStart := len(strings.TrimRight(field[leadingLen:], "'\")]}>,")) + leadingLen
	leading := field[:leadingLen]
	core := field[leadingLen:trailingStart]
	trailing := field[trailingStart:]
	parsed, err := url.Parse(core)
	if err != nil || parsed.User == nil || parsed.Scheme == "" || parsed.Host == "" {
		return field
	}
	parsed.User = nil
	prefix := parsed.Scheme + "://"
	return leading + prefix + "[redacted]@" + strings.TrimPrefix(parsed.String(), prefix) + trailing
}
