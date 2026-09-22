// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/cloud"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// fixtureBase anchors every deterministic timestamp used by maxRawSnapshot.
// Every fixture timestamp is fixtureBase plus a fixed, distinct offset so
// RenderJSON/RenderText output never depends on wall time or test order.
var fixtureBase = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

// fixtureTime returns a deterministic timestamp offset by n minutes from
// fixtureBase. Distinct n values keep every populated *_at field distinct.
func fixtureTime(n int) time.Time {
	return fixtureBase.Add(time.Duration(n) * time.Minute)
}

// fixtureDuration returns a deterministic, distinct, non-zero duration.
func fixtureDuration(n int) time.Duration {
	return time.Duration(n) * time.Second
}

// maxRawSnapshot returns a RawSnapshot with every exported field populated
// with a distinctive, deterministic, non-zero value: every slice has at
// least two elements, every pointer is non-nil, and every nested struct is
// fully populated. It is the fixture behind the #6775 pre-nest wire-output
// lock (render_wire_golden_test.go, render_wire_text_golden_test.go,
// render_wire_keys_test.go) so a moved json tag or dropped section fails
// loudly instead of shifting a value silently.
func maxRawSnapshot() RawSnapshot {
	return RawSnapshot{
		AsOf: fixtureTime(0),
		ScopeCounts: []NamedCount{
			{Name: "active", Count: 11},
			{Name: "pending", Count: 4},
		},
		GenerationCounts: []NamedCount{
			{Name: "active", Count: 9},
			{Name: "pending", Count: 3},
		},
		ScopeActivity: ScopeActivitySnapshot{
			Active:    11,
			Changed:   4,
			Unchanged: 7,
		},
		GenerationHistory: GenerationHistorySnapshot{
			Active:     9,
			Pending:    3,
			Completed:  15,
			Superseded: 2,
			Failed:     1,
			Other:      6,
		},
		GenerationTransitions: []GenerationTransitionSnapshot{
			{
				ScopeID:                   "scope-fixture-a",
				GenerationID:              "generation-fixture-a1",
				Status:                    "completed",
				TriggerKind:               "scheduled",
				FreshnessHint:             "fresh",
				ObservedAt:                fixtureTime(10),
				ActivatedAt:               fixtureTime(11),
				SupersededAt:              fixtureTime(12),
				CurrentActiveGenerationID: "generation-fixture-a2",
			},
			{
				ScopeID:                   "scope-fixture-b",
				GenerationID:              "generation-fixture-b1",
				Status:                    "pending",
				TriggerKind:               "webhook",
				FreshnessHint:             "stale",
				ObservedAt:                fixtureTime(13),
				ActivatedAt:               fixtureTime(14),
				SupersededAt:              fixtureTime(15),
				CurrentActiveGenerationID: "generation-fixture-b2",
			},
		},
		StageCounts: []StageStatusCount{
			{Stage: "projector", Status: "pending", Count: 21},
			{Stage: "projector", Status: "claimed", Count: 22},
			{Stage: "projector", Status: "running", Count: 23},
			{Stage: "projector", Status: "retrying", Count: 24},
			{Stage: "projector", Status: "succeeded", Count: 25},
			{Stage: "projector", Status: "failed", Count: 26},
			{Stage: "projector", Status: "dead_letter", Count: 27},
			{Stage: "reducer", Status: "pending", Count: 31},
			{Stage: "reducer", Status: "claimed", Count: 32},
			{Stage: "reducer", Status: "running", Count: 33},
			{Stage: "reducer", Status: "retrying", Count: 34},
			{Stage: "reducer", Status: "succeeded", Count: 35},
			{Stage: "reducer", Status: "failed", Count: 36},
			{Stage: "reducer", Status: "dead_letter", Count: 37},
		},
		DomainBacklogs: []DomainBacklog{
			{Domain: "domain-fixture-a", Outstanding: 61, InFlight: 6, Retrying: 5, Failed: 4, DeadLetter: 3, OldestAge: fixtureDuration(601)},
			{Domain: "domain-fixture-b", Outstanding: 55, InFlight: 5, Retrying: 4, Failed: 3, DeadLetter: 2, OldestAge: fixtureDuration(551)},
			{Domain: "domain-fixture-c", Outstanding: 49, InFlight: 4, Retrying: 3, Failed: 2, DeadLetter: 1, OldestAge: fixtureDuration(491)},
			{Domain: "domain-fixture-d", Outstanding: 43, InFlight: 3, Retrying: 2, Failed: 1, DeadLetter: 1, OldestAge: fixtureDuration(431)},
			{Domain: "domain-fixture-e", Outstanding: 37, InFlight: 2, Retrying: 1, Failed: 1, DeadLetter: 1, OldestAge: fixtureDuration(371)},
			{Domain: "domain-fixture-f", Outstanding: 31, InFlight: 1, Retrying: 1, Failed: 1, DeadLetter: 1, OldestAge: fixtureDuration(311)},
		},
		ProducerActivity: ProducerActivitySnapshot{
			HasActiveOrPendingGeneration: true,
			LatestGenerationAge:          fixtureDuration(180),
		},
		QueueBlockages: []QueueBlockage{
			{Stage: "projector", Domain: "domain-fixture-a", ConflictDomain: "conflict-domain-a", ConflictKey: "conflict-key-a", Blocked: 8, OldestAge: fixtureDuration(801)},
			{Stage: "reducer", Domain: "domain-fixture-b", ConflictDomain: "conflict-domain-b", ConflictKey: "conflict-key-b", Blocked: 5, OldestAge: fixtureDuration(501)},
		},
		RetryPolicies: []RetryPolicySummary{
			{Stage: "projector", MaxAttempts: 4, RetryDelay: fixtureDuration(45)},
			{Stage: "reducer", MaxAttempts: 6, RetryDelay: fixtureDuration(90)},
		},
		Queue: QueueSnapshot{
			Total:                                 200,
			Outstanding:                           45,
			Pending:                               20,
			InFlight:                              15,
			Retrying:                              6,
			Succeeded:                             120,
			Failed:                                8,
			DeadLetter:                            5,
			ProvenanceEdgeIdentityUpgradeApplied:  true,
			ProvenanceEdgeIdentityUpgradeRequired: 3,
			OldestOutstandingAge:                  fixtureDuration(900),
			OverdueClaims:                         2,
		},
		LatestQueueFailure: &QueueFailureSnapshot{
			Stage:          "reducer",
			Domain:         "domain-fixture-a",
			Status:         "failed",
			WorkItemID:     "work-item-fixture-1",
			ScopeID:        "scope-fixture-a",
			GenerationID:   "generation-fixture-a1",
			FailureClass:   "transient_backend_error",
			FailureMessage: "fixture failure message",
			FailureDetails: "fixture failure details",
			UpdatedAt:      fixtureTime(20),
		},
		Coordinator:        fixtureCoordinatorSnapshot(),
		RegistryCollectors: fixtureRegistryCollectors(),
		AWSCloudScans: []cloud.AWSScanStatus{
			{
				CollectorInstanceID: "aws-collector-1",
				AccountID:           "111111111111",
				Region:              "us-east-1",
				ServiceKind:         "s3",
				Status:              "succeeded",
				CommitStatus:        "committed",
				FailureClass:        "",
				FailureMessage:      "",
				APICallCount:        400,
				ThrottleCount:       2,
				WarningCount:        1,
				ResourceCount:       88,
				RelationshipCount:   44,
				TagObservationCount: 12,
				BudgetExhausted:     false,
				CredentialFailed:    false,
				LastStartedAt:       fixtureTime(30),
				LastObservedAt:      fixtureTime(31),
				LastCompletedAt:     fixtureTime(32),
				LastSuccessfulAt:    fixtureTime(33),
				UpdatedAt:           fixtureTime(34),
			},
			{
				CollectorInstanceID: "aws-collector-2",
				AccountID:           "222222222222",
				Region:              "us-west-2",
				ServiceKind:         "ec2",
				Status:              "failed",
				CommitStatus:        "not_committed",
				FailureClass:        "credential_error",
				FailureMessage:      "fixture credential failure",
				APICallCount:        150,
				ThrottleCount:       5,
				WarningCount:        3,
				ResourceCount:       20,
				RelationshipCount:   10,
				TagObservationCount: 4,
				BudgetExhausted:     true,
				CredentialFailed:    true,
				LastStartedAt:       fixtureTime(40),
				LastObservedAt:      fixtureTime(41),
				LastCompletedAt:     fixtureTime(42),
				LastSuccessfulAt:    fixtureTime(43),
				UpdatedAt:           fixtureTime(44),
			},
		},
		AWSFreshness: cloud.AWSFreshnessSnapshot{
			StatusCounts: []NamedCount{
				{Name: "queued", Count: 6},
				{Name: "claimed", Count: 3},
			},
			OldestQueuedAge: fixtureDuration(360),
		},
		InfraInventory: InfraInventorySnapshot{
			Reported:       true,
			State:          "fenced",
			MarkerPresent:  true,
			DirtyRepos:     17,
			OldestDirtyAge: fixtureDuration(720),
		},
		VulnerabilitySources: []collector.VulnerabilitySourceState{
			{
				CollectorInstanceID: "vuln-collector-1",
				ScopeID:             "scope-fixture-a",
				Source:              "osv",
				Ecosystem:           "go",
				WindowStart:         fixtureTime(50),
				WindowEnd:           fixtureTime(51),
				LastAttemptAt:       fixtureTime(52),
				LastSuccessAt:       fixtureTime(53),
				NextRetryAt:         fixtureTime(54),
				LastErrorClass:      "rate_limited",
				FreshnessState:      "fresh",
				TerminalStatus:      "succeeded",
				ResultCount:         14,
				WarningCount:        2,
				UpdatedAt:           fixtureTime(55),
			},
			{
				CollectorInstanceID: "vuln-collector-2",
				ScopeID:             "scope-fixture-b",
				Source:              "ghsa",
				Ecosystem:           "npm",
				WindowStart:         fixtureTime(56),
				WindowEnd:           fixtureTime(57),
				LastAttemptAt:       fixtureTime(58),
				LastSuccessAt:       fixtureTime(59),
				NextRetryAt:         fixtureTime(60),
				LastErrorClass:      "timeout",
				FreshnessState:      "stale",
				TerminalStatus:      "failed",
				ResultCount:         9,
				WarningCount:        1,
				UpdatedAt:           fixtureTime(61),
			},
		},
		SemanticExtraction: fixtureSemanticExtractionStatus(),
		AnswerNarration:    fixtureAnswerNarrationStatus(),
		CollectorGenerationDeadLetters: collector.GenerationDeadLetterSnapshot{
			DeadLetter:          4,
			ReplayRequested:     2,
			ReplayAttempts:      6,
			OldestDeadLetterAge: fixtureDuration(1200),
		},
		CollectorFactEvidence: []collector.FactEvidence{
			{
				InstanceID:       "documentation-collector-1",
				CollectorKind:    "documentation",
				EvidenceSource:   "source_facts",
				SourceSystems:    []string{"confluence", "notion"},
				ObservationCount: 33,
				LastObservedAt:   fixtureTime(70),
				UpdatedAt:        fixtureTime(71),
			},
			{
				InstanceID:       "fixture-uncataloged-instance",
				CollectorKind:    "fixture_uncataloged_kind",
				EvidenceSource:   "reducer_facts",
				SourceSystems:    []string{"fixture-source-a", "fixture-source-b"},
				ObservationCount: 12,
				LastObservedAt:   fixtureTime(72),
				UpdatedAt:        fixtureTime(73),
			},
		},
		AWSCloudScansTruncated: true,
		AWSCloudScanLimit:      2,
		TerraformStateLastSerials: []TerraformStateLocatorSerial{
			{
				SafeLocatorHash: "safe-locator-hash-a",
				BackendKind:     "s3",
				Lineage:         "lineage-fixture-a",
				Serial:          101,
				GenerationID:    "generation-fixture-a1",
				ObservedAt:      fixtureTime(80),
			},
			{
				SafeLocatorHash: "safe-locator-hash-b",
				BackendKind:     "gcs",
				Lineage:         "lineage-fixture-b",
				Serial:          202,
				GenerationID:    "generation-fixture-b1",
				ObservedAt:      fixtureTime(81),
			},
		},
		TerraformStateRecentWarnings: []TerraformStateLocatorWarning{
			{
				SafeLocatorHash: "safe-locator-hash-a",
				BackendKind:     "s3",
				WarningKind:     "drift_detected",
				Reason:          "resource_replaced",
				Severity:        "high",
				Actionability:   "actionable",
				Source:          "warning_fact",
				SourceHandle:    "source-handle-a",
				GenerationID:    "generation-fixture-a1",
				ObservedAt:      fixtureTime(90),
			},
			{
				SafeLocatorHash: "safe-locator-hash-b",
				BackendKind:     "gcs",
				WarningKind:     "stale_lock",
				Reason:          "lock_expired",
				Severity:        "medium",
				Actionability:   "informational",
				Source:          "warning_fact",
				SourceHandle:    "source-handle-b",
				GenerationID:    "generation-fixture-b1",
				ObservedAt:      fixtureTime(91),
			},
		},
	}
}
