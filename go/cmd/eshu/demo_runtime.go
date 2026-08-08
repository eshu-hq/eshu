// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// defaultDemoProject is the Compose project name `eshu demo` owns. It is
// deliberately distinct from the default local stack so the demo can never
// adopt, restart, or tear down a stack the operator started for real work.
const defaultDemoProject = "eshu-demo"

// demoComposeFile is the credential-free overlay (#4742). It wraps the corpus
// and runtime fragments so the demo entrypoint stays one file.
const demoComposeFile = "docker-compose.demo.yaml"

// demoReadyTimeout bounds the wait for indexing completeness. The demo corpus
// is the 20-repository golden-corpus replay, so a wait this long means
// something is wrong rather than merely slow.
const demoReadyTimeout = 10 * time.Minute

// demoReadyPollInterval is how often readiness is sampled while waiting.
const demoReadyPollInterval = 2 * time.Second

// demoMCPBase is where the demo overlay publishes the MCP server. The demo
// owns its own Compose project, so these are the demo's ports, not the
// operator's default stack.
const demoMCPBase = "http://127.0.0.1:8081"

// demoIndexStatus is the subset of /api/v0/status/index the demo waits on.
// Readiness is indexing completeness, never process health: a stack that is
// merely "up" answers the five demo questions wrongly or not at all.
type demoIndexStatus struct {
	// Complete reports whether indexing finished for every demo repository.
	Complete bool `json:"complete"`
	// Repositories is how many repositories are indexed so far, for progress.
	Repositories int `json:"repositories"`
}

// demoAnswer is the first correlated answer the demo proves, with the truth
// labels that make it evidence rather than prose.
type demoAnswer struct {
	// Question is the manifest question asked (specs/demo-first-answers.v1.yaml).
	Question string `json:"question"`
	// Answer is the rendered answer text.
	Answer string `json:"answer"`
	// Truth carries freshness/completeness/backend provenance for the answer.
	Truth map[string]any `json:"truth"`
}

// demoResult is the machine-readable outcome of `eshu demo up`.
type demoResult struct {
	// Project is the Compose project the demo owns for this run.
	Project string `json:"project"`
	// Ready reports whether indexing completeness was reached.
	Ready bool `json:"ready"`
	// FirstAnswer is the guided first question and its answer.
	FirstAnswer demoAnswer `json:"first_answer"`
	// PhaseMillis records per-phase wall time. This is the raw input the TTFA
	// measurement lane (#4744) consumes; it is emitted here so the timings come
	// from the command that actually owns the lifecycle.
	PhaseMillis map[string]int64 `json:"phase_millis"`
	// TotalMillis is wall time from preflight to first answer.
	TotalMillis int64 `json:"total_millis"`
}

// demoExecFunc shells out with an explicit extra environment. Compose needs
// the ephemeral demo key in its env, so the seam carries it rather than the
// command mutating the process environment.
type demoExecFunc func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)

// demoProbeFunc reads indexing completeness from a running demo stack.
type demoProbeFunc func(ctx context.Context, apiBase string) (demoIndexStatus, error)

// demoAskFunc asks the manifest's first question against a running demo stack.
type demoAskFunc func(ctx context.Context, apiBase, question string) (demoAnswer, error)

// demoRuntime owns the demo Compose lifecycle. Every side effect is behind an
// injectable seam, matching the first_run runtime probe pattern, so the
// refuse-to-clobber and teardown invariants are unit-testable.
type demoRuntime struct {
	exec    demoExecFunc
	probe   demoProbeFunc
	ask     demoAskFunc
	now     func() time.Time
	project string
	apiBase string
	// pollInterval is how long waitReady sleeps between readiness samples.
	// Injected so unit tests exercise the multi-poll path without spending
	// real seconds; zero means demoReadyPollInterval.
	pollInterval time.Duration
	// apiKey is the ephemeral per-run credential handed to the stack and
	// reused as the bearer on this command's own MCP call.
	apiKey string
}

// readyPollInterval returns the effective sleep between readiness samples.
func (r *demoRuntime) readyPollInterval() time.Duration {
	if r.pollInterval > 0 {
		return r.pollInterval
	}
	return demoReadyPollInterval
}

// newDemoRuntime builds a runtime wired to the real Docker CLI and HTTP API.
func newDemoRuntime(project string) *demoRuntime {
	return &demoRuntime{
		exec:    runDemoCommand,
		probe:   probeDemoIndexStatus,
		ask:     askDemoQuestion,
		now:     time.Now,
		project: project,
		apiBase: "http://127.0.0.1:8080",
	}
}

// runDemoCommand is the production exec seam.
func runDemoCommand(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- args are program-constructed compose invocations, never user text
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.CombinedOutput()
}

// newEphemeralDemoKey mints a per-run credential for the demo stack.
//
// The demo runtime overlay refuses to start mcp-server with no resolvable
// credential source (#5168, deliberate). "Zero credentials" is a promise to the
// operator, not to the stack, so the command mints one, uses it, and throws it
// away with the stack rather than asking the operator for one or leaving the
// MCP port open.
func newEphemeralDemoKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mint ephemeral demo key: %w", err)
	}
	return "demo-" + hex.EncodeToString(buf), nil
}

