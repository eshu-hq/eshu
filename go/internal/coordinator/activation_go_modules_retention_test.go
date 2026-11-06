// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

const goRotationRetentionWarning = "go reachability rotation is longer than half the generation retention cutoff"

// retentionWarningLogs runs one pass over a fleet and returns its log output.
func retentionWarningLogs(t *testing.T, fleetSize int, block string, cutoff time.Duration) string {
	t.Helper()
	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	var logs bytes.Buffer
	service := goModulesService(t, &now, &fakeStore{}, &fakeGoModuleRepositoryReader{fleet: goFleet(fleetSize)}, `{"mode":"broad"}`)
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	service.GenerationRetentionSoftCutoff = cutoff
	instance := goModulesInstance(now, goModulesActivationConfiguration(block, "scanner_worker", "scanner_worker"))
	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	return logs.String()
}

// TestGoModulesWarnsWhenRotationOutgrowsRetention is #10089. The commit-skew fence reads
// repository history back to the retention soft cutoff, and its bound is two rotations,
// so a rotation over half the cutoff makes it fence on a healthy scanner. The bound comes
// from the configured cutoff, and the comparison is strict.
func TestGoModulesWarnsWhenRotationOutgrowsRetention(t *testing.T) {
	t.Parallel()

	const daily = `{"derive_from_go_modules":{"enabled":true,"scan_interval":"24h"}}`
	const hourly = testGoModulesBlock
	week := 7 * 24 * time.Hour
	for _, tc := range []struct {
		name   string
		fleet  int
		block  string
		cutoff time.Duration
		want   bool
	}{
		{name: "four slots at the defaults warn", fleet: 1000, block: daily, cutoff: week, want: true},
		{name: "three slots at the defaults do not", fleet: 768, block: daily, cutoff: week},
		{name: "a longer configured cutoff does not", fleet: 1000, block: daily, cutoff: 2 * week},
		{name: "a shorter configured cutoff does", fleet: 256, block: daily, cutoff: 36 * time.Hour, want: true},
		{name: "a period exactly at half the cutoff does not", fleet: 257, block: hourly, cutoff: 4 * time.Hour},
		{name: "a period just past half the cutoff does", fleet: 257, block: hourly, cutoff: 4*time.Hour - time.Minute, want: true},
		{name: "no configured cutoff does not", fleet: 1000, block: daily},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logs := retentionWarningLogs(t, tc.fleet, tc.block, tc.cutoff)
			if got := strings.Contains(logs, goRotationRetentionWarning); got != tc.want {
				t.Fatalf("warned = %v, want %v; logs = %s", got, tc.want, logs)
			}
		})
	}
}

// TestGoModulesRetentionWarningNamesTheNumbersAndNoRepository pins what an operator reads
// at 3 AM, and that no repository value reaches the line.
func TestGoModulesRetentionWarningNamesTheNumbersAndNoRepository(t *testing.T) {
	t.Parallel()

	const daily = `{"derive_from_go_modules":{"enabled":true,"scan_interval":"24h"}}`
	logs := retentionWarningLogs(t, 1000, daily, 7*24*time.Hour)
	var line string
	for _, candidate := range strings.Split(logs, "\n") {
		if strings.Contains(candidate, goRotationRetentionWarning) {
			line = candidate
		}
	}
	for _, want := range []string{
		`"level":"WARN"`, `"reason":"go_rotation_exceeds_half_retention_cutoff"`, `"slot_count":4`,
		`"interval_seconds":86400`, `"rotation_period_seconds":345600`, `"retention_cutoff_seconds":604800`,
		`"total_candidates":1000`, `"collector_kind":"scanner_worker"`,
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("warning line = %s, want %s", line, want)
		}
	}
	for _, leak := range []string{"repository:r_", "github.com", "go-reachability://"} {
		if strings.Contains(line, leak) {
			t.Fatalf("warning line %s carries %q", line, leak)
		}
	}
}

// TestGoModulesRetentionWarningOnlyOnAPlannedPass keeps the warning to one line per plan
// bucket: a repeat pass stops at the planned probe, with no slot count to judge.
func TestGoModulesRetentionWarningOnlyOnAPlannedPass(t *testing.T) {
	t.Parallel()

	const daily = `{"derive_from_go_modules":{"enabled":true,"scan_interval":"24h"}}`
	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	reader := &fakeGoModuleRepositoryReader{fleet: goFleet(1000)}
	var logs bytes.Buffer
	service := goModulesService(t, &now, &fakeStore{}, reader, `{"mode":"broad"}`)
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	service.GenerationRetentionSoftCutoff = 7 * 24 * time.Hour
	instance := goModulesInstance(now, goModulesActivationConfiguration(daily, "scanner_worker", "scanner_worker"))

	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("first pass error = %v", err)
	}
	reader.planned = map[string]bool{reader.filters[0].RunID: true}
	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("repeat pass error = %v", err)
	}
	if got := strings.Count(logs.String(), goRotationRetentionWarning); got != 1 {
		t.Fatalf("warnings = %d, want 1 across a planned pass and a repeat pass", got)
	}
}
