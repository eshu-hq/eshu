// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoadSecretLinesRepairConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		env     map[string]string
		want    secretLinesRepairConfig
		wantErr string
	}{
		{"defaults on", nil, secretLinesRepairConfig{Enabled: true, PollInterval: 30 * time.Second}, ""},
		{"disabled", map[string]string{"ESHU_SECRET_LINES_REPAIR_ENABLED": "false"}, secretLinesRepairConfig{Enabled: false, PollInterval: 30 * time.Second}, ""},
		{"poll", map[string]string{"ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL": "5s"}, secretLinesRepairConfig{Enabled: true, PollInterval: 5 * time.Second}, ""},
		{"bad bool", map[string]string{"ESHU_SECRET_LINES_REPAIR_ENABLED": "maybe"}, secretLinesRepairConfig{}, "ESHU_SECRET_LINES_REPAIR_ENABLED"},
		{"bad duration", map[string]string{"ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL": "soon"}, secretLinesRepairConfig{}, "ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL"},
		{"too short", map[string]string{"ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL": "100ms"}, secretLinesRepairConfig{}, "ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL"},
	}
	for _, tc := range cases {
		got, err := loadSecretLinesRepairConfig(func(key string) string { return tc.env[key] })
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err = %v, want one naming %s", tc.name, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %+v, %v; want %+v", tc.name, got, err, tc.want)
		}
	}
}

// fakeRepairRunner records that Run started and blocks until its context ends.
type fakeRepairRunner struct{ started chan struct{} }

func (f fakeRepairRunner) Run(ctx context.Context) error {
	close(f.started)
	<-ctx.Done()
	return nil
}

func TestStartSecretLinesRepairRunsUntilShutdown(t *testing.T) {
	t.Parallel()

	runner := fakeRepairRunner{started: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	wait := startSecretLinesRepair(ctx, secretLinesRepairConfig{Enabled: true}, runner, slog.New(slog.DiscardHandler))
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("repair runner did not start")
	}
	cancel()
	waited := make(chan struct{})
	go func() { wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after shutdown")
	}
}

func TestStartSecretLinesRepairDisabledRunsNothing(t *testing.T) {
	t.Parallel()

	runner := fakeRepairRunner{started: make(chan struct{})}
	var logs strings.Builder
	wait := startSecretLinesRepair(context.Background(), secretLinesRepairConfig{Enabled: false}, runner,
		slog.New(slog.NewTextHandler(&logs, nil)))
	wait()
	select {
	case <-runner.started:
		t.Fatal("disabled repair runner started")
	default:
	}
	if !strings.Contains(logs.String(), "legacy scan") {
		t.Fatalf("disabled log = %q, want it to say reads stay on the legacy scan", logs.String())
	}
}
