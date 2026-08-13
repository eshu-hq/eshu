// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"testing"
	"time"
)

type preflightBenchmarkCase struct {
	name         string
	input        Input
	wantDecision string
	wantReason   string
}

func preflightBenchmarkCases() []preflightBenchmarkCase {
	return []preflightBenchmarkCase{
		{
			name: "advisory_repo_path",
			input: Input{
				Host:     "claude",
				Enabled:  true,
				Trigger:  "read",
				RepoPath: "services/api/handler.go",
				Budget:   DefaultBudget,
			},
			wantDecision: DecisionAdvise,
			wantReason:   reasonBoundedPreflight,
		},
		{
			name: "stale_service",
			input: Input{
				Host:      "claude",
				Enabled:   true,
				Trigger:   "search",
				Service:   "checkout",
				Freshness: freshnessStale,
				Budget:    DefaultBudget,
			},
			wantDecision: DecisionAdvise,
			wantReason:   reasonStaleIndex,
		},
		{
			name: "timeout_fail_open",
			input: Input{
				Host:     "claude",
				Enabled:  true,
				Trigger:  "read",
				RepoPath: "services/api/handler.go",
				Budget:   DefaultBudget,
				Elapsed:  DefaultBudget + time.Millisecond,
			},
			wantDecision: decisionSkip,
			wantReason:   reasonTimeout,
		},
		{
			name: "unsupported_host",
			input: Input{
				Host:     "codex",
				Enabled:  true,
				Trigger:  "read",
				RepoPath: "services/api/handler.go",
				Budget:   DefaultBudget,
			},
			wantDecision: decisionSkip,
			wantReason:   reasonUnsupportedHost,
		},
		{
			name: "broad_scope",
			input: Input{
				Host:    "claude",
				Enabled: true,
				Trigger: "glob",
				Budget:  DefaultBudget,
			},
			wantDecision: decisionSkip,
			wantReason:   reasonBroadScope,
		},
		{
			name: "permission_denied",
			input: Input{
				Host:       "claude",
				Enabled:    true,
				Trigger:    "read",
				RepoPath:   "services/api/handler.go",
				Permission: permissionDenied,
				Budget:     DefaultBudget,
			},
			wantDecision: decisionSkip,
			wantReason:   reasonPermissionDenied,
		},
	}
}

func TestAssistantHookPreflightBenchmarkCasesCoverContract(t *testing.T) {
	cases := preflightBenchmarkCases()
	if len(cases) < 6 {
		t.Fatalf("benchmark cases = %d, want at least 6 contract paths", len(cases))
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		if tc.name == "" {
			t.Fatal("benchmark case has empty name")
		}
		if tc.input.Budget != DefaultBudget {
			t.Fatalf("%s budget = %s, want default %s", tc.name, tc.input.Budget, DefaultBudget)
		}
		out := Evaluate(tc.input)
		if out.Decision != tc.wantDecision || out.Reason != tc.wantReason {
			t.Fatalf("%s decision=%s reason=%s, want %s/%s", tc.name, out.Decision, out.Reason, tc.wantDecision, tc.wantReason)
		}
		if tc.wantReason != reasonTimeout && out.ElapsedMS != 0 {
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
	for _, tc := range preflightBenchmarkCases() {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				out := Evaluate(tc.input)
				if out.Decision != tc.wantDecision || out.Reason != tc.wantReason {
					b.Fatalf("decision=%s reason=%s, want %s/%s", out.Decision, out.Reason, tc.wantDecision, tc.wantReason)
				}
			}
		})
	}
}
