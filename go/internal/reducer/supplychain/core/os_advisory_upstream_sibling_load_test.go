// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestOSAdvisoryPassIndexesSiblingCVEFactWithUpstream is the loader-level
// guard for the OSV distro-record CVE grouping (#9914). A DSA, DLA, USN or
// Alpine record names its CVEs only through the `upstream` list on the
// record's own vulnerability.cve fact; the vulnerability.affected_package
// facts carry the distro advisory id and nothing else. A grouping pre-pass in
// the index build can therefore only re-key an affected package if the pass
// loads and indexes the sibling cve fact, payload included, for every
// affected_package it loads. This test drives the real
// loadSupplyChainImpactEvidence path and the real index build, with the
// converter's own envelopes, and proves that sibling arrives in both the
// loaded batch and the index, in either envelope order.
func TestOSAdvisoryPassIndexesSiblingCVEFactWithUpstream(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		fixture  string
		recordID string
		upstream []string
	}{
		{
			name:     "debian security bulletin with a bundled CVE and its DEBIAN-CVE twin",
			fixture:  "debian-cve-2023-38039.json",
			recordID: "DSA-5651-1",
			upstream: []string{"CVE-2023-38039", "DEBIAN-CVE-2023-38039"},
		},
		{
			name:     "ubuntu security notice naming two CVEs",
			fixture:  "ubuntu-cve-2022-27775.json",
			recordID: "USN-6766-1",
			upstream: []string{"CVE-2022-27775", "CVE-2022-27774", "UBUNTU-CVE-2022-27775"},
		},
		{
			name:     "alpine record re-keyed to its CVE",
			fixture:  "alpine-cve-2023-27537.json",
			recordID: "ALPINE-CVE-2023-27537",
			upstream: []string{"CVE-2023-27537"},
		},
	}

	for _, tc := range cases {
		for _, reverse := range []bool{false, true} {
			name := tc.name
			if reverse {
				name += " (affected entries reversed)"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				record := osvFixtureRecord(t, tc.fixture)
				record.ID = tc.recordID
				record.Aliases = nil
				record.Upstream = tc.upstream
				envelopes := convertedOSVRecord(t, record, reverse)
				scopeID, generationID := envelopes[0].ScopeID, envelopes[0].GenerationID

				loader := &scanScopedSupplyChainImpactFactLoader{
					factsByScope: map[string][]facts.Envelope{
						scanScopedFactLoaderKey(scopeID, generationID): envelopes,
					},
				}
				handler := SupplyChainImpactHandler{FactLoader: loader}
				loaded, _, err := handler.loadSupplyChainImpactEvidence(context.Background(), reducercontract.Intent{
					IntentID:     "intent-upstream-sibling",
					ScopeID:      scopeID,
					GenerationID: generationID,
					SourceSystem: "vulnerability_intelligence",
					Domain:       reducercontract.DomainSupplyChainImpact,
				})
				if err != nil {
					t.Fatalf("loadSupplyChainImpactEvidence() error = %v, want nil", err)
				}

				var cveEnvelopes []facts.Envelope
				affectedLoaded := 0
				for _, envelope := range loaded.envelopes {
					switch envelope.FactKind {
					case chain.VulnerabilityCVEFactKind:
						cveEnvelopes = append(cveEnvelopes, envelope)
					case chain.VulnerabilityAffectedPackageFactKind:
						affectedLoaded++
					}
				}
				if affectedLoaded == 0 {
					t.Fatalf("loaded no affected_package fact from %s; fixture is not exercising the pass", tc.fixture)
				}
				if len(cveEnvelopes) != 1 {
					t.Fatalf("loaded %d vulnerability.cve facts beside %d affected_package facts, want the 1 sibling", len(cveEnvelopes), affectedLoaded)
				}
				gotUpstream, _ := cveEnvelopes[0].Payload["upstream"].([]any)
				got := make([]string, 0, len(gotUpstream))
				for _, entry := range gotUpstream {
					text, _ := entry.(string)
					got = append(got, text)
				}
				// The converter writes upstream sorted and de-duplicated.
				want := slices.Sorted(slices.Values(tc.upstream))
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("loaded cve payload upstream = %v, want %v", got, want)
				}

				index, _, err := buildSupplyChainImpactIndexWithQuarantine(loaded.envelopes)
				if err != nil {
					t.Fatalf("buildSupplyChainImpactIndexWithQuarantine() error = %v, want nil", err)
				}
				if len(index.cves) != 1 {
					t.Fatalf("index.cves = %d entries, want the sibling cve fact indexed", len(index.cves))
				}
				cveID := index.cves[0].CVEID
				if len(index.affectedPackages[cveID]) != affectedLoaded {
					t.Fatalf("index.affectedPackages[%q] = %d, want all %d loaded affected packages under the sibling's cve_id",
						cveID, len(index.affectedPackages[cveID]), affectedLoaded)
				}
			})
		}
	}
}
