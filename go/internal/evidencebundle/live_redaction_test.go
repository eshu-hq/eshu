// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencebundle

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

// realisticLiveSnapshot is the shape cmd/eshu's decoders actually populate:
// enum states, reason codes, and counts. Tests below mutate one field of it to
// inject a canary, so the only difference between a passing and a failing case
// is the injected value.
func realisticLiveSnapshot() LiveSnapshot {
	return LiveSnapshot{
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
}

// TestBuildLiveBundleRendersCleanSnapshotWithoutCanaries is the control for the
// injection cases below: a snapshot carrying no sensitive value must validate
// and render, so a failure there means the injection tests are meaningful
// rather than failing for an unrelated reason.
func TestBuildLiveBundleRendersCleanSnapshotWithoutCanaries(t *testing.T) {
	bundle := BuildLiveBundle(realisticLiveSnapshot(), LiveBundleOptions{
		ScopeID:   "live:local",
		CreatedAt: fixedLiveCreatedAt,
	})
	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate(clean snapshot) error = %v", err)
	}
	raw, err := RenderJSON(bundle)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	registry := redact.HostedGovernanceRegistry()
	if err := registry.AssertNoForbiddenCanary(redact.SurfaceOnboardingArtifacts, raw); err != nil {
		t.Fatalf("clean snapshot tripped a canary: %v\nbundle: %s", err, raw)
	}
}

// TestBuildLiveBundleNeverLeaksHostedGovernanceCanaries injects every canary
// the shared hosted-governance registry forbids on SurfaceOnboardingArtifacts
// into the free-text status fields a live bundle carries, and requires that
// each one is caught rather than serialized.
//
// The earlier version of this test built a snapshot with no canary in it at
// all and then asserted the rendered bundle contained none. That assertion was
// satisfied by the absence of the input, not by any redaction behavior, so it
// would have passed with every check removed.
func TestBuildLiveBundleNeverLeaksHostedGovernanceCanaries(t *testing.T) {
	registry := redact.HostedGovernanceRegistry()
	forbidden := registry.ForbiddenCanaries(redact.SurfaceOnboardingArtifacts)
	if len(forbidden) == 0 {
		t.Fatal("registry reports no forbidden canaries for SurfaceOnboardingArtifacts; this test would be vacuous")
	}

	// Each carrier is a free-text field that a status response could populate
	// and that therefore reaches the rendered bundle.
	carriers := map[string]func(*LiveSnapshot, string){
		"health_reason":     func(s *LiveSnapshot, v string) { s.HealthReasons = []string{v} },
		"semantic_reason":   func(s *LiveSnapshot, v string) { s.SemanticExtraction.Reason = v },
		"semantic_state":    func(s *LiveSnapshot, v string) { s.SemanticExtraction.State = v },
		"collector_health":  func(s *LiveSnapshot, v string) { s.Collectors[0].Health = v },
		"domain_name":       func(s *LiveSnapshot, v string) { s.DomainBacklogs[0].Domain = v },
		"stage_name":        func(s *LiveSnapshot, v string) { s.StageSummaries[0].Stage = v },
		"collector_kind":    func(s *LiveSnapshot, v string) { s.Collectors[0].CollectorKind = v },
		"health_state_word": func(s *LiveSnapshot, v string) { s.HealthState = v },
	}

	for _, canary := range forbidden {
		for carrierName, inject := range carriers {
			t.Run(string(canary.Class)+"/"+carrierName, func(t *testing.T) {
				snapshot := realisticLiveSnapshot()
				inject(&snapshot, canary.Raw)
				bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
					ScopeID:   "live:local",
					CreatedAt: fixedLiveCreatedAt,
				})

				// The contract is that a bundle carrying a sensitive value never
				// reaches a caller: either Validate refuses it, or the value is
				// gone from the rendered bytes. Anything else is a leak.
				validateErr := Validate(bundle)
				raw, renderErr := RenderJSON(bundle)
				if renderErr != nil {
					t.Fatalf("RenderJSON() error = %v", renderErr)
				}
				canaryErr := registry.AssertNoForbiddenCanary(redact.SurfaceOnboardingArtifacts, raw)

				if validateErr == nil && canaryErr == nil {
					t.Fatalf("injected %s canary %q via %s survived into the rendered bundle and Validate accepted it\nbundle: %s",
						canary.Class, canary.Raw, carrierName, raw)
				}
				// Record which mechanism caught it. If a case is ever caught by
				// neither Validate nor the registry, the guard above fires; if
				// it is caught only because the value never reached the output,
				// this log makes that visible rather than silently comfortable.
				t.Logf("caught by: validate=%v registry=%v", validateErr != nil, canaryErr != nil)
			})
		}
	}
}

// TestValidateRejectsBareHostPortEndpoints covers the shape Go's own network
// errors produce. The scheme-anchored privateEndpointPattern only matches a
// full URL, so "127.0.0.1:5432" in an error string slipped through until
// privateHostPortPattern was added.
func TestValidateRejectsBareHostPortEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"127.0.0.1:5432",
		"localhost:5432",
		"10.0.5.3:5432",
		"192.168.1.9:8080",
		"172.20.0.4:7687",
		"internal-llm.svc.cluster.local:8080",
	} {
		t.Run(endpoint, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{"dial tcp " + endpoint + ": connection refused"}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
				ScopeID:   "live:local",
				CreatedAt: fixedLiveCreatedAt,
			})
			if err := Validate(bundle); err == nil {
				t.Fatalf("Validate accepted a bundle carrying private endpoint %q", endpoint)
			}
		})
	}
}

// TestValidateKeepsOrdinaryDottedTextUsable guards the pattern above from
// firing on version-like or count-like text, which would make the live bundle
// unusable for honest content.
func TestValidateKeepsOrdinaryDottedTextUsable(t *testing.T) {
	snapshot := realisticLiveSnapshot()
	snapshot.HealthReasons = []string{
		"backend nornicdb v1.1.11 reported 10.4 percent retry rate",
		"stage parse pending 1024",
	}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
		ScopeID:   "live:local",
		CreatedAt: fixedLiveCreatedAt,
	})
	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate rejected ordinary dotted text: %v", err)
	}
}
