// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/hookpreflight"
)

func TestAssistantHookPreflightLatencySamplesStayWithinBudget(t *testing.T) {
	cases := []struct {
		name string
		run  func() error
	}{
		{
			name: "evaluator_advisory",
			run: func() error {
				out := hookpreflight.Evaluate(hookpreflight.Input{
					Host:     "claude",
					Enabled:  true,
					Trigger:  "read",
					RepoPath: "services/api/handler.go",
					Budget:   hookpreflight.DefaultBudget,
				})
				if out.Decision != hookpreflight.DecisionAdvise {
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
			if summary.max >= hookpreflight.DefaultBudget {
				t.Fatalf("max latency = %s, want below %s", summary.max, hookpreflight.DefaultBudget)
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

func errUnexpectedAssistantHookDecision(out hookpreflight.Output) error {
	return fmt.Errorf("decision=%s reason=%s, want advise", out.Decision, out.Reason)
}

func runAssistantHookPreflightBenchmarkCommand(payload string, wantOutput bool) error {
	cmd := newAssistantHookPreflightCommand()
	cmd.SetIn(strings.NewReader(payload))
	out := new(strings.Builder)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--host", "claude", "--enabled", "--json", "--budget", hookpreflight.DefaultBudget.String()})
	if err := cmd.Execute(); err != nil {
		return err
	}
	if (out.Len() > 0) != wantOutput {
		return fmt.Errorf("stdout length = %d, want output=%v", out.Len(), wantOutput)
	}
	return nil
}
