// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestRunMaterializePlatformPrerequisiteRedactsBackendConnectionTarget(t *testing.T) {
	original := openPlatformPrerequisiteBackend
	openPlatformPrerequisiteBackend = func(context.Context) (platformPrerequisiteBackend, func(), error) {
		return nil, nil, errors.New("dial tcp graph.private.corp.example:7687: connection refused")
	}
	t.Cleanup(func() { openPlatformPrerequisiteBackend = original })

	var stdout, stderr bytes.Buffer
	err := runMaterializePlatformPrerequisiteCommand(
		context.Background(),
		[]string{"-repo-id", "repo:test", "-kind", "kubernetes", "-name", "prod", "-locator", "cluster/prod"},
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("runMaterializePlatformPrerequisiteCommand() error = nil, want backend-open error")
	}
	if strings.Contains(err.Error(), "graph.private.corp.example") || strings.Contains(err.Error(), ":7687") {
		t.Fatalf("error leaks private connection target: %q", err)
	}
	if !strings.Contains(err.Error(), "open graph backend") || !strings.Contains(err.Error(), "configuration and reachability") {
		t.Fatalf("error = %q, want useful redacted backend-open guidance", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunMaterializePlatformPrerequisiteRedactsBackendStageErrors(t *testing.T) {
	tests := []struct {
		name          string
		stage         string
		sensitiveHost string
		sensitivePort string
		configure     func(*recordingPlatformPrerequisiteBackend, error)
	}{
		{
			name:          "repository count",
			stage:         "verify source Repository",
			sensitiveHost: "repository.private.corp.example",
			sensitivePort: "7441",
			configure: func(backend *recordingPlatformPrerequisiteBackend, err error) {
				backend.repositoryErr = err
			},
		},
		{
			name:          "platform materializer",
			stage:         "materialize Platform prerequisite",
			sensitiveHost: "writer.private.corp.example",
			sensitivePort: "7552",
			configure: func(backend *recordingPlatformPrerequisiteBackend, err error) {
				backend.executeErr = err
			},
		},
		{
			name:          "platform count",
			stage:         "verify Platform",
			sensitiveHost: "platform.private.corp.example",
			sensitivePort: "7663",
			configure: func(backend *recordingPlatformPrerequisiteBackend, err error) {
				backend.platformErr = err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &recordingPlatformPrerequisiteBackend{repositoryCount: 1, platformCount: 1}
			backendCause := errors.New("dial tcp " + test.sensitiveHost + ":" + test.sensitivePort + ": connection refused")
			test.configure(backend, backendCause)
			original := openPlatformPrerequisiteBackend
			closed := false
			openPlatformPrerequisiteBackend = func(context.Context) (platformPrerequisiteBackend, func(), error) {
				return backend, func() { closed = true }, nil
			}
			t.Cleanup(func() { openPlatformPrerequisiteBackend = original })

			var stdout, stderr bytes.Buffer
			err := runMaterializePlatformPrerequisiteCommand(
				context.Background(),
				[]string{"-repo-id", "repo:test", "-kind", "kubernetes", "-name", "prod", "-locator", "cluster/prod"},
				&stdout,
				&stderr,
			)
			if err == nil {
				t.Fatal("runMaterializePlatformPrerequisiteCommand() error = nil, want backend-stage error")
			}
			if strings.Contains(err.Error(), test.sensitiveHost) || strings.Contains(err.Error(), test.sensitivePort) {
				t.Fatalf("error leaks private connection target: %q", err)
			}
			if errors.Is(err, backendCause) {
				t.Fatalf("command error preserves sensitive backend cause across the redaction boundary: %q", err)
			}
			if !strings.Contains(err.Error(), test.stage) {
				t.Fatalf("error = %q, want safe stage label %q", err, test.stage)
			}
			if !strings.Contains(err.Error(), "configuration and reachability") {
				t.Fatalf("error = %q, want useful redacted backend guidance", err)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q, want both empty", stdout.String(), stderr.String())
			}
			if !closed {
				t.Fatal("backend close function was not called")
			}
		})
	}
}

func TestPlatformPrerequisiteBuildsExactProductionRow(t *testing.T) {
	materializer := &recordingPlatformPrerequisiteMaterializer{}
	verifier := &stubPlatformPrerequisiteVerifier{repositoryCount: 1, platformCount: 1}
	options := platformPrerequisiteOptions{
		repoID:  "repo-ifa-repo-dependency-source",
		kind:    "kubernetes",
		name:    "prod-cluster",
		locator: "cluster/prod-cluster",
	}

	platformID, count, err := materializePlatformPrerequisite(
		context.Background(),
		options,
		materializer,
		verifier,
	)
	if err != nil {
		t.Fatalf("materializePlatformPrerequisite() error = %v", err)
	}
	const wantPlatformID = "platform:kubernetes:none:cluster/prod-cluster:none:none"
	if platformID != wantPlatformID {
		t.Fatalf("platformID = %q, want %q", platformID, wantPlatformID)
	}
	if count != 1 {
		t.Fatalf("verified count = %d, want 1", count)
	}
	if len(materializer.calls) != 1 {
		t.Fatalf("Materialize calls = %d, want 1", len(materializer.calls))
	}
	if len(materializer.calls[0]) != 1 {
		t.Fatalf("rows passed to Materialize = %d, want exactly 1", len(materializer.calls[0]))
	}
	want := reducer.InfrastructurePlatformRow{
		RepoID:          "repo-ifa-repo-dependency-source",
		PlatformID:      wantPlatformID,
		PlatformName:    "prod-cluster",
		PlatformKind:    "kubernetes",
		PlatformLocator: "cluster/prod-cluster",
	}
	if got := materializer.calls[0][0]; got != want {
		t.Fatalf("row = %#v, want %#v", got, want)
	}
}

func TestPlatformPrerequisiteRejectsInvalidInputBeforeOpeningBackend(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing repo", args: []string{"-kind", "kubernetes", "-name", "prod", "-locator", "cluster/prod"}, want: "-repo-id is required"},
		{name: "blank repo", args: []string{"-repo-id", " ", "-kind", "kubernetes", "-name", "prod", "-locator", "cluster/prod"}, want: "-repo-id is required"},
		{name: "blank kind", args: []string{"-repo-id", "repo:test", "-kind", "  ", "-name", "prod", "-locator", "cluster/prod"}, want: "-kind is required"},
		{name: "blank name", args: []string{"-repo-id", "repo:test", "-kind", "kubernetes", "-name", "\t", "-locator", "cluster/prod"}, want: "-name is required"},
		{name: "blank locator", args: []string{"-repo-id", "repo:test", "-kind", "kubernetes", "-name", "prod", "-locator", " "}, want: "-locator is required"},
		{name: "blank optional provider", args: []string{"-repo-id", "repo:test", "-kind", "kubernetes", "-name", "prod", "-locator", "cluster/prod", "-provider", " "}, want: "-provider must not be blank when set"},
		{name: "positional", args: []string{"-repo-id", "repo:test", "-kind", "kubernetes", "-name", "prod", "-locator", "cluster/prod", "extra"}, want: "positional arguments are not accepted"},
	}

	original := openPlatformPrerequisiteBackend
	opens := 0
	openPlatformPrerequisiteBackend = func(context.Context) (platformPrerequisiteBackend, func(), error) {
		opens++
		return nil, func() {}, nil
	}
	t.Cleanup(func() { openPlatformPrerequisiteBackend = original })

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := runMaterializePlatformPrerequisiteCommand(
				context.Background(),
				test.args,
				&stdout,
				&stderr,
			)
			if err == nil {
				t.Fatal("runMaterializePlatformPrerequisiteCommand() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want substring %q", err, test.want)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
	if opens != 0 {
		t.Fatalf("backend opens = %d, want 0", opens)
	}
}

func TestPlatformPrerequisiteFailsWhenMaterializerDidNotCreatePlatform(t *testing.T) {
	materializer := &recordingPlatformPrerequisiteMaterializer{}
	verifier := &stubPlatformPrerequisiteVerifier{repositoryCount: 1, platformCount: 0}

	_, _, err := materializePlatformPrerequisite(
		context.Background(),
		platformPrerequisiteOptions{
			repoID:  "repo:test",
			kind:    "kubernetes",
			name:    "prod",
			locator: "cluster/prod",
		},
		materializer,
		verifier,
	)
	if err == nil {
		t.Fatal("materializePlatformPrerequisite() error = nil, want failed postcondition")
	}
	if !strings.Contains(err.Error(), "verified 0 Platform nodes") {
		t.Fatalf("error = %q, want exact Platform postcondition", err)
	}
}

func TestPlatformPrerequisiteRequiresSourceRepository(t *testing.T) {
	materializer := &recordingPlatformPrerequisiteMaterializer{}
	verifier := &stubPlatformPrerequisiteVerifier{repositoryCount: 0}

	_, _, err := materializePlatformPrerequisite(
		context.Background(),
		platformPrerequisiteOptions{
			repoID:  "repo:missing",
			kind:    "kubernetes",
			name:    "prod",
			locator: "cluster/prod",
		},
		materializer,
		verifier,
	)
	if err == nil {
		t.Fatal("materializePlatformPrerequisite() error = nil, want missing Repository error")
	}
	if !strings.Contains(err.Error(), `source Repository "repo:missing"`) {
		t.Fatalf("error = %q, want source Repository prerequisite", err)
	}
	if len(materializer.calls) != 0 {
		t.Fatalf("Materialize calls = %d, want 0", len(materializer.calls))
	}
}

func TestRunDispatchesMaterializePlatformPrerequisite(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"materialize-platform-prerequisite", "-bogus-flag"},
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run(materialize-platform-prerequisite) error = nil, want flag error")
	}
	if !strings.Contains(stderr.String(), "ifa materialize-platform-prerequisite") {
		t.Fatalf("stderr = %q, want subcommand flag-set name", stderr.String())
	}
}

