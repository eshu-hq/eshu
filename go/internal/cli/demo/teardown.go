// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Down removes the demo stack along with its volumes and networks.
//
// `-v` and `--remove-orphans` are not optional niceties: the acceptance
// criteria for the demo are zero leaked containers, volumes, or networks, and
// a plain `compose down` leaves named volumes behind. Every invocation is
// scoped to the demo project, so a stack the demo did not create is out of
// reach here by construction rather than by care.
func (r *Runtime) Down(ctx context.Context) error {
	// Up refuses to adopt a project it did not start; Down holds the same
	// line, or --project pointed at an operator's stack removes it along with
	// its volumes.
	owned, err := r.ownsProject(ctx)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf(
			"compose project %q was not created from %s; refusing to remove a stack eshu demo did not start",
			r.project, ComposeFileName)
	}
	if _, err := r.exec(ctx, nil, "docker", r.composeArgs("down", "-v", "--remove-orphans")...); err != nil {
		return fmt.Errorf("remove demo stack (project %q): %w", r.project, err)
	}
	return nil
}

// ownsProject reports whether this project was created from the demo overlay.
//
// Compose stamps every container with the config files that created it, so the
// label is evidence rather than an assumption. An earlier attempt at this guard
// always returned true, which is worse than no guard at all -- project
// membership alone does not prove this command created the stack.
//
// A project with no containers counts as owned: a down after a partial or
// failed up still needs to clean up.
func (r *Runtime) ownsProject(ctx context.Context) (bool, error) {
	out, err := r.exec(ctx, nil, "docker", "ps", "--all",
		"--filter", "label=com.docker.compose.project="+r.project,
		"--format", "{{.Label \"com.docker.compose.project.config_files\"}}")
	if err != nil {
		return false, fmt.Errorf("check ownership of project %q: %w", r.project, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !configFilesNameTheOverlay(line) {
			return false, nil
		}
	}
	return true, nil
}

// Status reports whether the demo project is running and finished indexing.
//
// Running and ready are deliberately separate: a stack that is up but still
// indexing answers the demo questions incompletely, so reporting "running" as
// "ready" would be the same health-vs-completeness mistake first-run refuses
// to make.
func (r *Runtime) Status(ctx context.Context) (Result, error) {
	res := Result{Project: r.project, PhaseMillis: map[string]int64{}}
	running, err := r.alreadyRunning(ctx)
	if err != nil {
		return res, err
	}
	if !running {
		return res, nil
	}
	if r.apiKey == "" {
		// Status runs in a fresh process, so Up's ephemeral key is gone. Ask
		// the running stack for its own key rather than minting a second one:
		// an unauthenticated probe gets 401 and reports a healthy stack as not
		// ready, which is indistinguishable from a real failure.
		if key, keyErr := r.recoverKey(ctx); keyErr == nil {
			r.apiKey = key
		}
	}
	indexStatus, err := r.probe(ctx, r.apiBase, r.apiKey)
	if err != nil {
		// Up but unreachable is a real state worth reporting rather than an
		// error: the operator asked whether it is ready, and it is not.
		return res, nil
	}
	res.Ready = indexStatus.Complete()
	return res, nil
}

// configFilesNameTheOverlay reports whether a config_files label lists the
// demo overlay as one of its entries.
//
// The label is a comma-separated list of absolute paths, so the comparison is
// per-entry and on the basename. Substring matching over the whole line would
// accept a path that merely embeds the name -- not-docker-compose.demo.yaml.bak
// alongside it -- and this guard exists to refuse tearing down a stack the demo
// did not create, so it has to err toward refusing.
func configFilesNameTheOverlay(label string) bool {
	for _, entry := range strings.Split(label, ",") {
		if filepath.Base(strings.TrimSpace(entry)) == ComposeFileName {
			return true
		}
	}
	return false
}

// serviceName is the Compose service that carries the demo stack's own
// credential. It must match the service key in
// docker-compose.demo.runtime.yaml, which docker-compose.demo.yaml includes;
// TestDemoServiceNameMatchesComposeOverlay fails if the two drift apart.
const serviceName = "eshu"

// recoverKey reads the API key back out of the running demo stack.
//
// The key belongs to the stack, not to the process that started it, so this
// keeps one credential per stack instead of inventing a second one that the
// services would reject.
func (r *Runtime) recoverKey(ctx context.Context) (string, error) {
	out, err := r.exec(ctx, nil, "docker", r.composeArgs("exec", "-T", serviceName, "printenv", "ESHU_API_KEY")...)
	if err != nil {
		return "", fmt.Errorf("recover demo key from project %q: %w", r.project, err)
	}
	key := strings.TrimSpace(string(out))
	if key == "" {
		return "", fmt.Errorf("project %q returned no ESHU_API_KEY", r.project)
	}
	return key, nil
}

// sortStrings sorts in place. A tiny helper so the truth-label renderer does
// not pull sort into render.go's import set for one call.
func sortStrings(s []string) { sort.Strings(s) }
