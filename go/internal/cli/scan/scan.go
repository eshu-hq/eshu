// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scan

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

const (
	// statusEndpoint is the pipeline status route Execute polls to decide
	// whether an index is queryable. It is the only readiness evidence the
	// scan family trusts; process health is not readiness.
	statusEndpoint = "/api/v0/status/pipeline"
	// queryProbeEndpoint is the smallest bounded read that proves the graph
	// answers a question, run once before the bootstrap and once after
	// readiness so the result can distinguish "drained" from "answerable".
	queryProbeEndpoint = "/api/v0/repositories?limit=1"
)

// Client is the narrow API read surface the scan family needs. It exists so
// this package does not have to name go/cmd/eshu's *APIClient, which lives in
// package main and therefore cannot be imported. *APIClient satisfies Client
// as written.
type Client interface {
	// Get performs a GET against the API and decodes the JSON response into
	// result.
	Get(path string, result any) error
}

// Runtime carries the process-owned collaborators Execute needs. Every field
// is supplied by the cobra wrapper in go/cmd/eshu, which owns process contact:
// PATH lookup, the bootstrap child process, the API client, and the inherited
// environment. Tests replace the seams with fakes.
//
// Execute rejects a Runtime missing Client, Environ, LookPath, RunBootstrap,
// FetchStatus, or FetchQueryProbe rather than reaching a nil dereference part
// way through a scan. Now and Wait are pure clock helpers and default to
// time.Now and a cancellable timer when nil.
type Runtime struct {
	// Client reads the status and query-probe endpoints.
	Client Client
	// ServiceURL is the API base URL recorded in Result.Evidence. It is
	// provenance only; Execute never dials it.
	ServiceURL string
	// Environ is the base environment the bootstrap child inherits, before
	// Options.BootstrapEnv applies the scan overrides. A nil Environ is
	// rejected because an empty base silently strips PATH from the child.
	Environ []string
	// LookPath resolves eshu-bootstrap-index on PATH, mirroring
	// exec.LookPath semantics.
	LookPath func(file string) (string, error)
	// RunBootstrap runs the bootstrap-index child process to completion,
	// streaming its output to stdout and stderr.
	RunBootstrap func(ctx context.Context, binary string, args []string, env []string, stdout, stderr io.Writer) error
	// FetchStatus reads the pipeline status report. Production wiring passes
	// FetchPipelineStatus.
	FetchStatus func(client Client) (PipelineStatus, error)
	// FetchQueryProbe runs the bounded query probe. Production wiring passes
	// FetchQueryProbe.
	FetchQueryProbe func(client Client) (map[string]any, error)
	// Now reads the clock used for every reported duration. Defaults to
	// time.Now.
	Now func() time.Time
	// Wait sleeps between readiness polls and must return the context error
	// when the context ends first. Defaults to a cancellable timer.
	Wait func(ctx context.Context, interval time.Duration) error
}

// Options is the resolved scan request. go/cmd/eshu builds it from cobra
// flags; this package never reads a flag itself.
type Options struct {
	// Force re-indexes a source that is already present.
	Force bool
	// JSON selects the canonical JSON envelope. Execute does not render it --
	// the wrapper does -- but the flag also suppresses the human announcement.
	JSON bool
	// Wait keeps polling until the pipeline is drained and healthy. With Wait
	// false the result stops at "submitted".
	Wait bool
	// AllowPartial downgrades a readiness failure to a "partial" result with
	// warnings instead of an error, but only once a health state has been
	// observed.
	AllowPartial bool
	// Timeout bounds the whole scan, including the bootstrap child.
	Timeout time.Duration
	// PollInterval is the readiness polling period.
	PollInterval time.Duration
	// DiscoveryReport is an absolute path the bootstrap child writes a
	// discovery advisory report to. Empty disables the report.
	DiscoveryReport string
	// ReposDir is the repository cache the bootstrap child uses. ReposDir
	// resolves the production value.
	ReposDir string
	// Profile is the config profile name recorded in the truth envelope.
	Profile string
	// Target is the resolved source being scanned.
	Target Target
	// RuntimeEnv, when non-empty, replaces the process environment as the
	// bootstrap child's base. The vuln-scan local runtime uses it to hand the
	// child an owner-managed Postgres and graph endpoint.
	RuntimeEnv []string
}

