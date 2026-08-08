// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// down removes the demo stack along with its volumes and networks.
//
// `-v` and `--remove-orphans` are not optional niceties: the acceptance
// criteria for the demo are zero leaked containers, volumes, or networks, and
// a plain `compose down` leaves named volumes behind. Every invocation is
// scoped to the demo project, so a stack the demo did not create is out of
// reach here by construction rather than by care.
func (r *demoRuntime) down(ctx context.Context) error {
	// up refuses to adopt a project it did not start; down holds the same
	// line, or --project pointed at an operator's stack removes it along with
	// its volumes.
	owned, err := r.ownsProject(ctx)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf(
			"compose project %q was not created from %s; refusing to remove a stack eshu demo did not start",
			r.project, demoComposeFileName)
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
func (r *demoRuntime) ownsProject(ctx context.Context) (bool, error) {
	out, err := r.exec(ctx, nil, "docker", "ps", "--all",
		"--filter", "label=com.docker.compose.project="+r.project,
		"--format", "{{.Label \"com.docker.compose.project.config_files\"}}")
	if err != nil {
		return false, fmt.Errorf("check ownership of project %q: %w", r.project, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	sawAny := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sawAny = true
		if !strings.Contains(line, demoComposeFileName) {
			return false, nil
		}
	}
	_ = sawAny
	return true, nil
}

// status reports whether the demo project is running and finished indexing.
//
// Running and ready are deliberately separate: a stack that is up but still
// indexing answers the demo questions incompletely, so reporting "running" as
// "ready" would be the same health-vs-completeness mistake first-run refuses
// to make.
func (r *demoRuntime) status(ctx context.Context) (demoResult, error) {
	res := demoResult{Project: r.project, PhaseMillis: map[string]int64{}}
	running, err := r.alreadyRunning(ctx)
	if err != nil {
		return res, err
	}
	if !running {
		return res, nil
	}
	if r.apiKey == "" {
		// status runs in a fresh process, so up's ephemeral key is gone. Ask
		// the running stack for its own key rather than minting a second one:
		// an unauthenticated probe gets 401 and reports a healthy stack as not
		// ready, which is indistinguishable from a real failure.
		if key, keyErr := r.recoverDemoKey(ctx); keyErr == nil {
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

// recoverDemoKey reads the API key back out of the running demo stack.
//
// The key belongs to the stack, not to the process that started it, so this
// keeps one credential per stack instead of inventing a second one that the
// services would reject.
func (r *demoRuntime) recoverDemoKey(ctx context.Context) (string, error) {
	out, err := r.exec(ctx, nil, "docker", r.composeArgs("exec", "-T", "eshu", "printenv", "ESHU_API_KEY")...)
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
// not pull sort into demo.go's import set for one call.
func sortStrings(s []string) { sort.Strings(s) }
