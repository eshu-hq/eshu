// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

type assistantHookPreflightBenchmarkCase struct {
	name         string
	input        assistantHookPreflightInput
	wantDecision string
	wantReason   string
}

func assistantHookPreflightBenchmarkCases() []assistantHookPreflightBenchmarkCase {
	return []assistantHookPreflightBenchmarkCase{
		{
			name: "advisory_repo_path",
			input: assistantHookPreflightInput{
				Host:     "claude",
				Enabled:  true,
				Trigger:  "read",
				RepoPath: "services/api/handler.go",
				Budget:   assistantHookDefaultBudget,
			},
			wantDecision: assistantHookDecisionAdvise,
			wantReason:   assistantHookReasonBoundedPreflight,
		},
		{
			name: "stale_service",
			input: assistantHookPreflightInput{
				Host:      "claude",
				Enabled:   true,
				Trigger:   "search",
				Service:   "checkout",
				Freshness: assistantHookFreshnessStale,
				Budget:    assistantHookDefaultBudget,
			},
			wantDecision: assistantHookDecisionAdvise,
			wantReason:   assistantHookReasonStaleIndex,
		},
		{
			name: "timeout_fail_open",
			input: assistantHookPreflightInput{
				Host:     "claude",
				Enabled:  true,
				Trigger:  "read",
				RepoPath: "services/api/handler.go",
				Budget:   assistantHookDefaultBudget,
				Elapsed:  assistantHookDefaultBudget + time.Millisecond,
			},
			wantDecision: assistantHookDecisionSkip,
			wantReason:   assistantHookReasonTimeout,
		},
		{
			name: "unsupported_host",
			input: assistantHookPreflightInput{
				Host:     "codex",
				Enabled:  true,
				Trigger:  "read",
				RepoPath: "services/api/handler.go",
				Budget:   assistantHookDefaultBudget,
			},
			wantDecision: assistantHookDecisionSkip,
			wantReason:   assistantHookReasonUnsupportedHost,
		},
		{
			name: "broad_scope",
			input: assistantHookPreflightInput{
				Host:    "claude",
				Enabled: true,
				Trigger: "glob",
				Budget:  assistantHookDefaultBudget,
			},
			wantDecision: assistantHookDecisionSkip,
			wantReason:   assistantHookReasonBroadScope,
		},
		{
			name: "permission_denied",
			input: assistantHookPreflightInput{
				Host:       "claude",
				Enabled:    true,
				Trigger:    "read",
				RepoPath:   "services/api/handler.go",
				Permission: assistantHookPermissionDenied,
				Budget:     assistantHookDefaultBudget,
			},
			wantDecision: assistantHookDecisionSkip,
			wantReason:   assistantHookReasonPermissionDenied,
		},
	}
}

func TestAssistantHookPreflightBenchmarkCasesCoverContract(t *testing.T) {
	cases := assistantHookPreflightBenchmarkCases()
	if len(cases) < 6 {
		t.Fatalf("benchmark cases = %d, want at least 6 contract paths", len(cases))
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		if tc.name == "" {
			t.Fatal("benchmark case has empty name")
		}
		if tc.input.Budget != assistantHookDefaultBudget {
			t.Fatalf("%s budget = %s, want default %s", tc.name, tc.input.Budget, assistantHookDefaultBudget)
		}
		out := evaluateAssistantHookPreflight(tc.input)
		if out.Decision != tc.wantDecision || out.Reason != tc.wantReason {
			t.Fatalf("%s decision=%s reason=%s, want %s/%s", tc.name, out.Decision, out.Reason, tc.wantDecision, tc.wantReason)
		}
		if tc.wantReason != assistantHookReasonTimeout && out.ElapsedMS != 0 {
			t.Fatalf("%s elapsed_ms = %d, want deterministic zero for evaluator benchmark", tc.name, out.ElapsedMS)
		}
		seen[tc.name] = true
	}
	for _, name := range []string{
		"advisory_repo_path",
		"stale_service",
		"timeout_fail_open",
		"unsupported_host",
		"broad_scope",
		"permission_denied",
	} {
		if !seen[name] {
			t.Fatalf("benchmark case %q missing", name)
		}
	}
}

func BenchmarkAssistantHookPreflightEvaluate(b *testing.B) {
	for _, tc := range assistantHookPreflightBenchmarkCases() {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				out := evaluateAssistantHookPreflight(tc.input)
				if out.Decision != tc.wantDecision || out.Reason != tc.wantReason {
					b.Fatalf("decision=%s reason=%s, want %s/%s", out.Decision, out.Reason, tc.wantDecision, tc.wantReason)
				}
			}
		})
	}
}

