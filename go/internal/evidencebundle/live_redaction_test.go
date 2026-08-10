// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencebundle

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestBuildLiveBundleNeverLeaksHostedGovernanceCanaries is the negative-
// leakage check required by #4045. It renders a live bundle from a
// realistic LiveSnapshot (status-derived enum states, reason codes, and
// counts -- the only shapes cmd/eshu's decoders ever populate) and asserts
// the rendered JSON never contains any of the shared hosted-governance
// registry's forbidden canaries on SurfaceOnboardingArtifacts, the surface a
// portable evidence bundle actually is. This checks the output against the
// repo-wide canonical secret-shape taxonomy in go/internal/redact, not just
// this package's own bespoke regexes in bundle.go/validate.go -- reusing the
// shared registry rather than reinventing pattern coverage.
func TestBuildLiveBundleNeverLeaksHostedGovernanceCanaries(t *testing.T) {
	snapshot := LiveSnapshot{
		RepositoryCount: 12,
		HealthState:     "degraded",
		HealthReasons:   []string{"queue backlog", "domain aws_relationship blocked"},
		Queue: LiveQueueSnapshot{
			Pending:    4,
			InFlight:   2,
			Retrying:   1,
			Succeeded:  100,
			Failed:     0,
			DeadLetter: 1,
		},
		QueueBlockedCount: 1,
		ScopeActivity:     LiveScopeActivitySnapshot{Active: 12, Changed: 3, Unchanged: 9},
		GenerationHistory: LiveGenerationHistorySnapshot{Active: 1, Pending: 0, Completed: 40, Failed: 0},
		StageSummaries: []LiveStageSummarySnapshot{
			{Stage: "parse", Pending: 2, Retrying: 0, Failed: 0, DeadLetter: 0},
		},
		DomainBacklogs: []LiveDomainBacklogSnapshot{
			{Domain: "aws_relationship", Outstanding: 1, Retrying: 0, Failed: 0, DeadLetter: 1},
		},
		Collectors: []LiveCollectorSnapshot{
			{CollectorKind: "git", StatusCategory: "ready", Health: "healthy"},
			{CollectorKind: "aws", StatusCategory: "failed", Health: "unhealthy"},
		},
		SemanticExtraction: LiveSemanticExtractionSnapshot{
			State:              "unavailable",
			Reason:             "provider_not_configured",
			ProviderConfigured: false,
		},
	}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:local", CreatedAt: fixedLiveCreatedAt})
	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate(BuildLiveBundle(realistic snapshot)) error = %v", err)
	}

	raw, err := RenderJSON(bundle)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}

	registry := redact.HostedGovernanceRegistry()
	if err := registry.AssertNoForbiddenCanary(redact.SurfaceOnboardingArtifacts, raw); err != nil {
		t.Fatalf("AssertNoForbiddenCanary(SurfaceOnboardingArtifacts) error = %v\nbundle: %s", err, raw)
	}
}
