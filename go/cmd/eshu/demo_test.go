// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeDemoExec records every command the runtime shells out to and replays a
// scripted reply, so the whole lifecycle is exercised without Docker.
type fakeDemoExec struct {
	calls    [][]string
	replies  map[string]demoExecReply
	fallback demoExecReply
}

type demoExecReply struct {
	out []byte
	err error
}

func (f *fakeDemoExec) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	joined := strings.Join(call, " ")
	for pattern, reply := range f.replies {
		if strings.Contains(joined, pattern) {
			return reply.out, reply.err
		}
	}
	return f.fallback.out, f.fallback.err
}

func (f *fakeDemoExec) sawContaining(fragment string) bool {
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), fragment) {
			return true
		}
	}
	return false
}

// steadyClock advances a fixed step per call so phase timings are deterministic.
func steadyClock(step time.Duration) func() time.Time {
	base := time.Unix(1_760_000_000, 0).UTC()
	n := 0
	return func() time.Time {
		t := base.Add(time.Duration(n) * step)
		n++
		return t
	}
}

func newTestDemoRuntime(exec *fakeDemoExec) *demoRuntime {
	return &demoRuntime{
		exec:    exec.run,
		now:     steadyClock(time.Second),
		project: defaultDemoProject,
		// Keep the multi-poll readiness path off the wall clock.
		pollInterval: time.Millisecond,
		// A ready index and a correct first answer unless a test overrides.
		probe: func(context.Context, string) (demoIndexStatus, error) {
			return demoIndexStatus{Complete: true, Repositories: 20}, nil
		},
		ask: func(context.Context, string, string) (demoAnswer, error) {
			return demoAnswer{Question: "q", Answer: "a", Truth: map[string]any{"backend": "nornicdb"}}, nil
		},
	}
}

func TestDemoUp_UsesUniqueProjectNameAndDemoOverlay(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{replies: map[string]demoExecReply{
		// No pre-existing containers for this project.
		"compose -p eshu-demo -f docker-compose.demo.yaml ps": {out: []byte("")},
	}}
	rt := newTestDemoRuntime(exec)

	if _, err := rt.up(context.Background()); err != nil {
		t.Fatalf("up: %v", err)
	}
	if !exec.sawContaining("-p eshu-demo") {
		t.Errorf("compose was not scoped to the demo project name; calls=%v", exec.calls)
	}
	if !exec.sawContaining("docker-compose.demo.yaml") {
		t.Errorf("compose did not use the demo overlay; calls=%v", exec.calls)
	}
	if !exec.sawContaining("up -d") {
		t.Errorf("compose never brought the stack up; calls=%v", exec.calls)
	}
}

func TestDemoUp_RefusesToClobberARunningProjectAndNeverDownsIt(t *testing.T) {
	t.Parallel()
	// `compose ps` reports a running container, so this project is not ours.
	exec := &fakeDemoExec{replies: map[string]demoExecReply{
		"compose -p eshu-demo -f docker-compose.demo.yaml ps": {out: []byte("eshu-demo-postgres-1\n")},
	}}
	rt := newTestDemoRuntime(exec)

	_, err := rt.up(context.Background())
	if err == nil {
		t.Fatal("up() error = nil, want a refuse-to-clobber error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error %q does not explain that the project is already running", err)
	}
	// The invariant that matters: a stack we did not start is never torn down.
	if exec.sawContaining("down") {
		t.Errorf("up() tore down a stack it did not start; calls=%v", exec.calls)
	}
	if exec.sawContaining("up -d") {
		t.Errorf("up() started over a running project; calls=%v", exec.calls)
	}
}

func TestDemoUp_MissingDockerIsADiagnosticsGradeError(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{
		replies: map[string]demoExecReply{
			"docker version": {err: errors.New("exec: \"docker\": executable file not found in $PATH")},
		},
	}
	rt := newTestDemoRuntime(exec)

	_, err := rt.up(context.Background())
	if err == nil {
		t.Fatal("up() error = nil, want a docker preflight error")
	}
	msg := err.Error()
	// Diagnostics-grade means it names what was probed and what to do, not
	// just the raw exec failure.
	for _, want := range []string{"docker", "not"} {
		if !strings.Contains(strings.ToLower(msg), want) {
			t.Errorf("error %q missing %q; a bare exec error is not diagnostics-grade", msg, want)
		}
	}
	if exec.sawContaining("up -d") {
		t.Errorf("up() started compose despite a failed docker preflight; calls=%v", exec.calls)
	}
}

