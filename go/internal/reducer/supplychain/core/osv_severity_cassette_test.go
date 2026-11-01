// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// goldenOrdersGoAliasScopeID is the cassette scope that carries the
// CVE-2026-9229 / GO-2026-9229 OSV chain the B-7 snapshot queries.
const goldenOrdersGoAliasScopeID = "vulnerability_intelligence:orders-api:go-alias"

// TestGoldenCassetteOSVSeverityReachesImpactFinding is the Docker-free proof
// for the severity assertions the B-7 snapshot pins on CVE-2026-9229 (#9692).
// It replays the committed OSV cassette scope through the reducer, adds the
// package-consumption fact the live corpus gets from the orders-api go.mod
// fixture, and checks that the cvss_vector and severity_label the OSV
// collector now emits become a derived score, provenance, and a CVSS reason
// code on the finding payload the API serves. The snapshot's required values
// must stay equal to the values asserted here.
func TestGoldenCassetteOSVSeverityReachesImpactFinding(t *testing.T) {
	t.Parallel()

	envelopes := loadGoldenCassetteScopeEnvelopes(t, goldenOrdersGoAliasScopeID)
	envelopes = append(envelopes, packageConsumptionFactWithRange(
		"orders-api-lib-common-consumption",
		"gomod://proxy.golang.org/github.com/acme/lib-common",
		"repository:r_ea78e8bb",
		"v1.0.0",
	))

	var found *SupplyChainImpactFinding
	findings := BuildSupplyChainImpactFindings(envelopes)
	for i := range findings {
		if findings[i].CVEID == "CVE-2026-9229" {
			found = &findings[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no finding for CVE-2026-9229 among %d findings: %#v", len(findings), findings)
	}

	payload := supplyChainImpactPayload(SupplyChainImpactWrite{}, *found)
	if got := payload["cvss_score"]; got != 7.5 {
		t.Errorf("cvss_score = %#v, want 7.5 derived from the vector", got)
	}
	provenance, ok := payload["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("provenance = %#v, want an object", payload["provenance"])
	}
	for key, want := range map[string]any{
		"selected_severity_source":       "osv-go",
		"selected_severity_score":        7.5,
		"selected_severity_score_source": "derived_from_vector",
		"selected_severity_vector":       "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
		"selected_severity_label":        "high",
	} {
		if provenance[key] != want {
			t.Errorf("provenance[%q] = %#v, want %#v", key, provenance[key], want)
		}
	}
	if !containsString(found.PriorityReasonCodes, "cvss_v3_high") {
		t.Errorf("PriorityReasonCodes = %#v, want cvss_v3_high", found.PriorityReasonCodes)
	}
}

// loadGoldenCassetteScopeEnvelopes reads the committed vulnerability
// intelligence cassette and returns the envelopes of one scope. It decodes
// the JSON with a local struct because the cassette package imports the
// collector, which imports the reducer, which imports this package.
func loadGoldenCassetteScopeEnvelopes(t *testing.T, scopeID string) []facts.Envelope {
	t.Helper()

	path := filepath.Join("..", "..", "..", "..", "..", "testdata", "cassettes",
		"vulnerabilityintelligence", "supply-chain-demo.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vulnerability intelligence cassette: %v", err)
	}
	var file struct {
		Scopes []struct {
			ScopeID      string    `json:"scope_id"`
			GenerationID string    `json:"generation_id"`
			ObservedAt   time.Time `json:"observed_at"`
			Facts        []struct {
				FactKind      string         `json:"fact_kind"`
				StableFactKey string         `json:"stable_fact_key"`
				Payload       map[string]any `json:"payload"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse cassette: %v", err)
	}
	for _, scope := range file.Scopes {
		if scope.ScopeID != scopeID {
			continue
		}
		envelopes := make([]facts.Envelope, 0, len(scope.Facts))
		for _, fact := range scope.Facts {
			envelopes = append(envelopes, facts.Envelope{
				FactID:        fact.StableFactKey,
				ScopeID:       scope.ScopeID,
				GenerationID:  scope.GenerationID,
				FactKind:      fact.FactKind,
				StableFactKey: fact.StableFactKey,
				ObservedAt:    scope.ObservedAt,
				Payload:       fact.Payload,
			})
		}
		return envelopes
	}
	t.Fatalf("cassette has no scope %q", scopeID)
	return nil
}
