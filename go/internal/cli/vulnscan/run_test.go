// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

const (
	repoRunReadyZero    = `{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-1"},"evidence_sources":[{"family":"package.consumption","fact_count":1,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":20,"freshness":"fresh"}],"freshness":"fresh"}},"truth":{"level":"exact"},"error":null}`
	repoRunFindings     = `{"data":{"findings":[{"finding_id":"finding-1","cve_id":"CVE-2026-0001","package_name":"ws","impact_status":"affected_exact"}],"count":1,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_with_findings","target_scope":{"repository_id":"repo-1"},"evidence_sources":[{"family":"package.consumption","fact_count":2,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":80,"freshness":"fresh"}],"freshness":"fresh"}},"truth":{"level":"exact"},"error":null}`
	repoRunNotConfig    = `{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"not_configured","target_scope":{"repository_id":"repo-1"},"missing_evidence":["advisory_sources"]}},"truth":{"level":"exact"},"error":null}`
	repoRunStaleCache   = `{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-1"},"evidence_sources":[{"family":"package.consumption","fact_count":3,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":50,"freshness":"stale"}],"freshness":"stale"}},"truth":{"level":"exact"},"error":null}`
	repoRunUnsupported  = `{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"unsupported","target_scope":{"repository_id":"repo-1"},"missing_evidence":["unsupported_targets"]}},"truth":{"level":"exact"},"error":null}`
	repoRunServerError  = `{"data":{"findings":[],"count":0,"limit":50,"truncated":false},"truth":{"level":"exact"},"error":{"message":"impact findings unavailable"}}`
	repoRunRepositories = `{"count":1,"repositories":[{"id":"repo-1","name":"one","path":"/work/repo","local_path":"/work/repo"}]}`
)

// fakeRepoClient answers the two reads RunRepo makes: the repository listing
// (through Get, as reposelector.Resolve issues it) and the impact findings
// (through GetEnvelope). It never dials anything.
type fakeRepoClient struct {
	repositories string
	findings     string
	findingsErr  error
	requests     []string
}

func (c *fakeRepoClient) Get(path string, result any) error {
	c.requests = append(c.requests, path)
	switch {
	case strings.HasPrefix(path, "/api/v0/repositories"):
		return json.Unmarshal([]byte(c.repositories), result)
	case strings.HasPrefix(path, "/api/v0/status/pipeline"):
		return json.Unmarshal([]byte(`{"health":{"state":"healthy"},"generation_history":{"completed":1}}`), result)
	default:
		return fmt.Errorf("unexpected GET %s", path)
	}
}

func (c *fakeRepoClient) GetEnvelope(path string, result any) error {
	c.requests = append(c.requests, path)
	if c.findingsErr != nil {
		return c.findingsErr
	}
	return json.Unmarshal([]byte(c.findings), result)
}

// fakeScanRuntime is a scan.Runtime that reaches ready without touching PATH,
// a child process, or the clock. statusErr, when set, fails the preflight the
// way an unreachable API does.
func fakeScanRuntime(client scan.Client, statusErr error) scan.Runtime {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	return scan.Runtime{
		Client:     client,
		ServiceURL: "http://api.test",
		Environ:    []string{"PATH=/bin"},
		LookPath:   func(string) (string, error) { return "/bin/eshu-bootstrap-index", nil },
		RunBootstrap: func(context.Context, string, []string, []string, io.Writer, io.Writer) error {
			return nil
		},
		FetchStatus: func(scan.Client) (scan.PipelineStatus, error) {
			if statusErr != nil {
				return scan.PipelineStatus{}, statusErr
			}
			return scan.PipelineStatus{
				Health:            scan.Health{State: "healthy"},
				GenerationHistory: scan.GenerationHistory{Completed: 1},
			}, nil
		},
		FetchQueryProbe: func(scan.Client) (map[string]any, error) {
			return map[string]any{"data": map[string]any{"repositories": []any{}}}, nil
		},
		Now: func() time.Time {
			now = now.Add(time.Second)
			return now
		},
		Wait: func(context.Context, time.Duration) error { return nil },
	}
}