func TestDemoUp_WaitsForIndexCompletenessNotProcessHealth(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{}
	rt := newTestDemoRuntime(exec)
	polls := 0
	rt.probe = func(context.Context, string) (demoIndexStatus, error) {
		polls++
		if polls < 3 {
			return demoIndexStatus{Complete: false, Repositories: polls}, nil
		}
		return demoIndexStatus{Complete: true, Repositories: 20}, nil
	}

	res, err := rt.up(context.Background())
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if polls < 3 {
		t.Errorf("readiness polled %d times; it must wait for completeness, not the first reply", polls)
	}
	if !res.Ready {
		t.Error("result not marked ready after completeness reported true")
	}
}

func TestDemoUp_RecordsPerPhaseTimings(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{}
	rt := newTestDemoRuntime(exec)

	res, err := rt.up(context.Background())
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	// These are the raw input the TTFA issue (#4744) consumes.
	for _, phase := range []string{"preflight", "up", "ready", "first_answer"} {
		if _, ok := res.PhaseMillis[phase]; !ok {
			t.Errorf("phase %q missing from timings %v", phase, res.PhaseMillis)
		}
	}
	if res.TotalMillis <= 0 {
		t.Errorf("TotalMillis = %d, want > 0", res.TotalMillis)
	}
}

func TestDemoUp_CarriesTheFirstAnswerAndItsTruth(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{}
	rt := newTestDemoRuntime(exec)

	res, err := rt.up(context.Background())
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if res.FirstAnswer.Answer == "" {
		t.Error("first answer is empty; demo up must reach an answer, not just a running stack")
	}
	if len(res.FirstAnswer.Truth) == 0 {
		t.Error("first answer carries no truth labels; an unlabelled answer is not evidence")
	}
}

func TestDemoDown_RemovesVolumesAndOrphansForTheDemoProjectOnly(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{}
	rt := newTestDemoRuntime(exec)

	if err := rt.down(context.Background()); err != nil {
		t.Fatalf("down: %v", err)
	}
	if !exec.sawContaining("-p eshu-demo") {
		t.Errorf("down was not scoped to the demo project; calls=%v", exec.calls)
	}
	for _, flag := range []string{"down", "-v", "--remove-orphans"} {
		if !exec.sawContaining(flag) {
			t.Errorf("down missing %q; leaked volumes/networks fail the acceptance criteria; calls=%v", flag, exec.calls)
		}
	}
}

func TestDemoRuntime_ProjectOverrideIsHonoured(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{}
	rt := newTestDemoRuntime(exec)
	rt.project = "eshu-demo-reviewer"

	if _, err := rt.up(context.Background()); err != nil {
		t.Fatalf("up: %v", err)
	}
	if !exec.sawContaining("-p eshu-demo-reviewer") {
		t.Errorf("--project override ignored; calls=%v", exec.calls)
	}
	if exec.sawContaining("-p eshu-demo ") {
		t.Errorf("override still touched the default project; calls=%v", exec.calls)
	}
}

func TestDemoEnvelope_ShapeMatchesTheFirstRunContract(t *testing.T) {
	t.Parallel()
	res := demoResult{
		Ready:       true,
		FirstAnswer: demoAnswer{Question: "q", Answer: "a", Truth: map[string]any{"backend": "nornicdb"}},
		PhaseMillis: map[string]int64{"up": 1},
		TotalMillis: 1,
	}
	env := demoEnvelopeFor(res, nil)
	if env.Error != nil {
		t.Errorf("Error = %v, want nil on success", env.Error)
	}
	if len(env.Truth) == 0 {
		t.Error("envelope Truth is empty; the answer's provenance must survive into --json")
	}

	failed := demoEnvelopeFor(demoResult{}, errors.New("compose up failed"))
	if failed.Error == nil || failed.Error.Message == "" {
		t.Fatal("failed envelope carries no error message")
	}
}