func TestRunMaterializePlatformPrerequisiteUsesProductionMaterializer(t *testing.T) {
	backend := &recordingPlatformPrerequisiteBackend{repositoryCount: 1, platformCount: 1}
	original := openPlatformPrerequisiteBackend
	closed := false
	openPlatformPrerequisiteBackend = func(context.Context) (platformPrerequisiteBackend, func(), error) {
		return backend, func() { closed = true }, nil
	}
	t.Cleanup(func() { openPlatformPrerequisiteBackend = original })

	var stdout, stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{
			"materialize-platform-prerequisite",
			"-repo-id", "repo-ifa-repo-dependency-source",
			"-kind", "kubernetes",
			"-name", "prod-cluster",
			"-locator", "cluster/prod-cluster",
		},
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run(materialize-platform-prerequisite) error = %v", err)
	}
	if !closed {
		t.Fatal("backend close function was not called")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	const wantOutput = "platform_id=platform:kubernetes:none:cluster/prod-cluster:none:none verified=1\n"
	if got := stdout.String(); got != wantOutput {
		t.Fatalf("stdout = %q, want %q", got, wantOutput)
	}
	if len(backend.executeParams) != 1 {
		t.Fatalf("ExecuteCypher calls = %d, want 1", len(backend.executeParams))
	}
	rows, ok := backend.executeParams[0]["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", backend.executeParams[0]["rows"])
	}
	if len(rows) != 1 {
		t.Fatalf("production materializer rows = %d, want 1", len(rows))
	}
	if got := rows[0]["platform_id"]; got != "platform:kubernetes:none:cluster/prod-cluster:none:none" {
		t.Fatalf("platform_id = %v, want canonical id", got)
	}
}

