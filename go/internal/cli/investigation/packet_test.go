// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation_test

import (
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/investigation"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// failClient fails every call. BuildPacket tests inject their own fetches
// through Deps, so a real transport must never be reached.
type failClient struct{ t *testing.T }

func (c *failClient) GetEnvelope(string, any) error {
	c.t.Helper()
	c.t.Fatal("GetEnvelope called; the test injected its own fetch")
	return nil
}

func (c *failClient) PostEnvelope(string, any, any) error {
	c.t.Helper()
	c.t.Fatal("PostEnvelope called; the test injected its own fetch")
	return nil
}

func exactTruth() *query.TruthEnvelope {
	return &query.TruthEnvelope{
		Level:     query.TruthLevelExact,
		Basis:     query.TruthBasisAuthoritativeGraph,
		Profile:   query.ProfileLocalAuthoritative,
		Backend:   query.GraphBackendNornicDB,
		Freshness: query.TruthFreshness{State: query.FreshnessFresh},
	}
}

func supplyChainEnvelope() investigation.SupplyChainExplainEnvelope {
	return investigation.SupplyChainExplainEnvelope{
		Data: query.SupplyChainImpactExplanationResult{
			Outcome: "finding_explained",
			Input: query.SupplyChainImpactExplanationFilter{
				AdvisoryID: "GHSA-aaaa-bbbb-cccc",
				PackageID:  "pkg:golang/example.com/vuln",
			},
			Finding: &query.SupplyChainImpactFindingResult{
				FindingID:       "finding-1",
				AdvisoryID:      "GHSA-aaaa-bbbb-cccc",
				PackageName:     "example.com/vuln",
				ImpactStatus:    "affected",
				WorkloadIDs:     []string{"workload:checkout"},
				ServiceIDs:      []string{"service:checkout"},
				EvidenceFactIDs: []string{"fact-advisory"},
			},
			ImpactPath: []query.SupplyChainImpactPathHop{
				{Hop: "advisory", Status: "present", EvidenceFactIDs: []string{"fact-advisory"}},
				{Hop: "service", Status: "present"},
			},
			Evidence: []query.SupplyChainImpactEvidenceFactSummary{
				{FactID: "fact-advisory", FactKind: "vulnerability_advisory", SourceSystem: "osv"},
			},
			Freshness: query.SupplyChainImpactExplanationFreshness{
				State: "fresh", LatestObservedAt: "2026-06-18T00:00:00Z",
			},
		},
		Truth: exactTruth(),
	}
}

func supplyChainScope() map[string]string {
	return map[string]string{"advisory_id": "GHSA-aaaa-bbbb-cccc", "package_id": "pkg:golang/example.com/vuln"}
}

func TestBuildPacketUnknownFamilyRefusesWithoutFetching(t *testing.T) {
	t.Parallel()

	deps := investigation.Deps{
		FetchSupplyChainExplain: func(investigation.Client, query.SupplyChainImpactExplanationFilter) (investigation.SupplyChainExplainEnvelope, error) {
			t.Fatal("no fetch may run for an unrecognized family")
			return investigation.SupplyChainExplainEnvelope{}, nil
		},
	}
	packet, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
		Family:  query.InvestigationFamily("not_a_family"),
		Subject: map[string]string{"x": "y"},
	})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	if packet.Refusal != query.PacketRefusalUnknownFamily {
		t.Fatalf("refusal = %q, want unknown_family", packet.Refusal)
	}
	if packet.Identity.Family != query.InvestigationFamily("not_a_family") {
		t.Fatalf("family = %q, want the operator's raw family echoed back", packet.Identity.Family)
	}
}

func TestBuildPacketEmptyFamilyStillNamesTheScope(t *testing.T) {
	t.Parallel()

	packet, err := investigation.BuildPacket(&failClient{t: t}, investigation.Deps{}, investigation.Request{})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	if packet.Refusal != query.PacketRefusalUnknownFamily {
		t.Fatalf("refusal = %q, want unknown_family", packet.Refusal)
	}
	if packet.Identity.Subject["requested"] != "unspecified" {
		t.Fatalf("subject = %v, want the placeholder scope", packet.Identity.Subject)
	}
}