// Target is the resolved source a scan indexes.
type Target struct {
	// Path is the absolute path the operator named.
	Path string `json:"path"`
	// Root is the workspace root the bootstrap child indexes, which may be an
	// ancestor of Path.
	Root string `json:"root"`
	// Kind is "workspace", "repository", or "directory".
	Kind string `json:"kind"`
}

// Execute runs one scan: preflight the API, run eshu-bootstrap-index against
// the resolved target, then prove the result is queryable rather than merely
// submitted. announce prints a one-line "Scanning <root>..." banner to stdout;
// the bootstrap child's own output always streams to stdout and stderr.
//
// The returned Result is meaningful even when the error is non-nil -- it
// carries the last status report and the evidence gathered so far, which is
// what the JSON envelope reports on failure.
func Execute(
	parentCtx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	rt Runtime,
	opts Options,
	announce bool,
) (Result, error) {
	result := newResult(opts, rt.ServiceURL)
	if err := rt.validate(); err != nil {
		return result, err
	}
	rt = rt.withClockDefaults()

	startedAt := rt.Now()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(parentCtx, opts.Timeout)
	defer cancel()

	preflight, err := rt.FetchStatus(rt.Client)
	if err != nil {
		return result, fmt.Errorf("scan preflight status check: %w", err)
	}
	result.StatusReport = preflight
	queryProbe, err := rt.FetchQueryProbe(rt.Client)
	if err != nil {
		return result, fmt.Errorf("scan preflight query check: %w", err)
	}
	result.QueryProbe = queryProbe

	binary, err := rt.LookPath("eshu-bootstrap-index")
	if err != nil {
		return result, fmt.Errorf("eshu-bootstrap-index not found in PATH: %w", err)
	}
	result.Evidence.BootstrapBinary = binary

	bootstrapStartedAt := rt.Now()
	if announce {
		_, _ = fmt.Fprintf(stdout, "Scanning %s...\n", opts.Target.Root)
	}
	if err := rt.RunBootstrap(
		runCtx,
		binary,
		opts.BootstrapArgs(),
		opts.BootstrapEnv(rt.Environ),
		stdout,
		stderr,
	); err != nil {
		return result, fmt.Errorf("run bootstrap index: %w", err)
	}
	bootstrapCompletedAt := rt.Now()
	result.Timings.BootstrapCompleteMS = durationMillis(bootstrapCompletedAt.Sub(bootstrapStartedAt))

	if !opts.Wait {
		result.Status = "submitted"
		result.Truth = Truth("stale", "partial", opts.Profile, CurrentGraphBackend())
		return result, nil
	}

	readyResult, err := waitForReadiness(runCtx, rt, opts, result, startedAt, bootstrapCompletedAt)
	if err != nil {
		if opts.AllowPartial && readyResult.StatusReport.Health.State != "" {
			readyResult.Status = "partial"
			readyResult.Truth = Truth("stale", "partial", opts.Profile, CurrentGraphBackend())
			readyResult.Warnings = append(readyResult.Warnings, err.Error())
			return readyResult, nil
		}
		return readyResult, err
	}
	readyResult.Status = "ready"
	queryProbe, err = rt.FetchQueryProbe(rt.Client)
	if err != nil {
		return readyResult, fmt.Errorf("scan query readiness check: %w", err)
	}
	readyResult.QueryProbe = queryProbe
	readyResult.Truth = Truth("current", "complete", opts.Profile, CurrentGraphBackend())
	return readyResult, nil
}