// composeArgs builds the project-scoped compose argument prefix. Every compose
// call in this file goes through it so no code path can act on the operator's
// default stack by omitting -p.
func (r *demoRuntime) composeArgs(rest ...string) []string {
	return append([]string{"compose", "-p", r.project, "-f", demoComposeFile}, rest...)
}

// preflight proves Docker is usable before anything is started. A missing
// daemon or binary is reported with what was probed and what to do, following
// the first_run_diagnostics precedent, rather than surfacing a raw exec error.
func (r *demoRuntime) preflight(ctx context.Context) error {
	if _, err := r.exec(ctx, nil, "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		return fmt.Errorf(
			"docker is not available or its daemon is not running (probed: docker version): %w\n"+
				"eshu demo needs a running Docker daemon. Start Docker Desktop (or dockerd) and retry", err)
	}
	return nil
}

// alreadyRunning reports whether the demo project already has containers. It
// is the guard behind the invariant that `eshu demo up` never adopts or
// destroys a stack it did not start.
func (r *demoRuntime) alreadyRunning(ctx context.Context) (bool, error) {
	out, err := r.exec(ctx, nil, "docker", r.composeArgs("ps", "--quiet")...)
	if err != nil {
		// A compose failure here is not proof of absence, so fail closed
		// rather than assuming the project is free and clobbering it.
		return false, fmt.Errorf("could not determine whether project %q is already running: %w", r.project, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// up brings the demo stack to a first correlated answer. Phases are timed
// individually so the TTFA lane has per-phase attribution rather than one
// opaque total.
func (r *demoRuntime) up(ctx context.Context) (demoResult, error) {
	res := demoResult{Project: r.project, PhaseMillis: map[string]int64{}}
	start := r.now()

	phaseStart := start
	if err := r.preflight(ctx); err != nil {
		return res, err
	}
	res.PhaseMillis["preflight"] = r.sinceMillis(phaseStart)

	running, err := r.alreadyRunning(ctx)
	if err != nil {
		return res, err
	}
	if running {
		return res, fmt.Errorf(
			"compose project %q is already running; eshu demo will not take over a stack it did not start\n"+
				"run `eshu demo down` if it is a previous demo, or pass --project <name> to use a separate one", r.project)
	}

	phaseStart = r.now()
	key, err := newEphemeralDemoKey()
	if err != nil {
		return res, err
	}
	r.apiKey = key
	if out, err := r.exec(ctx, []string{"ESHU_DEMO_API_KEY=" + key}, "docker", r.composeArgs("up", "-d", "--wait")...); err != nil {
		// Carry the compose output. Reporting only "exit status 1" forces the
		// operator to re-run compose by hand to find the cause, which is what
		// happened the first time this ran against a real stack.
		return res, fmt.Errorf("bring up demo stack (project %q): %w\n%s", r.project, err, strings.TrimSpace(string(out)))
	}
	res.PhaseMillis["up"] = r.sinceMillis(phaseStart)

	phaseStart = r.now()
	if err := r.waitReady(ctx); err != nil {
		return res, err
	}
	res.Ready = true
	res.PhaseMillis["ready"] = r.sinceMillis(phaseStart)

	phaseStart = r.now()
	answer, err := r.ask(ctx, r.apiBase, demoFirstQuestion)
	if err != nil {
		return res, fmt.Errorf("ask the first demo question: %w", err)
	}
	res.FirstAnswer = answer
	res.PhaseMillis["first_answer"] = r.sinceMillis(phaseStart)

	res.TotalMillis = r.sinceMillis(start)
	return res, nil
}

// waitReady polls indexing completeness until the demo can answer correctly.
func (r *demoRuntime) waitReady(ctx context.Context) error {
	deadline := r.now().Add(demoReadyTimeout)
	var last demoIndexStatus
	for {
		status, err := r.probe(ctx, r.apiBase)
		if err == nil {
			last = status
			if status.Complete {
				return nil
			}
		}
		if !r.now().Before(deadline) {
			return fmt.Errorf(
				"demo stack did not finish indexing within %s (last seen: %d repositories, complete=%v)\n"+
					"inspect it with `docker compose -p %s -f %s logs`",
				demoReadyTimeout, last.Repositories, last.Complete, r.project, demoComposeFile)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.readyPollInterval()):
		}
	}
}

// sinceMillis measures elapsed wall time from t using the injected clock.
func (r *demoRuntime) sinceMillis(t time.Time) int64 {
	ms := r.now().Sub(t).Milliseconds()
	if ms <= 0 {
		// A monotonic clock can report 0 for a fast phase; the TTFA lane reads
		// these as durations, so never emit a negative or absent value.
		return 1
	}
	return ms
}

// probeDemoIndexStatus is the production readiness seam.
func probeDemoIndexStatus(ctx context.Context, apiBase string) (demoIndexStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/api/v0/status/index", nil)
	if err != nil {
		return demoIndexStatus{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return demoIndexStatus{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return demoIndexStatus{}, fmt.Errorf("index status: HTTP %d", resp.StatusCode)
	}
	var status demoIndexStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return demoIndexStatus{}, fmt.Errorf("decode index status: %w", err)
	}
	return status, nil
}
