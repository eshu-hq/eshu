// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/liveness"
)

func TestLoadGenerationLivenessConfigProgressWindow(t *testing.T) {
	tests := []struct {
		name            string
		env             map[string]string
		wantWindow      time.Duration
		wantClampedFrom time.Duration
	}{
		{name: "default", wantWindow: 10 * time.Minute},
		{
			name:       "explicit above poll interval",
			env:        map[string]string{generationLivenessProgressWindowEnv: "20m"},
			wantWindow: 20 * time.Minute,
		},
		{
			name:            "below poll interval is clamped",
			env:             map[string]string{generationLivenessProgressWindowEnv: "1m"},
			wantWindow:      5 * time.Minute,
			wantClampedFrom: time.Minute,
		},
		{
			name: "default below a raised poll interval is clamped",
			env: map[string]string{
				generationLivenessPollIntervalEnv: "15m",
			},
			wantWindow:      15 * time.Minute,
			wantClampedFrom: 10 * time.Minute,
		},
		{
			name:       "unparseable falls back to default",
			env:        map[string]string{generationLivenessProgressWindowEnv: "soon"},
			wantWindow: 10 * time.Minute,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadGenerationLivenessConfig(func(key string) string { return tc.env[key] })
			if got := cfg.Runner.Policy.ProgressWindow; got != tc.wantWindow {
				t.Fatalf("ProgressWindow = %v, want %v", got, tc.wantWindow)
			}
			if got := cfg.ProgressWindowClampedFrom; got != tc.wantClampedFrom {
				t.Fatalf("ProgressWindowClampedFrom = %v, want %v", got, tc.wantClampedFrom)
			}
		})
	}
}

func TestWarnGenerationLivenessProgressWindowClampLogsOnlyWhenClamped(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	warnGenerationLivenessProgressWindowClamp(context.Background(), logger, loadGenerationLivenessConfig(nil))
	if buf.Len() != 0 {
		t.Fatalf("unclamped config logged: %s", buf.String())
	}

	cfg := loadGenerationLivenessConfig(func(key string) string {
		if key == generationLivenessProgressWindowEnv {
			return "30s"
		}
		return ""
	})
	warnGenerationLivenessProgressWindowClamp(context.Background(), logger, cfg)
	out := buf.String()
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("want exactly one warning line, got %q", out)
	}
	for _, want := range []string{
		`"level":"WARN"`,
		`"configured_progress_window":"30s"`,
		`"effective_progress_window":"5m0s"`,
		`"env":"ESHU_GENERATION_LIVENESS_PROGRESS_WINDOW"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("warning %q missing %s", out, want)
		}
	}
}

func TestPostgresGenerationLivenessRecovererForwardsProgressWindow(t *testing.T) {
	cfg := loadGenerationLivenessConfig(func(key string) string {
		if key == generationLivenessProgressWindowEnv {
			return "12m"
		}
		return ""
	})
	observer := activeGenerationAgeObserverFor(nil, cfg)
	if got := observer.policy.ProgressWindow; got != 12*time.Minute {
		t.Fatalf("gauge observer ProgressWindow = %v, want 12m", got)
	}
	var _ liveness.Recoverer = postgresGenerationLivenessRecoverer{}
}