// validate rejects a Runtime missing a seam this package cannot supply itself,
// naming the field so the caller sees the wiring gap instead of a nil panic.
func (r Runtime) validate() error {
	switch {
	case r.Client == nil:
		return fmt.Errorf("scan: Runtime.Client is required")
	case r.Environ == nil:
		return fmt.Errorf("scan: Runtime.Environ is required")
	case r.LookPath == nil:
		return fmt.Errorf("scan: Runtime.LookPath is required")
	case r.RunBootstrap == nil:
		return fmt.Errorf("scan: Runtime.RunBootstrap is required")
	case r.FetchStatus == nil:
		return fmt.Errorf("scan: Runtime.FetchStatus is required")
	case r.FetchQueryProbe == nil:
		return fmt.Errorf("scan: Runtime.FetchQueryProbe is required")
	}
	return nil
}

// withClockDefaults fills the two pure clock seams. They are optional because
// neither touches the process, the network, or PATH.
func (r Runtime) withClockDefaults() Runtime {
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.Wait == nil {
		r.Wait = waitInterval
	}
	return r
}

// waitInterval sleeps for interval, returning early with the context error if
// the context ends first. A non-positive interval returns immediately.
func waitInterval(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return nil
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // callers match on context.Canceled/DeadlineExceeded
	case <-timer.C:
		return nil
	}
}

// FetchPipelineStatus reads the pipeline status report through client. It is
// the production value for Runtime.FetchStatus, and go/cmd/eshu's first-run
// flow also calls it directly to decide whether an existing index is reusable.
func FetchPipelineStatus(client Client) (PipelineStatus, error) {
	var status PipelineStatus
	if err := client.Get(statusEndpoint, &status); err != nil {
		return PipelineStatus{}, err //nolint:wrapcheck // the wrapper's error text is the operator-visible contract
	}
	return status, nil
}

// FetchQueryProbe runs the bounded repository read that proves the graph
// answers a question. It is the production value for Runtime.FetchQueryProbe.
func FetchQueryProbe(client Client) (map[string]any, error) {
	var probe map[string]any
	if err := client.Get(queryProbeEndpoint, &probe); err != nil {
		return nil, err //nolint:wrapcheck // the wrapper's error text is the operator-visible contract
	}
	return probe, nil
}

// newResult seeds a Result that already reports failure, so an early return
// never reads as success. The two warnings state the timings the scan command
// cannot measure today rather than reporting them as zero.
func newResult(opts Options, serviceURL string) Result {
	return Result{
		Command: "scan",
		Status:  "failed",
		Target:  opts.Target,
		Evidence: Evidence{
			ServiceURL:     serviceURL,
			StatusEndpoint: statusEndpoint,
			QueryEndpoint:  queryProbeEndpoint,
		},
		Warnings: []string{
			"collector_complete_ms is unavailable because eshu-bootstrap-index does not emit a structured parent-process collector timestamp yet",
			"projection_complete_ms is unavailable because source-local projection completion is only logged by the bootstrap child today",
		},
	}
}

// ReposDir resolves the repository cache the bootstrap child writes to for a
// workspace root: the <cache>/repos directory of the Eshu local layout.
//
// This reads the environment through eshulocal.BuildLayout, to which it passes
// os.Getenv and os.UserHomeDir. See the package README for the exact set of
// variables that resolution can consult.
func ReposDir(root string) (string, error) {
	layout, err := eshulocal.BuildLayout(os.Getenv, os.UserHomeDir, runtime.GOOS, root)
	if err != nil {
		return "", fmt.Errorf("resolve scan cache: %w", err)
	}
	return filepath.Join(layout.CacheDir, "repos"), nil
}

