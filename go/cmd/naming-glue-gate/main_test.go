// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// fakeClassifier returns a canned Report or error, so run() is testable
// without a real DeepSeek call.
type fakeClassifier struct {
	report Report
	err    error
	called bool
}

func (f *fakeClassifier) Classify(_ context.Context, _ []Candidate) (Report, error) {
	f.called = true
	return f.report, f.err
}

func fakeEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func baseArgs() []string {
	return []string{"-base-ref", "main", "-head-ref", "HEAD", "-dirs", "go/internal"}
}

func TestRunNoCandidatesSkipsClassifierAndPasses(t *testing.T) {
	runner := fakeGitRunner{byRefDir: map[string][]string{
		"main:go/internal": {"go/internal/query"},
		"HEAD:go/internal": {"go/internal/query"},
	}}
	classifier := &fakeClassifier{}
	var stdout, stderr bytes.Buffer

	code := run(baseArgs(), &stdout, &stderr, classifier, runner, fakeEnv(map[string]string{"DEEPSEEK_API_KEY": "k"}))

	if code != 0 {
		t.Fatalf("run() = %d, want 0; stderr: %s", code, stderr.String())
	}
	if classifier.called {
		t.Error("run() called the classifier with zero candidates, want it to short-circuit")
	}
}

func TestRunMissingAPIKeySkipsAdvisoryPass(t *testing.T) {
	runner := fakeGitRunner{byRefDir: map[string][]string{
		"main:go/internal": {"go/internal/query"},
		"HEAD:go/internal": {"go/internal/query", "go/internal/query/taghistory"},
	}}
	classifier := &fakeClassifier{}
	var stdout, stderr bytes.Buffer

	code := run(baseArgs(), &stdout, &stderr, classifier, runner, fakeEnv(nil))

	if code != 0 {
		t.Fatalf("run() = %d, want 0 (missing key fails open); stderr: %s", code, stderr.String())
	}
	if classifier.called {
		t.Error("run() called the classifier despite a missing API key")
	}
	if !strings.Contains(stderr.String(), "DEEPSEEK_API_KEY") {
		t.Errorf("stderr should explain the missing key, got: %s", stderr.String())
	}
}

func TestRunClassifierErrorFailsOpen(t *testing.T) {
	runner := fakeGitRunner{byRefDir: map[string][]string{
		"main:go/internal": {"go/internal/query"},
		"HEAD:go/internal": {"go/internal/query", "go/internal/query/taghistory"},
	}}
	classifier := &fakeClassifier{err: errBoom}
	var stdout, stderr bytes.Buffer

	code := run(baseArgs(), &stdout, &stderr, classifier, runner, fakeEnv(map[string]string{"DEEPSEEK_API_KEY": "k"}))

	if code != 0 {
		t.Fatalf("run() = %d, want 0 (a transient API failure must not block on an infra problem); stderr: %s", code, stderr.String())
	}
}

func TestRunAllAcceptablePasses(t *testing.T) {
	runner := fakeGitRunner{byRefDir: map[string][]string{
		"main:go/internal": {"go/internal/query"},
		"HEAD:go/internal": {"go/internal/query", "go/internal/query/taghistory"},
	}}
	classifier := &fakeClassifier{report: Report{Findings: []Finding{
		{Path: "go/internal/query/taghistory", Verdict: VerdictAcceptable},
	}}}
	var stdout, stderr bytes.Buffer

	code := run(baseArgs(), &stdout, &stderr, classifier, runner, fakeEnv(map[string]string{"DEEPSEEK_API_KEY": "k"}))
	if code != 0 {
		t.Fatalf("run() = %d, want 0; stderr: %s", code, stderr.String())
	}
}

func TestRunViolationNonBlockingPassesButReports(t *testing.T) {
	runner := fakeGitRunner{byRefDir: map[string][]string{
		"main:go/internal": {"go/internal/query"},
		"HEAD:go/internal": {"go/internal/query", "go/internal/reducer/workloadinstance"},
	}}
	classifier := &fakeClassifier{report: Report{Findings: []Finding{
		{Path: "go/internal/reducer/workloadinstance", Verdict: VerdictGluedCompound, Evidence: "workload+instance", Confidence: "high"},
	}}}
	var stdout, stderr bytes.Buffer

	args := append(baseArgs(), "-blocking=false")
	code := run(args, &stdout, &stderr, classifier, runner, fakeEnv(map[string]string{"DEEPSEEK_API_KEY": "k"}))

	if code != 0 {
		t.Fatalf("run() = %d, want 0 (non-blocking mode never fails the process); stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "workloadinstance") {
		t.Errorf("stderr should still report the violation, got: %s", stderr.String())
	}
}

func TestRunViolationBlockingFails(t *testing.T) {
	runner := fakeGitRunner{byRefDir: map[string][]string{
		"main:go/internal": {"go/internal/query"},
		"HEAD:go/internal": {"go/internal/query", "go/internal/reducer/workloadinstance"},
	}}
	classifier := &fakeClassifier{report: Report{Findings: []Finding{
		{Path: "go/internal/reducer/workloadinstance", Verdict: VerdictGluedCompound, Evidence: "workload+instance", Confidence: "high"},
	}}}
	var stdout, stderr bytes.Buffer

	args := append(baseArgs(), "-blocking=true")
	code := run(args, &stdout, &stderr, classifier, runner, fakeEnv(map[string]string{"DEEPSEEK_API_KEY": "k"}))

	if code != 1 {
		t.Fatalf("run() = %d, want 1 (blocking mode fails on a real finding); stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "workloadinstance") {
		t.Errorf("stderr should report the violation, got: %s", stderr.String())
	}
}

func TestRunGitErrorExitsTwo(t *testing.T) {
	runner := fakeGitRunner{err: errBoom}
	classifier := &fakeClassifier{}
	var stdout, stderr bytes.Buffer

	code := run(baseArgs(), &stdout, &stderr, classifier, runner, fakeEnv(map[string]string{"DEEPSEEK_API_KEY": "k"}))

	if code != 2 {
		t.Fatalf("run() = %d, want 2 (unresolvable git state fails closed); stderr: %s", code, stderr.String())
	}
}

var errBoom = &staticError{"boom"}

type staticError struct{ msg string }

func (e *staticError) Error() string { return e.msg }
