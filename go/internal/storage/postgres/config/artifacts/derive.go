// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// DefaultDeriveTimeout is the deadline of one derive when DeriverConfig sets
// none (protocol section 3, failure semantics). The environment variable that
// will override it (Proposed name ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT) is
// registered by the wiring slice of PR3b-2.
const DefaultDeriveTimeout = 10 * time.Second

// deriveFailedEvent is the event_name of the ERROR record a failed derive
// logs (decision record section 3.5).
const deriveFailedEvent = "config_artifacts.derive.failed"

// Database is the connection pool a Deriver reads and writes through. Step 2
// of the protocol reads content_files through it with up to
// contentartifacts.ReadChunksInFlight statements in flight at once, and step
// 3 begins its own transaction on it, so it must be a pool (postgres.SQLDB in
// the runtimes), never a transaction.
type Database interface {
	db.ExecQueryer
	db.Beginner
}

// DeriverConfig configures a Deriver.
type DeriverConfig struct {
	// ExtractorVersion is the version of the config-artifact extractors this
	// binary runs. It must be positive. The derive stores it on the state row
	// and compares it in the write conditions, so an older binary never
	// replaces rows a newer one wrote.
	ExtractorVersion int
	// Encoding is the result of CheckEncoding at process start. When it is
	// not Supported every derive returns OutcomeEncodingUnsupported without a
	// statement.
	Encoding Encoding
	// Fence is the result of CheckFence at process start. When it is not
	// Supported, for a missing or non-firing trigger or for a failed catalog
	// read (ReasonFenceCheckFailed), every derive returns OutcomeFenceMissing
	// without a statement, as a failed encoding read maps to
	// OutcomeEncodingUnsupported.
	Fence Fence
	// Timeout bounds one derive, steps 1 to 3. Zero means
	// DefaultDeriveTimeout; a negative value is an error.
	Timeout time.Duration
	// Logger receives the config_artifacts.derive.failed record. Nil logs
	// nothing.
	Logger *slog.Logger
}

// Request names the repository one derive runs for.
type Request struct {
	// RepoID is the content_files repository id. It must not be blank.
	RepoID string
	// ScopeID and GenerationID are advisory provenance stored on the state
	// row by a successful derive. An empty value keeps the stored one: the
	// walker knows neither.
	ScopeID      string
	GenerationID string
	// Trigger says what started the derive. It is echoed in the Result for
	// the derives_total counter and in the failure log.
	Trigger Trigger
}

// Result is what one derive did. A derive never returns an error to its
// caller: a content Write must not fail because its read model failed
// (protocol section 3). Err is the underlying failure for the caller's log; it
// is nil for every outcome but OutcomeError, OutcomePanic and OutcomeTimeout.
// The wiring slice counts Outcome by Trigger and observes Duration; this
// package registers no metric.
type Result struct {
	// RepoID and Trigger echo the Request.
	RepoID  string
	Trigger Trigger
	// Outcome is the derives_total outcome label.
	Outcome Outcome
	// ContentVersion is v0, the version read in step 1, or -1 when the
	// derive stopped before step 1 finished.
	ContentVersion int64
	// Rows is the number of rows step 2 built. They were written only when
	// Outcome is OutcomeOK.
	Rows int
	// Stats are the counts and durations of step 2.
	Stats contentartifacts.ReadStats
	// Duration is the wall time of the derive, failure record included.
	Duration time.Duration
	// ErrorClass is the last_error_class a failed derive records: panic,
	// timeout, canceled, sqlstate_<SQLSTATE> or error. Empty otherwise.
	ErrorClass string
	// Err is the failure, for logging only.
	Err error
	// FailureRecorded is true when the failure-only state update committed.
	FailureRecorded bool
}

// loadFunc is the per-repository function of step 2. Production uses
// contentartifacts.LoadConfigPaths; tests substitute a slow, failing or
// panicking load through the same seam.
type loadFunc func(
	ctx context.Context,
	reader contentartifacts.Reader,
	repo contentartifacts.Repository,
	files []contentartifacts.File,
	hasFileList bool,
	allowance int,
) ([]map[string]any, contentartifacts.ReadStats, error)

// Deriver derives the stored config-artifact rows of one repository at a time
// under protocol section 3 of the decision record's companion. It is safe for
// concurrent use: two derives of one repository serialize only inside step 3,
// on the repository advisory lock, and derives of different repositories
// never wait on each other.
type Deriver struct {
	database         Database
	reader           contentartifacts.Reader
	extractorVersion int
	encoding         Encoding
	fence            Fence
	timeout          time.Duration
	logger           *slog.Logger

	load loadFunc
	// afterVersionRead and afterLock are test hooks: they run between steps
	// 1 and 2, and inside step 3 right after the lock is granted.
	afterVersionRead func(context.Context)
	afterLock        func(context.Context)
}

