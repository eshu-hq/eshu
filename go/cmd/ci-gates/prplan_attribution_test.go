// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestCalledAndDirectGateIdentitiesBothMustPass(t *testing.T) {
	t.Parallel()
	gate := resolvedRequiredGate{WorkflowFile: "frontend.yml", WorkflowName: "Frontend", Job: "console", GateIDs: []string{"console"}}
	checks := []checkRollup{
		{Name: "console", Workflow: "Frontend", Event: eventPullRequest, Bucket: "pass", State: "SUCCESS"},
		{Name: "call-frontend / console", Workflow: "Build Test", Event: eventPullRequest, Bucket: "fail", State: "FAILURE"},
	}
	got := evaluateRequiredChecks([]resolvedRequiredGate{gate}, checks, nil)
	if len(got.Failed) != 1 {
		t.Fatalf("direct green cannot hide called failure: %#v", got)
	}
	checks[1].Bucket, checks[1].State = "pass", "SUCCESS"
	got = evaluateRequiredChecks([]resolvedRequiredGate{gate}, checks, nil)
	if len(got.Failed)+len(got.Pending)+len(got.Cancelled) != 0 {
		t.Fatalf("both green should pass: %#v", got)
	}
}

func TestReadCurrentCalledChecksUsesExactHeadAndAttempt(t *testing.T) {
	t.Parallel()
	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?":                    `[{"workflow_runs":[{"id":20,"name":"Build Test","event":"pull_request","head_sha":"` + headSHAFixture + `","run_attempt":2},{"id":19,"name":"Build Test","event":"pull_request","head_sha":"` + headSHAFixture + `","run_attempt":1}]}]`,
		"/actions/runs/20/attempts/2/jobs": `[{"jobs":[{"name":"call-frontend / console","status":"completed","conclusion":"failure","run_attempt":2},{"name":"queue-selection / select","status":"completed","conclusion":"success","run_attempt":2}]}]`,
	}}
	got, err := readCurrentCalledChecks(context.Background(), runner, "eshu-hq/eshu", headSHAFixture)
	if err != nil || len(got) != 1 || got[0].Name != "call-frontend / console" || got[0].RunID != 20 || got[0].Bucket != "fail" {
		t.Fatalf("called checks = %#v, err=%v", got, err)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "/jobs") && !strings.Contains(call, "/attempts/2/jobs") {
			t.Fatalf("read an obsolete attempt: %s", call)
		}
	}
}

func TestReadCurrentCalledChecksRejectsWrongHead(t *testing.T) {
	t.Parallel()
	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?": `[{"workflow_runs":[{"id":20,"name":"Build Test","event":"pull_request","head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","run_attempt":1}]}]`,
	}}
	got, err := readCurrentCalledChecks(context.Background(), runner, "eshu-hq/eshu", headSHAFixture)
	if err != nil || len(got) != 0 {
		t.Fatalf("wrong head produced checks: %#v, err=%v", got, err)
	}
}

func TestReadPRChecksWithCalledRejectsStaleCallerRollup(t *testing.T) {
	t.Parallel()
	runner := &endpointRunner{routes: map[string]string{
		"pr checks":                        `[{"name":"console","state":"SUCCESS","bucket":"pass","workflow":"Frontend","event":"pull_request"},{"name":"call-frontend / console","state":"SUCCESS","bucket":"pass","workflow":"Build Test","event":"pull_request"}]`,
		"actions/runs?":                    `[{"workflow_runs":[{"id":20,"name":"Build Test","event":"pull_request","head_sha":"` + headSHAFixture + `","run_attempt":2}]}]`,
		"/actions/runs/20/attempts/2/jobs": `[{"jobs":[]}]`,
	}}
	required := []resolvedRequiredGate{{WorkflowFile: "frontend.yml", WorkflowName: "Frontend", Job: "console"}}
	checks, err := readPRChecksWithCalled(context.Background(), runner, "eshu-hq/eshu", 42, headSHAFixture, required)
	if err != nil {
		t.Fatal(err)
	}
	got := evaluateRequiredChecks(required, checks, nil)
	if len(got.Pending) != 1 || len(got.Failed) != 0 {
		t.Fatalf("stale caller success must wait on current attempt: checks=%#v, evaluation=%#v", checks, got)
	}
}

func TestCalledAliasRejectsUnrelatedWorkflowAndJob(t *testing.T) {
	t.Parallel()
	gate := resolvedRequiredGate{WorkflowFile: "frontend.yml", WorkflowName: "Frontend", Job: "console"}
	for _, check := range []checkRollup{
		{Name: "call-other / console", Workflow: "Build Test", Event: eventPullRequest, Bucket: "pass"},
		{Name: "call-frontend / console", Workflow: "Other", Event: eventPullRequest, Bucket: "pass"},
		{Name: "call-frontend / other", Workflow: "Build Test", Event: eventPullRequest, Bucket: "pass"},
	} {
		got := evaluateRequiredChecks([]resolvedRequiredGate{gate}, []checkRollup{check}, nil)
		if len(got.Pending) != 1 {
			t.Fatalf("unowned alias %#v passed gate: %#v", check, got)
		}
	}
}

func TestCalledFailureCannotBeClearedByDirectSuccess(t *testing.T) {
	t.Parallel()
	gate := resolvedRequiredGate{WorkflowFile: "frontend.yml", WorkflowName: "Frontend", Job: "console", GateIDs: []string{"console"}}
	failedCheck := checkRollup{Name: "call-frontend / console", Workflow: "Build Test", Event: eventPullRequest, Bucket: "fail", State: "FAILURE", RunID: 20, HeadSHA: headSHAFixture}
	evaluation := evaluateRequiredChecks([]resolvedRequiredGate{gate}, []checkRollup{failedCheck}, nil)
	if len(evaluation.Failed) != 1 {
		t.Fatalf("setup failed: %#v", evaluation)
	}
	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?": `[{"workflow_runs":[{"id":19,"name":"Frontend","event":"pull_request","check_suite_id":119}]}]`,
		"/check-runs":   `[{"check_runs":[{"name":"console","status":"completed","conclusion":"success","check_suite":{"id":119}}]}]`,
	}}
	got := attributeFailedGatesToNewestRun(context.Background(), runner, "eshu-hq/eshu", headSHAFixture, eventPullRequest, evaluation.Failed, nil, io.Discard)
	if len(got.Confirmed) != 1 {
		t.Fatalf("direct green cleared current called failure: %#v", got)
	}
}

func TestCalledCheckCancelledAndAmbiguousFailClosed(t *testing.T) {
	t.Parallel()
	gate := resolvedRequiredGate{WorkflowFile: "frontend.yml", WorkflowName: "Frontend", Job: "console"}
	cancelled := []checkRollup{{Name: "call-frontend / console", Workflow: "Build Test", Event: eventPullRequest, State: "CANCELLED", Bucket: "cancel"}}
	got := evaluateRequiredChecks([]resolvedRequiredGate{gate}, cancelled, nil)
	if len(got.Cancelled) != 1 {
		t.Fatalf("cancelled caller passed: %#v", got)
	}
	ambiguous := []checkRollup{{Name: "call-frontend / console", Workflow: "Build Test", Event: eventPullRequest, State: "AMBIGUOUS", Bucket: "fail"}}
	got = evaluateRequiredChecks([]resolvedRequiredGate{gate}, ambiguous, nil)
	if len(got.Failed) != 1 {
		t.Fatalf("ambiguous caller passed: %#v", got)
	}
}
