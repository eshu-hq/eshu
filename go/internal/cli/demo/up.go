// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Answer is the first correlated answer the demo proves, with the truth
// labels that make it evidence rather than prose.
type Answer struct {
	// Question is the manifest question asked (specs/demo-first-answers.v1.yaml).
	Question string `json:"question"`
	// Answer is the rendered answer text.
	Answer string `json:"answer"`
	// Truth carries freshness/completeness/backend provenance for the answer.
	Truth map[string]any `json:"truth"`
}

// Result is the machine-readable outcome of `eshu demo up`.
type Result struct {
	// Project is the Compose project the demo owns for this run.
	Project string `json:"project"`
	// Ready reports whether indexing completeness was reached.
	Ready bool `json:"ready"`
	// FirstAnswer is the guided first question and its answer.
	FirstAnswer Answer `json:"first_answer"`
	// PhaseMillis records per-phase wall time. This is the raw input the TTFA
	// measurement lane (#4744) consumes; it is emitted here so the timings come
	// from the command that actually owns the lifecycle.
	PhaseMillis map[string]int64 `json:"phase_millis"`
	// TotalMillis is wall time from preflight to first answer.
	TotalMillis int64 `json:"total_millis"`
}

// Up brings the demo stack to a first correlated answer. Phases are timed
// individually so the TTFA lane has per-phase attribution rather than one
// opaque total.
func (r *Runtime) Up(ctx context.Context) (Result, error) {
	res := Result{Project: r.project, PhaseMillis: map[string]int64{}}
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
	key, err := newEphemeralKey()
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
//
// ctx.Err() is returned bare so a caller can still match context.Canceled and
// context.DeadlineExceeded directly.
//
//nolint:wrapcheck // a bare ctx.Err() keeps errors.Is working for callers
func (r *Runtime) waitReady(ctx context.Context) error {
	deadline := r.now().Add(readyTimeout)
	var last IndexStatus
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
				readyTimeout, last.RepositoryCount, last.Complete(), r.project, r.composeFile)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.effectivePollInterval()):
		}
	}
}

// sinceMillis measures elapsed wall time from t using the injected clock.
func (r *Runtime) sinceMillis(t time.Time) int64 {
	ms := r.now().Sub(t).Milliseconds()
	if ms <= 0 {
		// A monotonic clock can report 0 for a fast phase; the TTFA lane reads
		// these as durations, so never emit a negative or absent value.
		return 1
	}
	return ms
}
