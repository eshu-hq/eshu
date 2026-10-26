// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"
)

// requiredChecks are the two merge-rule contexts as completed check runs.
func requiredChecks() []ck {
	return []ck{
		{run: 9001, name: "go-core-complete", status: "COMPLETED", result: "SUCCESS"},
		{run: 9002, name: "go-race-complete", status: "COMPLETED", result: "SUCCESS"},
	}
}

// preWave is the set a fresh PR shows for minutes before the Actions wave
// registers: only the always-on external checks, all green.
func preWave() fx {
	f := openPR(1, "aaaa1111")
	f.rollup, f.total = "SUCCESS", 5
	for i, n := range []string{"cloudflare", "codeql", "license/cla", "dco", "labeler"} {
		f.checks = append(f.checks, ck{run: int64(100 + i), name: n, status: "COMPLETED", result: "SUCCESS"})
	}
	return f
}

func TestPreWaveCheckSetIsNeverGreen(t *testing.T) {
	w := newWorld()
	waveArrives := preWave()
	waveArrives.total = 7
	waveArrives.checks = append(waveArrives.checks, requiredChecks()...)
	w.seed(1, preWave(), preWave(), preWave(), preWave(), waveArrives, waveArrives)
	h := newHarness(t, []int{1}, w)

	wantKinds(t, h.poll(), "BASELINE")
	for i := 0; i < 3; i++ {
		if got := h.poll(); got != nil {
			t.Fatalf("stable pre-wave read %d printed %v; five green external checks are not a finished CI", i+2, got)
		}
	}
	if got := h.poll(); got != nil {
		t.Fatalf("first read of the full set is not yet stable, got %v", got)
	}
	got := h.poll()
	wantKinds(t, got, "GREEN")
	if g := parseEvent(t, got[0]); g.kv["checks"] != "7" || g.kv["required"] != "go-core-complete,go-race-complete" {
		t.Fatalf("green must name the required contexts it saw: %s", got[0])
	}
}

func TestGreenNeedsEveryRequiredContextPresentAndSuccessful(t *testing.T) {
	for name, mutate := range map[string]func(*fx){
		"one required context absent": func(f *fx) { f.checks = f.checks[:len(f.checks)-1]; f.total-- },
		"one required context skipped": func(f *fx) {
			f.checks[len(f.checks)-1].result = "SKIPPED"
		},
		"one required context neutral": func(f *fx) {
			f.checks[len(f.checks)-1].result = "NEUTRAL"
		},
	} {
		w := newWorld()
		f := preWave()
		f.total = 7
		f.checks = append(f.checks, requiredChecks()...)
		mutate(&f)
		w.seed(1, f, f, f, f)
		h := newHarness(t, []int{1}, w)
		wantKinds(t, h.poll(), "BASELINE")
		for i := 0; i < 3; i++ {
			if got := h.poll(); got != nil {
				t.Fatalf("%s: poll %d printed %v", name, i+2, got)
			}
		}
	}
}

func TestGreenNeedsTheWholeCheckSetRead(t *testing.T) {
	w := newWorld()
	f := preWave()
	f.total = 9 // the rollup counts 9 checks but only 7 nodes came back
	f.checks = append(f.checks, requiredChecks()...)
	w.seed(1, f, f, f)
	h := newHarness(t, []int{1}, w)
	wantKinds(t, h.poll(), "BASELINE")
	h.wantSilent(2, "a partial read of the check set must never be green")
}

func TestRollupWithNoChecksIsNeverGreen(t *testing.T) {
	w := newWorld()
	f := openPR(1, "aaaa1111")
	f.rollup, f.total = "SUCCESS", 0
	w.seed(1, f, f, f)
	h := newHarness(t, []int{1}, w)
	wantKinds(t, h.poll(), "BASELINE")
	h.wantSilent(2, "an empty check set must never be green")
}

func TestRunRefusesToWatchWithoutRequiredContexts(t *testing.T) {
	w := newWorld()
	w.seed(1, openPR(1, "aaaa1111"))
	h := newHarness(t, []int{1}, w)
	h.wt.cfg.Required = nil

	err := h.wt.Run(context.Background())
	if err == nil || !strings.Contains(h.out.String(), "WATCH state=exit") {
		t.Fatalf("Run must fail loudly with a WATCH exit line, got err=%v out=%q", err, h.out.String())
	}
	if len(w.calls) != 0 {
		t.Fatalf("no GitHub call may happen before the required set is known")
	}
}

func TestResolveRequiredReadsTheRegistryOnce(t *testing.T) {
	w := newWorld()
	var got Command
	exec := func(ctx context.Context, c Command) ([]byte, error) {
		got = c
		return []byte("go-core-complete\ngo-race-complete\n"), nil
	}
	env := []string{"A=1", "GOROOT=/leaky", "GOTOOLCHAIN=go1.26.10"}
	list, err := resolveRequired(context.Background(), exec, "go", "/repo", env, "")
	if err != nil || strings.Join(list, ",") != "go-core-complete,go-race-complete" {
		t.Fatalf("list = %v, err = %v", list, err)
	}
	want := "-C go run ./cmd/ci-gates contexts --registry ../specs/ci-gates.v1.yaml --ruleset-only"
	if strings.Join(got.Args, " ") != want || got.Dir != "/repo" {
		t.Fatalf("command = %v in %q, want args %q", got.Args, got.Dir, want)
	}
	if strings.Contains(strings.Join(got.Env, " "), "GOROOT") || !strings.Contains(strings.Join(got.Env, " "), "GOTOOLCHAIN") {
		t.Fatalf("go child must not inherit GOROOT but keep the toolchain pin: %v", got.Env)
	}
	if override, err := resolveRequired(context.Background(), nil, "go", "/repo", nil, "a, b"); err != nil || strings.Join(override, ",") != "a,b" {
		t.Fatalf("the flag override must win without running anything, got %v, %v", override, err)
	}
	_ = w
}

func TestResolveRequiredFailsLoudly(t *testing.T) {
	fail := func(ctx context.Context, c Command) ([]byte, error) { return nil, &ExitError{Code: 1, Stderr: "boom"} }
	if _, err := resolveRequired(context.Background(), fail, "go", "/repo", nil, ""); err == nil {
		t.Fatalf("an unreadable registry must be an error")
	}
	empty := func(ctx context.Context, c Command) ([]byte, error) { return []byte("\n"), nil }
	if _, err := resolveRequired(context.Background(), empty, "go", "/repo", nil, ""); err == nil {
		t.Fatalf("an empty required set must be an error: a green with nothing required is a false green")
	}
}
