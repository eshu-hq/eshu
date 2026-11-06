// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"slices"
	"strings"
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
		key      string
		fixture  string
		recordID string
		upstream []string
	}{
		{
			name:     "debian security bulletin with a bundled CVE and its DEBIAN-CVE twin",
			key:      "built-sibling-dsa",
			fixture:  "file-debian-cve-2023-38039",
			recordID: "DSA-5651-1",
			upstream: []string{"CVE-2023-38039", "DEBIAN-CVE-2023-38039"},
		},
		{
			name:     "ubuntu security notice naming two CVEs",
			key:      "built-sibling-usn",
			fixture:  "file-ubuntu-cve-2022-27775",
			recordID: "USN-6766-1",
			upstream: []string{"CVE-2022-27775", "CVE-2022-27774", "UBUNTU-CVE-2022-27775"},
		},
		{
			name:     "alpine record re-keyed to its CVE",
			key:      "built-sibling-alpine",
			fixture:  "file-alpine-cve-2023-27537",
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

				envelopes := convertedOSVFixture(t, tc.key, reverse)
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
				// The grouping pass (#9914) files the sibling under every CVE
				// its upstream list names: one cve observation and the full
				// set of loaded affected packages per CVE key.
				var wantKeys []string
				for _, entry := range tc.upstream {
					if strings.HasPrefix(entry, "CVE-") {
						wantKeys = append(wantKeys, entry)
					}
				}
				slices.Sort(wantKeys)
				var gotKeys []string
				for _, cve := range index.cves {
					gotKeys = append(gotKeys, cve.CVEID)
				}
				if !slices.Equal(gotKeys, wantKeys) {
					t.Fatalf("index.cves keys = %v, want the sibling indexed once per upstream CVE %v", gotKeys, wantKeys)
				}
				// The record's own id no longer holds a second, orphaned copy.
				if got := len(index.affectedPackages[tc.recordID]); got != 0 {
					t.Fatalf("index.affectedPackages[%q] = %d entries, want none once filed under the CVE keys", tc.recordID, got)
				}
				for _, cveID := range wantKeys {
					if len(index.affectedPackages[cveID]) != affectedLoaded {
						t.Fatalf("index.affectedPackages[%q] = %d, want all %d loaded affected packages under the CVE key",
							cveID, len(index.affectedPackages[cveID]), affectedLoaded)
					}
				}
			})
		}
	}
}
