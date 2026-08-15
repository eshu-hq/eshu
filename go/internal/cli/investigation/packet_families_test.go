// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/investigation"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestBuildPacketDeployableUnit(t *testing.T) {
	t.Parallel()

	t.Run("complete scope produces a supported packet", func(t *testing.T) {
		t.Parallel()

		var gotParams url.Values
		deps := investigation.Deps{
			FetchAdmissionDecisions: func(_ investigation.Client, params url.Values) (investigation.AdmissionDecisionsEnvelope, error) {
				gotParams = params
				env := investigation.AdmissionDecisionsEnvelope{Truth: exactTruth()}
				env.Data.Decisions = []query.AdmissionDecisionResult{{
					DecisionID: "d1", Domain: "deployable_unit_correlation", State: "admitted",
					ScopeID: "s1", GenerationID: "g1", AnchorKind: "repository", AnchorID: "repo-1",
					CandidateKind: "deployable_unit", CandidateID: "w1",
					CanonicalWrite: query.AdmissionDecisionCanonicalWrite{
						Written: true, TargetKind: "CORRELATES_DEPLOYABLE_UNIT", TargetID: "w1",
					},
				}}
				return env, nil
			},
		}
		packet, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
			Family: query.InvestigationFamilyDeployableUnit,
			Subject: map[string]string{
				"scope_id": "s1", "generation_id": "g1", "repository_id": "repo-1", "workload_id": "w1",
			},
		})
		if err != nil {
			t.Fatalf("BuildPacket: %v", err)
		}
		if gotParams.Get("domain") != "deployable_unit_correlation" ||
			gotParams.Get("anchor_kind") != "repository" || gotParams.Get("anchor_id") != "repo-1" {
			t.Fatalf("params = %v", gotParams)
		}
		if !packet.Answer.Supported {
			t.Fatal("expected a supported packet")
		}
	})

	t.Run("missing scope or generation refuses without fetching", func(t *testing.T) {
		t.Parallel()

		deps := investigation.Deps{
			FetchAdmissionDecisions: func(investigation.Client, url.Values) (investigation.AdmissionDecisionsEnvelope, error) {
				t.Fatal("fetch must not run without scope_id and generation_id")
				return investigation.AdmissionDecisionsEnvelope{}, nil
			},
		}
		for _, subject := range []map[string]string{
			{"workload_id": "w1", "generation_id": "g1"},
			{"scope_id": "s1", "workload_id": "w1"},
		} {
			packet, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
				Family:  query.InvestigationFamilyDeployableUnit,
				Subject: subject,
			})
			if err != nil {
				t.Fatalf("BuildPacket(%v): %v", subject, err)
			}
			if packet.Refusal != query.PacketRefusalScopeNotFound {
				t.Fatalf("refusal = %q for %v, want scope_not_found", packet.Refusal, subject)
			}
		}
	})

	t.Run("a classifiable transport error becomes a refusal packet", func(t *testing.T) {
		t.Parallel()

		deps := investigation.Deps{
			FetchAdmissionDecisions: func(investigation.Client, url.Values) (investigation.AdmissionDecisionsEnvelope, error) {
				return investigation.AdmissionDecisionsEnvelope{}, &statusError{code: 503}
			},
		}
		packet, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
			Family:  query.InvestigationFamilyDeployableUnit,
			Subject: map[string]string{"scope_id": "s1", "generation_id": "g1"},
		})
		if err != nil {
			t.Fatalf("BuildPacket: %v", err)
		}
		if packet.Refusal != query.PacketRefusalBackendUnavailable {
			t.Fatalf("refusal = %q, want backend_unavailable", packet.Refusal)
		}
	})
}

func TestBuildPacketDrift(t *testing.T) {
	t.Parallel()

	t.Run("complete scope produces a supported packet", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		deps := investigation.Deps{
			FetchDriftFindings: func(_ investigation.Client, body map[string]any) (investigation.DriftFindingsEnvelope, error) {
				gotBody = body
				env := investigation.DriftFindingsEnvelope{Truth: &query.TruthEnvelope{
					Level:     query.TruthLevelExact,
					Basis:     query.TruthBasisRuntimeState,
					Freshness: query.TruthFreshness{State: query.FreshnessFresh},
				}}
				env.Data.DriftFindings = []query.CloudRuntimeDriftFindingView{{
					FactID: "f1", Provider: "aws", ScopeID: "acct1",
					CloudResourceUID: "aws:s3:b", FindingKind: "orphaned_cloud_resource",
					MatchedTerraformStateAddress: "aws_s3_bucket.b",
				}}
				return env, nil
			},
		}
		packet, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
			Family:  query.InvestigationFamilyDrift,
			Subject: map[string]string{"scope_id": "acct1", "provider": "aws"},
		})
		if err != nil {
			t.Fatalf("BuildPacket: %v", err)
		}
		if gotBody["scope_id"] != "acct1" || gotBody["provider"] != "aws" {
			t.Fatalf("body = %v", gotBody)
		}
		if packet.Identity.Family != query.InvestigationFamilyDrift {
			t.Fatalf("family = %q", packet.Identity.Family)
		}
	})

	t.Run("missing scope refuses without fetching", func(t *testing.T) {
		t.Parallel()

		deps := investigation.Deps{
			FetchDriftFindings: func(investigation.Client, map[string]any) (investigation.DriftFindingsEnvelope, error) {
				t.Fatal("fetch must not run without a scope")
				return investigation.DriftFindingsEnvelope{}, nil
			},
		}
		packet, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
			Family:  query.InvestigationFamilyDrift,
			Subject: map[string]string{"provider": "aws"},
		})
		if err != nil {
			t.Fatalf("BuildPacket: %v", err)
		}
		if packet.Refusal != query.PacketRefusalScopeNotFound {
			t.Fatalf("refusal = %q, want scope_not_found", packet.Refusal)
		}
	})

	t.Run("an unmapped envelope code surfaces as a CLI error", func(t *testing.T) {
		t.Parallel()

		deps := investigation.Deps{
			FetchDriftFindings: func(investigation.Client, map[string]any) (investigation.DriftFindingsEnvelope, error) {
				return investigation.DriftFindingsEnvelope{
					Error: &query.ErrorEnvelope{Code: query.ErrorCodeInvalidArgument, Message: "bad provider"},
				}, nil
			},
		}
		_, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
			Family:  query.InvestigationFamilyDrift,
			Subject: map[string]string{"scope_id": "acct1"},
		})
		if err == nil {
			t.Fatal("expected a CLI error")
		}
		if !strings.Contains(err.Error(), "read failed: invalid_argument: bad provider") {
			t.Fatalf("err = %v", err)
		}
	})
}
