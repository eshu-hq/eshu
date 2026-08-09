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
	"path/filepath"
	"strings"
	"time"
)

// defaultDemoProject is the Compose project name `eshu demo` owns. It is
// deliberately distinct from the default local stack so the demo can never
// adopt, restart, or tear down a stack the operator started for real work.
const defaultDemoProject = "eshu-demo"

// demoComposeFileName is the credential-free overlay (#4742). It wraps the
// corpus and runtime fragments so the demo entrypoint stays one file.
const demoComposeFileName = "docker-compose.demo.yaml"

// demoReadyTimeout bounds the wait for indexing completeness. The demo corpus
// is the 20-repository golden-corpus replay, so a wait this long means
// something is wrong rather than merely slow.
const demoReadyTimeout = 10 * time.Minute

// demoUpTimeout bounds `docker compose up -d --wait`. Compose can wedge with
// no containers created and no progress -- observed at 0% CPU for 16 minutes
// while measuring TTFA (#4744) -- and an unbounded phase turns that into a
// hang with no diagnosis instead of a failure that names itself.
const demoUpTimeout = 20 * time.Minute

// demoReadyPollInterval is how often readiness is sampled while waiting.
const demoReadyPollInterval = 2 * time.Second

// demoHTTPTimeout bounds every request the demo makes to its own stack.
//
// http.DefaultClient has no timeout, so a stack that accepts the connection
// and then stalls before sending headers would hang the readiness loop past
// its own deadline -- the poll interval only spaces retries, it does not bound
// a request already in flight.
const demoHTTPTimeout = 30 * time.Second

// demoHTTPClient is the bounded client for all demo reads.
var demoHTTPClient = &http.Client{Timeout: demoHTTPTimeout}

// The demo overlay binds ${ESHU_DEMO_BIND_ADDR:-127.0.0.1} with
// ${ESHU_DEMO_API_PORT:-18080} and ${ESHU_DEMO_MCP_PORT:-18091}. These read
// the same variables so a second demo that moves its ports to avoid the first
// one's is still probed where it actually listens.
//
// They are deliberately not 8080/8081: the demo runs its own Compose project
// so it never touches the operator's default stack, and an earlier draft
// hardcoded the default ports, which meant readiness read whatever was already
// listening there.
const (
	demoDefaultAPIPort  = "18080"
	demoDefaultMCPPort  = "18091"
	demoDefaultBindAddr = "127.0.0.1"
)

// demoAPIBase is the demo stack's HTTP API base URL.
func demoAPIBase() string { return demoBase(demoDefaultAPIPort, "ESHU_DEMO_API_PORT") }

// demoMCPBase is the demo stack's MCP base URL.
func demoMCPBase() string { return demoBase(demoDefaultMCPPort, "ESHU_DEMO_MCP_PORT") }

func demoBase(defaultPort, portEnv string) string {
	host := os.Getenv("ESHU_DEMO_BIND_ADDR")
	if strings.TrimSpace(host) == "" {
		host = demoDefaultBindAddr
	}
	port := os.Getenv(portEnv)
	if strings.TrimSpace(port) == "" {
		port = defaultPort
	}
	return "http://" + host + ":" + port
}

// resolveDemoComposeFile walks up from dir looking for the overlay, so an
// installed binary invoked outside the repository root still finds it.
// ESHU_DEMO_COMPOSE_FILE overrides the search entirely.
func resolveDemoComposeFile(dir string) (string, error) {
	// ESHU_DEMO_COMPOSE_FILE is an operator-set override for where their own
	// demo overlay lives, the same trust level as the working directory this
	// function otherwise searches. The value is handed to docker compose -f,
	// never opened or included by this process, and an operator who can set it
	// can already run docker directly.
	if override := strings.TrimSpace(os.Getenv("ESHU_DEMO_COMPOSE_FILE")); override != "" {
		return override, nil
	}
	current := dir
	for {
		candidate := filepath.Join(current, demoComposeFileName)
		// #nosec G703 -- candidate is the walked directory plus a constant
		// filename; Stat only tests existence and the path is never opened here
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf(
				"could not find %s in %s or any parent directory\n"+
					"run eshu demo from an Eshu checkout, or set ESHU_DEMO_COMPOSE_FILE to the overlay path",
				demoComposeFileName, dir)
		}
		current = parent
	}
}

// demoIndexStatus is the subset of /api/v0/status/index the demo waits on.
// Readiness is indexing completeness, never process health: a stack that is
// merely "up" answers the five demo questions wrongly or not at all.
type demoIndexStatus struct {
	// Status is the service's own health verdict ("healthy" when settled).
	Status string `json:"status"`
	// RepositoryCount is how many repositories are indexed so far.
	RepositoryCount int `json:"repository_count"`
	// Queue carries the outstanding backlog. Readiness is "no work left",
	// which is what distinguishes a settled stack from a merely running one.
	Queue struct {
		Outstanding int `json:"outstanding"`
	} `json:"queue"`
}

