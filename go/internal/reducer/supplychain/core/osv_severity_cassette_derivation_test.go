// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
)

// TestGoldenCassetteEveryOSVCVEFactDerivesSeverity requires every OSV
// vulnerability.cve fact in the committed cassette to give the reducer a usable
// severity. TestGoldenCassetteOSVSeverityReachesImpactFinding replays only the
// CVE-2026-9229 scope end to end, and the collector's producer guard proves the
// vector and label are producer-exact but not that the reducer can score them.
// A prefix-valid but unsupported vector on another OSV fact, such as
// CVSS:3.1/NOT-A-VECTOR, would pass both and leave that finding with no derived
// score, only the bucket its label sets. This guard decodes each fact through
// the reducer's typed seam and runs the same DeriveSeverity call the finding
// builder runs, so the failure names the advisory.
func TestGoldenCassetteEveryOSVCVEFactDerivesSeverity(t *testing.T) {
	t.Parallel()

	checked := 0
	for _, scopeID := range goldenCassetteScopeIDs(t) {
		for _, envelope := range loadGoldenCassetteScopeEnvelopes(t, scopeID) {
			if envelope.FactKind != chain.VulnerabilityCVEFactKind || envelope.Payload["source"] != "osv" {
				continue
			}
			checked++
			cve, err := supplyChainCVEFromEnvelope(envelope)
			if err != nil {
				t.Errorf("%s %s: decode vulnerability.cve: %v", scopeID, envelope.StableFactKey, err)
				continue
			}
			severity := cvss.DeriveSeverity(cve.CVSSScore, cve.CVSSVector, cve.SeverityLabel)
			if severity.Score <= 0 || severity.ScoreSource != cvss.ScoreSourceDerivedFromVector || severity.Bucket == cvss.BucketNone {
				t.Errorf("%s: advisory %s (%s) carries vector %q and label %q but the reducer derives %+v; the cassette's OSV severity must be usable by cvss.DeriveSeverity",
					scopeID, cve.AdvisoryID, cve.CVEID, cve.CVSSVector, cve.SeverityLabel, severity)
			}
		}
	}
	if checked < 3 {
		t.Fatalf("checked %d OSV vulnerability.cve facts in the cassette, want at least 3; the guard would be vacuous", checked)
	}
}

// goldenCassetteScopeIDs lists every scope id in the committed cassette, so the
// guard follows the cassette instead of a hard-coded scope list.
func goldenCassetteScopeIDs(t *testing.T) []string {
	t.Helper()

	path := filepath.Join("..", "..", "..", "..", "..", "testdata", "cassettes",
		"vulnerabilityintelligence", "supply-chain-demo.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vulnerability intelligence cassette: %v", err)
	}
	var file struct {
		Scopes []struct {
			ScopeID string `json:"scope_id"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse cassette: %v", err)
	}
	ids := make([]string, 0, len(file.Scopes))
	for _, scope := range file.Scopes {
		ids = append(ids, scope.ScopeID)
	}
	return ids
}