func repoRunOptions(jsonOut bool) RepoOptions {
	return RepoOptions{
		Scan: scan.Options{
			JSON:         jsonOut,
			Wait:         true,
			Timeout:      time.Minute,
			PollInterval: time.Millisecond,
			Target:       scan.Target{Path: "/work/repo", Root: "/work/repo", Kind: "repository"},
		},
		Limit: 50,
	}
}

func decodeRunEnvelope(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(stdout) error = %v; stdout=%q", err, out.String())
	}
	return payload
}

// TestRunRepoOutcomeContract drives the real RunRepo through every outcome
// the command distinguishes and pins, for each, the returned value's class
// and text, the exit code a *Failure carries, the readiness state the
// envelope reports, and whether the envelope carries an error member. These
// are the rows the wrapper's exit-code mapping depends on; a change to any
// one is a change to the published scanner contract.
func TestRunRepoOutcomeContract(t *testing.T) {
	tests := []struct {
		name          string
		client        *fakeRepoClient
		statusErr     error
		wait          bool
		repoID        string
		wantCode      int // 0 means success, -1 means an unclassified error
		wantMessage   string
		wantReadiness string
		wantEnvError  bool
	}{
		{
			name:          "ready zero findings",
			client:        &fakeRepoClient{repositories: repoRunRepositories, findings: repoRunReadyZero},
			wait:          true,
			wantCode:      0,
			wantReadiness: "ready_zero_findings",
		},
		{
			name:          "findings present",
			client:        &fakeRepoClient{repositories: repoRunRepositories, findings: repoRunFindings},
			wait:          true,
			wantCode:      3,
			wantMessage:   "vulnerability findings present",
			wantReadiness: "ready_with_findings",
		},
		{
			name:          "server not configured",
			client:        &fakeRepoClient{repositories: repoRunRepositories, findings: repoRunNotConfig},
			wait:          true,
			wantCode:      4,
			wantMessage:   "vulnerability scan did not reach a clean ready-zero result: not_configured",
			wantReadiness: "not_configured",
			wantEnvError:  true,
		},
		{
			name:          "stale advisory cache fails closed",
			client:        &fakeRepoClient{repositories: repoRunRepositories, findings: repoRunStaleCache},
			wait:          true,
			wantCode:      4,
			wantMessage:   "vuln-scan fail-closed: advisory_cache_stale",
			wantReadiness: "evidence_incomplete",
			wantEnvError:  true,
		},
		{
			name:          "unsupported target evidence",
			client:        &fakeRepoClient{repositories: repoRunRepositories, findings: repoRunUnsupported},
			wait:          true,
			wantCode:      5,
			wantMessage:   "vulnerability scan encountered unsupported target evidence",
			wantReadiness: "unsupported",
			wantEnvError:  true,
		},
		{
			name:          "server error member",
			client:        &fakeRepoClient{repositories: repoRunRepositories, findings: repoRunServerError},
			wait:          true,
			wantCode:      4,
			wantMessage:   "impact findings unavailable",
			wantReadiness: "evidence_incomplete",
			wantEnvError:  true,
		},
		{
			name:          "scan not ready without wait",
			client:        &fakeRepoClient{repositories: repoRunRepositories, findings: repoRunReadyZero},
			wait:          false,
			wantCode:      4,
			wantMessage:   "vulnerability scan target is not ready; rerun with --wait=true before reading findings",
			wantReadiness: "target_incomplete",
			wantEnvError:  true,
		},
		{
			name:          "scan preflight failure",
			client:        &fakeRepoClient{repositories: repoRunRepositories, findings: repoRunReadyZero},
			statusErr:     errors.New("connection refused"),
			wait:          true,
			wantCode:      -1,
			wantMessage:   "scan preflight status check: connection refused",
			wantReadiness: "target_incomplete",
			wantEnvError:  true,
		},
		{
			name:          "selector resolves nothing",
			client:        &fakeRepoClient{repositories: `{"count":0,"repositories":[]}`, findings: repoRunReadyZero},
			wait:          true,
			wantCode:      -1,
			wantMessage:   `resolve scanned repository: resolve repo selector "/work/repo": no matching repository`,
			wantReadiness: "evidence_incomplete",
			wantEnvError:  true,
		},
		{
			name:          "findings transport failure",
			client:        &fakeRepoClient{repositories: repoRunRepositories, findingsErr: errors.New("boom")},
			wait:          true,
			wantCode:      -1,
			wantMessage:   "fetch vulnerability impact findings: boom",
			wantReadiness: "evidence_incomplete",
			wantEnvError:  true,
		},
		{
			name:          "explicit repo id skips resolution",
			client:        &fakeRepoClient{repositories: `{"count":0,"repositories":[]}`, findings: repoRunReadyZero},
			wait:          true,
			repoID:        "repo-1",
			wantCode:      0,
			wantReadiness: "ready_zero_findings",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			opts := repoRunOptions(true)
			opts.Scan.Wait = tt.wait
			opts.RepoID = tt.repoID
			deps := RepoDeps{
				Client:      tt.client,
				ServiceURL:  "http://api.test",
				ScanRuntime: fakeScanRuntime(tt.client, tt.statusErr),
				Stdout:      stdout,
				Stderr:      stderr,
			}

			err := RunRepo(context.Background(), deps, opts)

			var failure *Failure
			switch {
			case tt.wantCode == 0:
				if err != nil {
					t.Fatalf("RunRepo() error = %v, want nil", err)
				}
			case tt.wantCode > 0:
				if !errors.As(err, &failure) {
					t.Fatalf("RunRepo() error = %T %v, want *Failure", err, err)
				}
				if failure.Code != tt.wantCode {
					t.Fatalf("Failure.Code = %d, want %d", failure.Code, tt.wantCode)
				}
				if failure.Message != tt.wantMessage {
					t.Fatalf("Failure.Message = %q, want %q", failure.Message, tt.wantMessage)
				}
			default:
				if err == nil {
					t.Fatal("RunRepo() error = nil, want an unclassified error")
				}
				if errors.As(err, &failure) {
					t.Fatalf("RunRepo() error = *Failure %v, want a plain error", failure)
				}
				if err.Error() != tt.wantMessage {
					t.Fatalf("RunRepo() error = %q, want %q", err.Error(), tt.wantMessage)
				}
			}

			payload := decodeRunEnvelope(t, stdout)
			data, ok := payload["data"].(map[string]any)
			if !ok {
				t.Fatalf("payload[data] = %T, want object", payload["data"])
			}
			if got := data["readiness_state"]; got != tt.wantReadiness {
				t.Fatalf("data[readiness_state] = %#v, want %q", got, tt.wantReadiness)
			}
			if got := payload["error"] != nil; got != tt.wantEnvError {
				t.Fatalf("payload[error] present = %v, want %v (error=%#v)", got, tt.wantEnvError, payload["error"])
			}
			if payload["truth"] == nil {
				t.Fatal("payload[truth] = nil, want a truth block on every path")
			}
			if data["scan"] == nil {
				t.Fatal("data[scan] = nil, want the scan result carried into the envelope")
			}
			if data["scan_performance"] == nil {
				t.Fatal("data[scan_performance] = nil, want the performance stamp on every path")
			}
			if data["report"] == nil {
				t.Fatal("data[report] = nil, want the report on every JSON path")
			}
		})
	}
}

// TestRunRepoRejectsMissingStreams pins that a wiring gap fails before the
// scan runs rather than as a nil-writer panic part way through, and that a
// local runtime supplied alongside the bad wiring is still stopped rather
// than leaked.
func TestRunRepoRejectsMissingStreams(t *testing.T) {
	client := &fakeRepoClient{repositories: repoRunRepositories, findings: repoRunReadyZero}
	closed := 0
	deps := RepoDeps{
		Client:            client,
		ScanRuntime:       fakeScanRuntime(client, nil),
		CloseLocalRuntime: func() error { closed++; return nil },
	}
	err := RunRepo(context.Background(), deps, repoRunOptions(true))
	if err == nil || !strings.Contains(err.Error(), "requires Stdout and Stderr") {
		t.Fatalf("RunRepo() error = %v, want the missing-stream error", err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("client requests = %v, want none before the wiring check", client.requests)
	}
	if closed != 1 {
		t.Fatalf("CloseLocalRuntime calls = %d, want 1 even when the wiring check fails", closed)
	}
}