func TestBuildPacketSupplyChain(t *testing.T) {
	t.Parallel()

	t.Run("complete scope produces a supported packet", func(t *testing.T) {
		t.Parallel()

		var gotFilter query.SupplyChainImpactExplanationFilter
		deps := investigation.Deps{
			FetchSupplyChainExplain: func(_ investigation.Client, filter query.SupplyChainImpactExplanationFilter) (investigation.SupplyChainExplainEnvelope, error) {
				gotFilter = filter
				return supplyChainEnvelope(), nil
			},
		}
		packet, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
			Family:  query.InvestigationFamilySupplyChainImpact,
			Subject: supplyChainScope(),
		})
		if err != nil {
			t.Fatalf("BuildPacket: %v", err)
		}
		if gotFilter.AdvisoryID != "GHSA-aaaa-bbbb-cccc" || gotFilter.PackageID != "pkg:golang/example.com/vuln" {
			t.Fatalf("filter = %+v, want the subject mapped onto the filter", gotFilter)
		}
		if !packet.Answer.Supported {
			t.Fatal("expected a supported packet")
		}
		if packet.Schema != query.InvestigationEvidencePacketSchema {
			t.Fatalf("schema = %q", packet.Schema)
		}
	})

	t.Run("incomplete scope refuses without fetching", func(t *testing.T) {
		t.Parallel()

		deps := investigation.Deps{
			FetchSupplyChainExplain: func(investigation.Client, query.SupplyChainImpactExplanationFilter) (investigation.SupplyChainExplainEnvelope, error) {
				t.Fatal("an advisory with no target must refuse before any fetch")
				return investigation.SupplyChainExplainEnvelope{}, nil
			},
		}
		packet, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
			Family:  query.InvestigationFamilySupplyChainImpact,
			Subject: map[string]string{"advisory_id": "GHSA-x"},
		})
		if err != nil {
			t.Fatalf("BuildPacket: %v", err)
		}
		if packet.Refusal != query.PacketRefusalScopeNotFound {
			t.Fatalf("refusal = %q, want scope_not_found", packet.Refusal)
		}
	})

	t.Run("a classifiable transport error becomes a refusal packet", func(t *testing.T) {
		t.Parallel()

		deps := investigation.Deps{
			FetchSupplyChainExplain: func(investigation.Client, query.SupplyChainImpactExplanationFilter) (investigation.SupplyChainExplainEnvelope, error) {
				return investigation.SupplyChainExplainEnvelope{}, &statusError{code: 501}
			},
		}
		packet, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
			Family:  query.InvestigationFamilySupplyChainImpact,
			Subject: supplyChainScope(),
		})
		if err != nil {
			t.Fatalf("BuildPacket: %v", err)
		}
		if packet.Refusal != query.PacketRefusalProfileUnsupported {
			t.Fatalf("refusal = %q, want profile_unsupported", packet.Refusal)
		}
	})

	t.Run("an unclassifiable transport error surfaces to the operator", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("dial tcp 127.0.0.1:8080: connect: connection refused")
		deps := investigation.Deps{
			FetchSupplyChainExplain: func(investigation.Client, query.SupplyChainImpactExplanationFilter) (investigation.SupplyChainExplainEnvelope, error) {
				return investigation.SupplyChainExplainEnvelope{}, sentinel
			},
		}
		_, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
			Family:  query.InvestigationFamilySupplyChainImpact,
			Subject: supplyChainScope(),
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want the transport error returned unwrapped so its text reaches the operator", err)
		}
	})

	t.Run("an in-envelope error code becomes a refusal packet", func(t *testing.T) {
		t.Parallel()

		deps := investigation.Deps{
			FetchSupplyChainExplain: func(investigation.Client, query.SupplyChainImpactExplanationFilter) (investigation.SupplyChainExplainEnvelope, error) {
				return investigation.SupplyChainExplainEnvelope{
					Error: &query.ErrorEnvelope{Code: query.ErrorCodeNotFound, Message: "no finding"},
				}, nil
			},
		}
		packet, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
			Family:  query.InvestigationFamilySupplyChainImpact,
			Subject: supplyChainScope(),
		})
		if err != nil {
			t.Fatalf("BuildPacket: %v", err)
		}
		if packet.Refusal != query.PacketRefusalScopeNotFound {
			t.Fatalf("refusal = %q, want scope_not_found", packet.Refusal)
		}
	})

	t.Run("bounds cap the source-facts layer", func(t *testing.T) {
		t.Parallel()

		envelope := supplyChainEnvelope()
		envelope.Data.Evidence = []query.SupplyChainImpactEvidenceFactSummary{
			{FactID: "fact-1", FactKind: "k"},
			{FactID: "fact-2", FactKind: "k"},
			{FactID: "fact-3", FactKind: "k"},
		}
		envelope.Data.Finding.EvidenceFactIDs = nil
		deps := investigation.Deps{
			FetchSupplyChainExplain: func(investigation.Client, query.SupplyChainImpactExplanationFilter) (investigation.SupplyChainExplainEnvelope, error) {
				return envelope, nil
			},
		}
		packet, err := investigation.BuildPacket(&failClient{t: t}, deps, investigation.Request{
			Family:  query.InvestigationFamilySupplyChainImpact,
			Subject: supplyChainScope(),
			Bounds:  investigation.BoundsFromMaxSourceFacts(1),
		})
		if err != nil {
			t.Fatalf("BuildPacket: %v", err)
		}
		if len(packet.SourceFacts) != 1 {
			t.Fatalf("source facts = %d, want 1", len(packet.SourceFacts))
		}
		if !packet.Bounds.Truncated {
			t.Fatal("expected bounds.truncated when the cap bites")
		}
	})
}