// NewDeriver returns a Deriver over database. It fails on a nil database, a
// non-positive extractor version or a negative timeout.
func NewDeriver(database Database, config DeriverConfig) (*Deriver, error) {
	if database == nil {
		return nil, errors.New("config artifact deriver: database is nil")
	}
	if config.ExtractorVersion <= 0 {
		return nil, fmt.Errorf("config artifact deriver: extractor version must be positive, got %d", config.ExtractorVersion)
	}
	if config.Timeout < 0 {
		return nil, fmt.Errorf("config artifact deriver: timeout must not be negative, got %s", config.Timeout)
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = DefaultDeriveTimeout
	}
	return &Deriver{
		database:         database,
		reader:           contentReader{queryer: database},
		extractorVersion: config.ExtractorVersion,
		encoding:         config.Encoding,
		fence:            config.Fence,
		timeout:          timeout,
		logger:           config.Logger,
		load:             contentartifacts.LoadConfigPaths,
	}, nil
}

// DeriveRepository derives req.RepoID under protocol section 3 and returns
// what happened. It never fails its caller:
//
//  1. It reads v0, the repository's committed content version, before it
//     lists any file.
//  2. With no transaction open and no lock held, it lists the repository's
//     files, reads the candidates of the whole list window
//     (contentartifacts.AllowanceWholeWindow) through the shared content_files
//     statements, and runs the extractors with no repository name.
//  3. In one short transaction it takes the repository advisory lock, reads
//     the state row, and writes only when mayWrite allows it: it deletes the
//     repository's rows, inserts the new ones, upserts the state with
//     content_version = v0 and failure_count = 0, and commits. Otherwise it
//     rolls back with OutcomeSuperseded.
//
// The derive runs under the configured deadline and recovers a panic. On an
// error, a panic or a timeout it writes a failure-only state update (see
// recordFailure), logs config_artifacts.derive.failed at ERROR, and returns
// the outcome with Err set. A failed derive never changes the rows or
// content_version, so readers keep trusting rows that still match the
// current version and fall back live otherwise.
//
// The deadline interrupts every statement and is checked again before step 3.
// A CPU-bound extractor is not preempted (Go cannot stop a goroutine, and
// abandoning one would leak it): an extractor that overruns ends as
// OutcomeTimeout when it returns, without a write. A panic inside one of the
// neutral package's chunk-read goroutines is outside this recover.
func (d *Deriver) DeriveRepository(ctx context.Context, req Request) Result {
	started := time.Now()
	result := Result{RepoID: req.RepoID, Trigger: req.Trigger, ContentVersion: -1}
	switch {
	case !d.encoding.Supported:
		result.Outcome = OutcomeEncodingUnsupported
		return result
	case !d.fence.Supported:
		result.Outcome = OutcomeFenceMissing
		return result
	case strings.TrimSpace(req.RepoID) == "":
		result.Outcome, result.ErrorClass = OutcomeError, errorClassError
		result.Err = errors.New("config artifact derive: repository id is blank")
		return result
	}

	deriveCtx, cancel := context.WithTimeout(ctx, d.timeout)
	outcome, err := d.derive(deriveCtx, req, &result)
	deadline := deriveCtx.Err()
	cancel()
	if err == nil {
		result.Outcome = outcome
		result.Duration = time.Since(started)
		return result
	}

	result.Outcome, result.ErrorClass = classifyFailure(err, deadline)
	result.Err = err
	recordErr := d.recordFailure(ctx, req.RepoID, result.ErrorClass)
	result.FailureRecorded = recordErr == nil
	result.Duration = time.Since(started)
	d.logFailure(ctx, result, recordErr)
	return result
}

// derive runs steps 1 to 3 and turns a panic into a *panicError.
func (d *Deriver) derive(ctx context.Context, req Request, result *Result) (outcome Outcome, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome, err = "", newPanicError(recovered)
		}
	}()

	v0, err := readContentVersion(ctx, d.database, req.RepoID)
	if err != nil {
		return "", err
	}
	result.ContentVersion = v0
	if d.afterVersionRead != nil {
		d.afterVersionRead(ctx)
	}

	built, stats, err := d.load(ctx, d.reader, contentartifacts.Repository{ID: req.RepoID}, nil, false, contentartifacts.AllowanceWholeWindow)
	result.Stats = stats
	if err != nil {
		return "", fmt.Errorf("derive config artifacts of %q: %w", req.RepoID, err)
	}
	rows, err := storedRows(built)
	if err != nil {
		return "", fmt.Errorf("derive config artifacts of %q: %w", req.RepoID, err)
	}
	result.Rows = len(rows)
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("derive config artifacts of %q: deadline before the write: %w", req.RepoID, err)
	}
	return d.write(ctx, req, v0, rows, stats)
}

// logFailure writes the config_artifacts.derive.failed record.
func (d *Deriver) logFailure(ctx context.Context, result Result, recordErr error) {
	if d.logger == nil {
		return
	}
	attrs := []any{
		"event_name", deriveFailedEvent,
		"repo_id", result.RepoID,
		"trigger", string(result.Trigger),
		"outcome", string(result.Outcome),
		"error_class", result.ErrorClass,
		"error", result.Err.Error(),
		"content_version", result.ContentVersion,
		"extractor_version", d.extractorVersion,
		"duration_seconds", result.Duration.Seconds(),
		"failure_recorded", result.FailureRecorded,
	}
	var panicked *panicError
	if errors.As(result.Err, &panicked) {
		attrs = append(attrs, "stack", panicked.stack)
	}
	if recordErr != nil {
		attrs = append(attrs, "failure_record_error", recordErr.Error())
	}
	d.logger.ErrorContext(context.WithoutCancel(ctx), "config artifact derive failed", attrs...)
}
