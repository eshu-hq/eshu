// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// flakyResult is one scripted jobs/runs endpoint answer: either a body or a
// transient transport failure such as the GitHub 502 seen on #9130.
type flakyResult struct {
	body string
	err  error
}

// flakyEndpointRunner mirrors the endpointRunner fake (substring route match,
// recorded calls) but serves a scripted answer sequence per route so a test
// can fail the first call and succeed on retry. The last answer repeats, so
// re-confirming reads observe steady state unless told otherwise.
type flakyEndpointRunner struct {
	scripts map[string][]flakyResult
	calls   []string
}

func (r *flakyEndpointRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	r.calls = append(r.calls, joined)
	keys := make([]string, 0, len(r.scripts))
	for key := range r.scripts {
		keys = append(keys, key)
	}
	// Longest key first so a specific route wins over a prefix of it.
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		if strings.Contains(joined, key) {
			seq := r.scripts[key]
			res := seq[0]
			if len(seq) > 1 {
				r.scripts[key] = seq[1:]
			}
			if res.err != nil {
				return nil, res.err
			}
			return []byte(res.body), nil
		}
	}
	return nil, fmt.Errorf("unexpected gh invocation: %s", joined)
}

func (r *flakyEndpointRunner) countCalls(substr string) int {
	n := 0
	for _, call := range r.calls {
		if strings.Contains(call, substr) {
			n++
		}
	}
	return n
}

func calledRunsFixture(id int64, attempt int) flakyResult {
	return flakyResult{body: `[{"workflow_runs":[{"id":` + fmt.Sprint(id) +
		`,"name":"Build Test","path":".github/workflows/test.yml",` +
		`"pull_requests":[{"number":42}],"event":"pull_request","head_sha":"` + headSHAFixture +
		`","run_attempt":` + fmt.Sprint(attempt) + `}]}]`}
}

func calledJobsFixture(conclusion string, attempt int) flakyResult {
	return flakyResult{body: `[{"jobs":[{"name":"call-frontend / console",` +
		`"status":"completed","conclusion":"` + conclusion + `","run_attempt":` + fmt.Sprint(attempt) + `}]}]`}
}

var errTransient502 = errors.New("exit status 1")

// stubCalledJobsSleep replaces the retry backoff with a no-op so retry tests
// run in milliseconds. Callers stay sequential (no t.Parallel) while stubbed.
func stubCalledJobsSleep(t *testing.T) {
	t.Helper()
	old := calledJobsSleep
	calledJobsSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { calledJobsSleep = old })
}

func TestReadCurrentCalledChecksRetriesTransientJobsFailure(t *testing.T) {
	stubCalledJobsSleep(t)
	runner := &flakyEndpointRunner{scripts: map[string][]flakyResult{
		"actions/runs?": {calledRunsFixture(20, 3)},
		"/actions/runs/20/attempts/3/jobs?per_page=100": {{"", errTransient502}, calledJobsFixture("success", 3)},
	}}
	checks, err := readCurrentCalledChecks(context.Background(), runner, "eshu-hq/eshu", 42, headSHAFixture)
	if err != nil || len(checks) != 1 || checks[0].Bucket != "pass" {
		t.Fatalf("transient jobs failure must heal on retry: checks=%#v, err=%v", checks, err)
	}
	if got := runner.countCalls("/attempts/3/jobs"); got != 2 {
		t.Fatalf("full-page jobs reads = %d, want 2 (one failure, one retry)", got)
	}
	for _, call := range runner.calls {
		// Suffix match: the full page ("?per_page=100") shares a prefix
		// with the fallback ("?per_page=1"), so Contains cannot tell them
		// apart.
		if strings.HasSuffix(call, "jobs?per_page=1") {
			t.Fatalf("small-page fallback ran on a healed read: %s", call)
		}
	}
}

func TestReadCurrentCalledChecksPersistentJobsFailureStillErrors(t *testing.T) {
	stubCalledJobsSleep(t)
	runner := &flakyEndpointRunner{scripts: map[string][]flakyResult{
		"actions/runs?": {calledRunsFixture(20, 3)},
		"/actions/runs/20/attempts/3/jobs?per_page=100": {{"", errTransient502}, {"", errTransient502}, {"", errTransient502}},
		"/actions/runs/20/attempts/3/jobs?per_page=1":   {{"", errTransient502}},
	}}
	checks, err := readCurrentCalledChecks(context.Background(), runner, "eshu-hq/eshu", 42, headSHAFixture)
	if err == nil {
		t.Fatal("persistent jobs failure must stay an error, never a false green")
	}
	if len(checks) != 0 {
		t.Fatalf("persistent jobs failure must return no checks: %#v", checks)
	}
	if !strings.Contains(err.Error(), "attempt 3") {
		t.Fatalf("error must name the evaluated attempt: %v", err)
	}
	if got := runner.countCalls("/attempts/3/jobs?per_page=100"); got != 3 {
		t.Fatalf("full-page jobs reads = %d, want 3 before fallback", got)
	}
	fellBack := false
	for _, call := range runner.calls {
		if strings.HasSuffix(call, "jobs?per_page=1") {
			fellBack = true
		}
	}
	if !fellBack {
		t.Fatal("repeated full-page failure must attempt the small-page fallback")
	}
}

