// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// TestRenderStatusIncludesSourcePacing proves AC6 on the status surface:
// each source publishes whether intake is slowed and why, with the
// claimable reducer observation behind the verdict, in both the JSON and
// text renderings. The healthy source publishes unslowed beside the slowed
// one, so the operator sees per-source isolation at a glance.
func TestRenderStatusIncludesSourcePacing(t *testing.T) {
	t.Parallel()

	engagedSince := time.Date(2026, 11, 21, 11, 0, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC),
			Coordinator: &status.CoordinatorSnapshot{
				SourcePacing: []collector.SourcePacingSnapshot{
					{
						SourceSystem:       "vuln-intel",
						Slowed:             true,
						Reason:             "source_behind",
						OldestClaimableAge: 2 * time.Hour,
						ClaimableDepth:     400,
						EngagedSince:       engagedSince,
					},
					{
						SourceSystem:       "git",
						OldestClaimableAge: time.Minute,
						ClaimableDepth:     3,
					},
				},
			},
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	body := string(payload)
	for _, want := range []string{
		`"source_pacing"`,
		`"source_system": "vuln-intel"`,
		`"slowed": true`,
		`"reason": "source_behind"`,
		`"claimable_depth": 400`,
		`"oldest_claimable_age_seconds": 7200`,
		`"source_system": "git"`,
		`"slowed": false`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("RenderJSON() missing %q in payload:\n%s", want, body)
		}
	}

	text := status.RenderText(report)
	for _, want := range []string{"Source pacing:", "vuln-intel", "slowed=true", "reason=source_behind", "git"} {
		if !strings.Contains(text, want) {
			t.Fatalf("RenderText() missing %q in output:\n%s", want, text)
		}
	}
}

// TestRenderStatusIncludesSourcePacingStuck proves the stuck flag rides
// every rendering: a stuck engagement serializes stuck=true in JSON and
// prints stuck=true in text, while a merely slowed one omits both.
func TestRenderStatusIncludesSourcePacingStuck(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC),
			Coordinator: &status.CoordinatorSnapshot{
				SourcePacing: []collector.SourcePacingSnapshot{
					{
						SourceSystem:       "vuln-intel",
						Slowed:             true,
						Reason:             "source_behind",
						OldestClaimableAge: 2 * time.Hour,
						ClaimableDepth:     400,
						EngagedSince:       time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC),
						Stuck:              true,
					},
					{
						SourceSystem:       "git",
						Slowed:             true,
						Reason:             "source_behind",
						OldestClaimableAge: 2 * time.Hour,
						ClaimableDepth:     400,
						EngagedSince:       time.Date(2026, 11, 21, 11, 30, 0, 0, time.UTC),
					},
				},
			},
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	body := string(payload)
	if !strings.Contains(body, `"stuck": true`) {
		t.Fatalf("RenderJSON() missing stuck flag in payload:\n%s", body)
	}
	if got := strings.Count(body, `"stuck"`); got != 1 {
		t.Fatalf("RenderJSON() has %d stuck keys, want 1 (only the stuck source):\n%s", got, body)
	}

	text := status.RenderText(report)
	if got := strings.Count(text, "stuck=true"); got != 1 {
		t.Fatalf("RenderText() has %d stuck=true markers, want 1:\n%s", got, text)
	}
}