// Complete reports whether the demo can answer correctly.
//
// These field names are read from the live response, not assumed: the route
// returns status/repository_count/queue, and an earlier draft looked for
// "complete" and "repositories", which do not exist. It therefore reported
// zero repositories forever against a healthy stack.
//
// Ready means the service calls itself healthy, at least one repository is
// indexed, and no queued work remains. Dropping the queue check would let the
// demo ask its question mid-projection and get a thin answer.
func (s demoIndexStatus) Complete() bool {
	return s.Status == "healthy" && s.RepositoryCount > 0 && s.Queue.Outstanding == 0
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

// demoProbeFunc reads indexing completeness. It takes the bearer because the
// status routes require authentication: an unauthenticated probe gets 401 and
// can never report ready, however healthy the stack is.
type demoProbeFunc func(ctx context.Context, apiBase, apiKey string) (demoIndexStatus, error)

// demoAskFunc asks the manifest's first question against a running demo stack.
type demoAskFunc func(ctx context.Context, apiBase, apiKey string) (demoAnswer, error)

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
	// upTimeout bounds the compose bring-up phase. Injected so tests exercise
	// the wedged-compose path without waiting the real budget; zero means
	// demoUpTimeout.
	upTimeout time.Duration
	// pollInterval is how long waitReady sleeps between readiness samples.
	// Injected so unit tests exercise the multi-poll path without spending
	// real seconds; zero means demoReadyPollInterval.
	pollInterval time.Duration
	// apiKey is the ephemeral per-run credential handed to the stack and
	// reused as the bearer on this command's own MCP call.
	apiKey string
	// composeFile is the resolved overlay path.
	composeFile string
}

// allImagesPresent reports whether every image the demo stack declares is
// already local.
//
// The names come from `compose config --images` rather than a hardcoded list,
// so a new service cannot fall outside the check, and one `docker image
// inspect` covers them all: it exits non-zero when any argument is missing.
func (r *demoRuntime) allImagesPresent(ctx context.Context) (bool, error) {
	out, err := r.exec(ctx, nil, "docker", r.composeArgs("config", "--images")...)
	if err != nil {
		return false, fmt.Errorf("list demo images (project %q): %w", r.project, err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return false, nil
	}
	if _, err := r.exec(ctx, nil, "docker", append([]string{"image", "inspect"}, names...)...); err != nil {
		return false, nil
	}
	return true, nil
}

// composeUpTimeout returns the effective bring-up budget.
func (r *demoRuntime) composeUpTimeout() time.Duration {
	if r.upTimeout > 0 {
		return r.upTimeout
	}
	return demoUpTimeout
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
		apiBase: demoAPIBase(),
	}
}

// newResolvedDemoRuntime builds a runtime with the overlay located relative to
// the working directory, so an installed binary works outside the repo root.
func newResolvedDemoRuntime(project string) (*demoRuntime, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	file, err := resolveDemoComposeFile(cwd)
	if err != nil {
		return nil, err
	}
	rt := newDemoRuntime(project)
	rt.composeFile = file
	return rt, nil
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
	file := r.composeFile
	if file == "" {
		file = demoComposeFileName
	}
	return append([]string{"compose", "-p", r.project, "-f", file}, rest...)
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
	// Build separately from up, but only when something actually needs
	// building. `up -d --wait` otherwise covers image build, container start,
	// corpus bootstrap, and the reducer drain in one bucket, and a regression
	// in any of them looks the same from outside.
	//
	// The guard matters: an unconditional `docker compose build` revalidates
	// every build context even when nothing changed, which measured 221,590 ms
	// on an otherwise warm run. Instrumentation that slows the thing it
	// measures is worse than the missing attribution it was added to fix.
	res.PhaseMillis["build"] = 0
	present, presentErr := r.allImagesPresent(ctx)
	if presentErr != nil {
		return res, presentErr
	}
	if !present {
		buildCtx, cancelBuild := context.WithTimeout(ctx, r.composeUpTimeout())
		buildOut, buildErr := r.exec(buildCtx, []string{"ESHU_DEMO_API_KEY=" + key}, "docker", r.composeArgs("build")...)
		cancelBuild()
		if buildErr != nil {
			return res, fmt.Errorf("build demo images (project %q): %w\n%s",
				r.project, buildErr, strings.TrimSpace(string(buildOut)))
		}
		res.PhaseMillis["build"] = r.sinceMillis(phaseStart)
	}

	phaseStart = r.now()
	upCtx, cancelUp := context.WithTimeout(ctx, r.composeUpTimeout())
	out, err := r.exec(upCtx, []string{"ESHU_DEMO_API_KEY=" + key}, "docker", r.composeArgs("up", "-d", "--wait")...)
	cancelUp()
	if err != nil {
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
	answer, err := r.ask(ctx, r.apiBase, r.apiKey)
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
		status, err := r.probe(ctx, r.apiBase, r.apiKey)
		if err == nil {
			last = status
			if status.Complete() {
				return nil
			}
		}
		if !r.now().Before(deadline) {
			return fmt.Errorf(
				"demo stack did not finish indexing within %s (last seen: %d repositories, complete=%v)\n"+
					"inspect it with `docker compose -p %s -f %s logs`",
				demoReadyTimeout, last.RepositoryCount, last.Complete(), r.project, r.composeFile)
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
func probeDemoIndexStatus(ctx context.Context, apiBase, apiKey string) (demoIndexStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/api/v0/status/index", nil)
	if err != nil {
		return demoIndexStatus{}, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := demoHTTPClient.Do(req)
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