// ResolveTarget turns an operator-supplied path into the absolute path plus the
// workspace root the bootstrap child indexes. explicitRoot, when non-empty,
// overrides root detection and forces the "workspace" kind.
func ResolveTarget(path, explicitRoot string) (Target, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Target{}, err //nolint:wrapcheck // preserves the CLI's existing operator-visible message
	}
	root, err := eshulocal.ResolveWorkspaceRoot(absPath, explicitRoot)
	if err != nil {
		return Target{}, err //nolint:wrapcheck // preserves the CLI's existing operator-visible message
	}
	return Target{
		Path: absPath,
		Root: root,
		Kind: TargetKind(root, strings.TrimSpace(explicitRoot) != ""),
	}, nil
}

// TargetKind classifies a resolved root. An operator-supplied root is always a
// workspace; otherwise a .eshu.yaml marks a workspace, a .git directory marks a
// repository, and anything else is a plain directory.
func TargetKind(root string, explicit bool) string {
	if explicit {
		return "workspace"
	}
	if pathExists(filepath.Join(root, ".eshu.yaml")) {
		return "workspace"
	}
	if pathExists(filepath.Join(root, ".git")) {
		return "repository"
	}
	return "directory"
}

// Truth builds the truth envelope the scan family reports alongside a result.
// Every scan-derived answer is runtime-level: it describes what the pipeline
// has projected, not an exact graph proof.
func Truth(freshness, completeness, profile, backend string) map[string]any {
	return map[string]any{
		"level":        "runtime",
		"freshness":    freshness,
		"completeness": completeness,
		"profile":      profile,
		"backend":      backend,
	}
}

// CurrentGraphBackend reports the configured graph backend for the truth
// envelope, reading ESHU_GRAPH_BACKEND. It returns "unknown" rather than
// guessing a default, so a truth envelope never claims a backend that was
// never configured.
func CurrentGraphBackend() string {
	if backend := strings.TrimSpace(os.Getenv("ESHU_GRAPH_BACKEND")); backend != "" {
		return backend
	}
	return "unknown"
}

// BootstrapArgs builds the bootstrap-index argv, including argv[0].
func (o Options) BootstrapArgs() []string {
	args := []string{"eshu-bootstrap-index", "--path", o.Target.Root}
	if o.Force {
		args = append(args, "--force")
	}
	return args
}

// BootstrapEnv builds the environment for the bootstrap child. base is the
// process environment the child would otherwise inherit; o.RuntimeEnv replaces
// it entirely when set, because a caller-supplied runtime environment already
// describes a complete, deliberately isolated child. The scan overrides always
// win over both.
func (o Options) BootstrapEnv(base []string) []string {
	overrides := map[string]string{
		"ESHU_REPO_SOURCE_MODE":  "filesystem",
		"ESHU_FILESYSTEM_ROOT":   o.Target.Root,
		"ESHU_FILESYSTEM_DIRECT": "true",
		"ESHU_REPOS_DIR":         o.ReposDir,
	}
	if strings.TrimSpace(o.DiscoveryReport) != "" {
		overrides["ESHU_DISCOVERY_REPORT"] = o.DiscoveryReport
	}
	if len(o.RuntimeEnv) > 0 {
		base = append([]string(nil), o.RuntimeEnv...)
	}
	return mergeEnv(base, overrides)
}

// mergeEnv folds overrides onto a KEY=VALUE base, last value winning. It is a
// copy of go/cmd/eshu's mergeEnvironment, which stays there for the seven
// callers outside the scan family.
func mergeEnv(base []string, overrides map[string]string) []string {
	merged := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				merged[entry[:i]] = entry[i+1:]
				break
			}
		}
	}
	for key, value := range overrides {
		merged[key] = value
	}
	env := make([]string, 0, len(merged))
	for key, value := range merged {
		env = append(env, key+"="+value)
	}
	return env
}

// pathExists reports whether a path is present. It is a copy of go/cmd/eshu's
// pathExists, which stays there for the first-run runtime probe.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// durationMillis renders a duration for the result's timing fields.
func durationMillis(d time.Duration) int64 {
	return d.Milliseconds()
}