func TestAssistantHookPreflightLatencySamplesStayWithinBudget(t *testing.T) {
	cases := []struct {
		name string
		run  func() error
	}{
		{
			name: "evaluator_advisory",
			run: func() error {
				out := evaluateAssistantHookPreflight(assistantHookPreflightInput{
					Host:     "claude",
					Enabled:  true,
					Trigger:  "read",
					RepoPath: "services/api/handler.go",
					Budget:   assistantHookDefaultBudget,
				})
				if out.Decision != assistantHookDecisionAdvise {
					return errUnexpectedAssistantHookDecision(out)
				}
				return nil
			},
		},
		{
			name: "command_advisory_json",
			run: func() error {
				return runAssistantHookPreflightBenchmarkCommand(`{
					"hook_event_name": "PreToolUse",
					"cwd": "workspace",
					"tool_name": "Read",
					"tool_input": {"file_path": "services/api/handler.go"}
				}`, true)
			},
		},
		{
			name: "command_malformed_fail_open",
			run: func() error {
				return runAssistantHookPreflightBenchmarkCommand(`not json`, false)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := measureAssistantHookLatencySamples(t, tc.run)
			if summary.max >= assistantHookDefaultBudget {
				t.Fatalf("max latency = %s, want below %s", summary.max, assistantHookDefaultBudget)
			}
			t.Logf("latency p50=%s p95=%s max=%s samples=%d", summary.p50, summary.p95, summary.max, summary.samples)
		})
	}
}

func BenchmarkAssistantHookPreflightCommand(b *testing.B) {
	cases := []struct {
		name       string
		payload    string
		wantOutput bool
	}{
		{
			name: "advisory_json",
			payload: `{
				"hook_event_name": "PreToolUse",
				"cwd": "workspace",
				"tool_name": "Read",
				"tool_input": {"file_path": "services/api/handler.go"}
			}`,
			wantOutput: true,
		},
		{
			name:       "malformed_payload_fail_open",
			payload:    `not json`,
			wantOutput: false,
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cmd := newAssistantHookPreflightCommand()
				cmd.SetIn(strings.NewReader(tc.payload))
				out := new(strings.Builder)
				cmd.SetOut(out)
				cmd.SetArgs([]string{"--host", "claude", "--enabled", "--json", "--budget", (200 * time.Millisecond).String()})
				if err := cmd.Execute(); err != nil {
					b.Fatalf("Execute(): %v", err)
				}
				if (out.Len() > 0) != tc.wantOutput {
					b.Fatalf("stdout length = %d, want output=%v", out.Len(), tc.wantOutput)
				}
			}
		})
	}
}

type assistantHookLatencySummary struct {
	p50     time.Duration
	p95     time.Duration
	max     time.Duration
	samples int
}

func measureAssistantHookLatencySamples(t *testing.T, run func() error) assistantHookLatencySummary {
	t.Helper()
	const sampleCount = 1000
	samples := make([]time.Duration, 0, sampleCount)
	for i := 0; i < sampleCount; i++ {
		start := time.Now()
		if err := run(); err != nil {
			t.Fatalf("sample %d: %v", i, err)
		}
		samples = append(samples, time.Since(start))
	}
	sort.Slice(samples, func(i, j int) bool {
		return samples[i] < samples[j]
	})
	return assistantHookLatencySummary{
		p50:     samples[sampleCount/2],
		p95:     samples[(sampleCount*95)/100],
		max:     samples[sampleCount-1],
		samples: sampleCount,
	}
}

func errUnexpectedAssistantHookDecision(out assistantHookPreflightOutput) error {
	return fmt.Errorf("decision=%s reason=%s, want advise", out.Decision, out.Reason)
}

func runAssistantHookPreflightBenchmarkCommand(payload string, wantOutput bool) error {
	cmd := newAssistantHookPreflightCommand()
	cmd.SetIn(strings.NewReader(payload))
	out := new(strings.Builder)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--host", "claude", "--enabled", "--json", "--budget", assistantHookDefaultBudget.String()})
	if err := cmd.Execute(); err != nil {
		return err
	}
	if (out.Len() > 0) != wantOutput {
		return fmt.Errorf("stdout length = %d, want output=%v", out.Len(), wantOutput)
	}
	return nil
}