func TestRunMaterializePlatformPrerequisiteReportsOutputFailureSafely(t *testing.T) {
	backend := &recordingPlatformPrerequisiteBackend{repositoryCount: 1, platformCount: 1}
	original := openPlatformPrerequisiteBackend
	openPlatformPrerequisiteBackend = func(context.Context) (platformPrerequisiteBackend, func(), error) {
		return backend, func() {}, nil
	}
	t.Cleanup(func() { openPlatformPrerequisiteBackend = original })

	const sensitiveCause = "write unix /private/runner/socket: broken pipe"
	err := runMaterializePlatformPrerequisiteCommand(
		context.Background(),
		[]string{"-repo-id", "repo:test", "-kind", "kubernetes", "-name", "prod", "-locator", "cluster/prod"},
		failingPlatformPrerequisiteWriter{err: errors.New(sensitiveCause)},
		&bytes.Buffer{},
	)
	if err == nil {
		t.Fatal("runMaterializePlatformPrerequisiteCommand() error = nil, want output failure")
	}
	if strings.Contains(err.Error(), sensitiveCause) || strings.Contains(err.Error(), "/private/runner/socket") {
		t.Fatalf("error leaks output destination details: %q", err)
	}
	if !strings.Contains(err.Error(), "write verified prerequisite result") || !strings.Contains(err.Error(), "output destination") {
		t.Fatalf("error = %q, want safe actionable output guidance", err)
	}
}

type recordingPlatformPrerequisiteMaterializer struct {
	calls [][]reducer.InfrastructurePlatformRow
}

type failingPlatformPrerequisiteWriter struct {
	err error
}

func (w failingPlatformPrerequisiteWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (m *recordingPlatformPrerequisiteMaterializer) Materialize(
	_ context.Context,
	rows []reducer.InfrastructurePlatformRow,
) (reducer.InfrastructurePlatformResult, error) {
	m.calls = append(m.calls, append([]reducer.InfrastructurePlatformRow(nil), rows...))
	return reducer.InfrastructurePlatformResult{PlatformEdgesWritten: len(rows)}, nil
}

type stubPlatformPrerequisiteVerifier struct {
	repositoryCount int
	platformCount   int
}

type recordingPlatformPrerequisiteBackend struct {
	repositoryCount int
	platformCount   int
	executeParams   []map[string]any
	repositoryErr   error
	executeErr      error
	platformErr     error
}

func (b *recordingPlatformPrerequisiteBackend) ExecuteCypher(
	_ context.Context,
	_ string,
	params map[string]any,
) error {
	b.executeParams = append(b.executeParams, params)
	return b.executeErr
}

func (b *recordingPlatformPrerequisiteBackend) CountRepository(context.Context, string) (int, error) {
	return b.repositoryCount, b.repositoryErr
}

func (b *recordingPlatformPrerequisiteBackend) CountPlatform(context.Context, string) (int, error) {
	return b.platformCount, b.platformErr
}

func (v *stubPlatformPrerequisiteVerifier) CountRepository(context.Context, string) (int, error) {
	return v.repositoryCount, nil
}

func (v *stubPlatformPrerequisiteVerifier) CountPlatform(context.Context, string) (int, error) {
	return v.platformCount, nil
}
