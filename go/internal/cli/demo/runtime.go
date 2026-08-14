// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultProject is the Compose project name `eshu demo` owns. It is
// deliberately distinct from the default local stack so the demo can never
// adopt, restart, or tear down a stack the operator started for real work.
const DefaultProject = "eshu-demo"

// ComposeFileName is the credential-free overlay (#4742). It wraps the
// corpus and runtime fragments so the demo entrypoint stays one file.
const ComposeFileName = "docker-compose.demo.yaml"

// readyTimeout bounds the wait for indexing completeness. The demo corpus
// is the 20-repository golden-corpus replay, so a wait this long means
// something is wrong rather than merely slow.
const readyTimeout = 10 * time.Minute

// upTimeout bounds `docker compose up -d --wait`. Compose can wedge with
// no containers created and no progress -- observed at 0% CPU for 16 minutes
// while measuring TTFA (#4744) -- and an unbounded phase turns that into a
// hang with no diagnosis instead of a failure that names itself.
const upTimeout = 20 * time.Minute

// readyPollInterval is how often readiness is sampled while waiting.
const readyPollInterval = 2 * time.Second

// httpTimeout bounds every request the demo makes to its own stack.
//
// http.DefaultClient has no timeout, so a stack that accepts the connection
// and then stalls before sending headers would hang the readiness loop past
// its own deadline -- the poll interval only spaces retries, it does not bound
// a request already in flight.
const httpTimeout = 30 * time.Second

// httpClient is the bounded client for all demo reads.
var httpClient = &http.Client{Timeout: httpTimeout}

// ExecFunc shells out with an explicit extra environment. Compose needs
// the ephemeral demo key in its env, so the seam carries it rather than the
// command mutating the process environment.
type ExecFunc func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)

// ProbeFunc reads indexing completeness. It takes the bearer because the
// status routes require authentication: an unauthenticated probe gets 401 and
// can never report ready, however healthy the stack is.
type ProbeFunc func(ctx context.Context, apiBase, apiKey string) (IndexStatus, error)

// AskFunc asks the manifest's first question against a running demo stack.
type AskFunc func(ctx context.Context, apiBase, apiKey string) (Answer, error)

// Options are the resolved inputs a Runtime needs. The caller resolves every
// one of them -- go/cmd/eshu's demo.go reads the --project flag and the
// ESHU_DEMO_* environment variables -- so this package never reads a cobra
// flag or the process environment to decide where the stack lives.
type Options struct {
	// Project is the Compose project name this run owns.
	Project string
	// ComposeFile is the resolved overlay path, from ResolveComposeFile.
	ComposeFile string
	// APIBase is the demo stack's HTTP API base URL, from APIBase.
	APIBase string
	// MCPBase is the demo stack's MCP base URL, from MCPBase.
	MCPBase string
}

// Runtime owns the demo Compose lifecycle. Every side effect is behind an
// injectable seam, matching the first_run runtime probe pattern, so the
// refuse-to-clobber and teardown invariants are unit-testable.
type Runtime struct {
	exec    ExecFunc
	probe   ProbeFunc
	ask     AskFunc
	now     func() time.Time
	project string
	apiBase string
	mcpBase string
	// upTimeout bounds the compose bring-up phase. Injected so tests exercise
	// the wedged-compose path without waiting the real budget; zero means the
	// package-level upTimeout.
	upTimeout time.Duration
	// pollInterval is how long waitReady sleeps between readiness samples.
	// Injected so unit tests exercise the multi-poll path without spending
	// real seconds; zero means readyPollInterval.
	pollInterval time.Duration
	// apiKey is the ephemeral per-run credential handed to the stack and
	// reused as the bearer on this command's own MCP call.
	apiKey string
	// composeFile is the resolved overlay path.
	composeFile string
}

// NewRuntime builds a runtime wired to the real Docker CLI and HTTP API.
func NewRuntime(opts Options) *Runtime {
	r := &Runtime{
		exec:        runCommand,
		probe:       probeIndexStatus,
		now:         time.Now,
		project:     opts.Project,
		apiBase:     opts.APIBase,
		mcpBase:     opts.MCPBase,
		composeFile: opts.ComposeFile,
	}
	r.ask = func(ctx context.Context, apiBase, apiKey string) (Answer, error) {
		return AskQuestion(ctx, apiBase, r.mcpBase, apiKey)
	}
	return r
}

// allImagesPresent reports whether every image the demo stack declares is
// already local.
//
// The names come from `compose config --images` rather than a hardcoded list,
// so a new service cannot fall outside the check, and one `docker image
// inspect` covers them all: it exits non-zero when any argument is missing.
func (r *Runtime) allImagesPresent(ctx context.Context) (bool, error) {
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
func (r *Runtime) composeUpTimeout() time.Duration {
	if r.upTimeout > 0 {
		return r.upTimeout
	}
	return upTimeout
}

// effectivePollInterval returns the effective sleep between readiness samples.
func (r *Runtime) effectivePollInterval() time.Duration {
	if r.pollInterval > 0 {
		return r.pollInterval
	}
	return readyPollInterval
}

// runCommand is the production exec seam. The subprocess inherits this
// process's environment (so docker resolves through PATH and reads
// DOCKER_HOST) plus the caller's extra entries; nothing here reads the
// environment to make a decision.
//
// The raw exec failure is the operator-visible cause. Every caller already
// wraps it with the phase and the project name, so a second wrap here would
// change the text an operator reads while diagnosing a wedged stack.
//
//nolint:wrapcheck // callers already add phase and project; a second wrap changes their text
func runCommand(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- args are program-constructed compose invocations, never user text
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.CombinedOutput()
}

// newEphemeralKey mints a per-run credential for the demo stack.
//
// The demo runtime overlay refuses to start mcp-server with no resolvable
// credential source (#5168, deliberate). "Zero credentials" is a promise to the
// operator, not to the stack, so the command mints one, uses it, and throws it
// away with the stack rather than asking the operator for one or leaving the
// MCP port open.
func newEphemeralKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mint ephemeral demo key: %w", err)
	}
	return "demo-" + hex.EncodeToString(buf), nil
}

// composeArgs builds the project-scoped compose argument prefix. Every compose
// call in this package goes through it so no code path can act on the
// operator's default stack by omitting -p.
func (r *Runtime) composeArgs(rest ...string) []string {
	file := r.composeFile
	if file == "" {
		file = ComposeFileName
	}
	return append([]string{"compose", "-p", r.project, "-f", file}, rest...)
}

// preflight proves Docker is usable before anything is started. A missing
// daemon or binary is reported with what was probed and what to do, following
// the first_run_diagnostics precedent, rather than surfacing a raw exec error.
func (r *Runtime) preflight(ctx context.Context) error {
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
func (r *Runtime) alreadyRunning(ctx context.Context) (bool, error) {
	out, err := r.exec(ctx, nil, "docker", r.composeArgs("ps", "--quiet")...)
	if err != nil {
		// A compose failure here is not proof of absence, so fail closed
		// rather than assuming the project is free and clobbering it.
		return false, fmt.Errorf("could not determine whether project %q is already running: %w", r.project, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}