func TestReadCurrentCalledChecksAttemptBumpRereads(t *testing.T) {
	runner := &flakyEndpointRunner{scripts: map[string][]flakyResult{
		"actions/runs?":                    {calledRunsFixture(20, 3), calledRunsFixture(20, 4)},
		"/actions/runs/20/attempts/3/jobs": {calledJobsFixture("failure", 3)},
		"/actions/runs/20/attempts/4/jobs": {calledJobsFixture("success", 4)},
	}}
	checks, err := readCurrentCalledChecks(context.Background(), runner, "eshu-hq/eshu", 42, headSHAFixture)
	if err != nil || len(checks) != 1 || checks[0].Bucket != "pass" {
		t.Fatalf("attempt bump must re-evaluate the new attempt: checks=%#v, err=%v", checks, err)
	}
	if got := runner.countCalls("/attempts/4/jobs"); got != 1 {
		t.Fatalf("new-attempt jobs reads = %d, want 1", got)
	}
}

func TestReadCurrentCalledChecksMixedAttemptStillErrors(t *testing.T) {
	runner := &flakyEndpointRunner{scripts: map[string][]flakyResult{
		"actions/runs?":                    {calledRunsFixture(20, 3)},
		"/actions/runs/20/attempts/3/jobs": {calledJobsFixture("success", 2)},
	}}
	checks, err := readCurrentCalledChecks(context.Background(), runner, "eshu-hq/eshu", 42, headSHAFixture)
	if err == nil {
		t.Fatal("genuinely mixed attempt evidence must stay an error")
	}
	if len(checks) != 0 {
		t.Fatalf("mixed attempt evidence must return no checks: %#v", checks)
	}
}

func TestReadCurrentCalledChecksRunDisappearsDuringJobsRead(t *testing.T) {
	runner := &flakyEndpointRunner{scripts: map[string][]flakyResult{
		"actions/runs?": {
			calledRunsFixture(20, 3),
			{body: `[{"workflow_runs":[]}]`},
		},
		"/actions/runs/20/attempts/3/jobs": {calledJobsFixture("success", 3)},
	}}
	checks, err := readCurrentCalledChecks(context.Background(), runner, "eshu-hq/eshu", 42, headSHAFixture)
	if err == nil {
		t.Fatal("disappearing run must stay an error, never a classification")
	}
	if len(checks) != 0 {
		t.Fatalf("disappearing run must return no checks: %#v", checks)
	}
	if !strings.Contains(err.Error(), "disappeared during jobs read") {
		t.Fatalf("error must report the disappearance: %v", err)
	}
}

func TestReadCurrentCalledChecksAttemptKeepsMovingFailsClosed(t *testing.T) {
	runner := &flakyEndpointRunner{scripts: map[string][]flakyResult{
		"actions/runs?": {
			calledRunsFixture(20, 3),
			calledRunsFixture(20, 4),
			calledRunsFixture(20, 5),
			calledRunsFixture(20, 6),
		},
		"/actions/runs/20/attempts/3/jobs": {calledJobsFixture("success", 3)},
		"/actions/runs/20/attempts/4/jobs": {calledJobsFixture("success", 4)},
		"/actions/runs/20/attempts/5/jobs": {calledJobsFixture("success", 5)},
	}}
	checks, err := readCurrentCalledChecks(context.Background(), runner, "eshu-hq/eshu", 42, headSHAFixture)
	if err == nil {
		t.Fatal("a constantly moving attempt must stay an error, never a classification")
	}
	if len(checks) != 0 {
		t.Fatalf("a constantly moving attempt must return no checks: %#v", checks)
	}
	if !strings.Contains(err.Error(), "kept moving during jobs read") {
		t.Fatalf("error must report pin exhaustion: %v", err)
	}
	for _, attempt := range []string{"/attempts/3/jobs", "/attempts/4/jobs", "/attempts/5/jobs"} {
		if got := runner.countCalls(attempt); got != 1 {
			t.Fatalf("attempt jobs reads for %s = %d, want 1 (chase every pin, classify none)", attempt, got)
		}
	}
}
