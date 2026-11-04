// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// nonHostFact is an OS affected_package fact whose source is not the installed
// row's vendor and not OSV, so the join never line-checks it as the host's own
// vendor fact (#9945). It carries fixes and optionally one ECOSYSTEM range.
func nonHostFact(source string, fixes []string, ranges ...[2]string) facts.Envelope {
	payload := map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "EXT-9945-1", "source": source,
		"package_id": "pkg:deb/debian/curl", "ecosystem": "deb", "package_name": "curl",
	}
	if len(fixes) > 0 {
		values := make([]any, 0, len(fixes))
		for _, fix := range fixes {
			values = append(values, fix)
		}
		payload["fixed_versions"] = values
	}
	if len(ranges) > 0 {
		values := make([]any, 0, len(ranges))
		for _, r := range ranges {
			values = append(values, map[string]any{"type": "ECOSYSTEM", "events": []any{
				map[string]any{"introduced": r[0]}, map[string]any{"fixed": r[1]},
			}})
		}
		payload["affected_ranges"] = values
	}
	return facts.Envelope{FactID: "affected-nonhost-9945", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: payload}
}

// TestNonHostSourceFixIsNeverKnownFixedBesideAVerifiedFact pins R2-1 (#9945).
// A Debian 12 host at 1:1.0-3 joins a verified own-line vendor fact whose list
// names another version, so the host's line is known. A fact from any other
// source (blank, nvd, ubuntu, glad, ghsa) that carries a fix below the host has
// no verified line either: its fix must not read not_affected_known_fixed, and
// neither the finding nor the remediation may offer it, in both fact orders.
func TestNonHostSourceFixIsNeverKnownFixedBesideAVerifiedFact(t *testing.T) {
	t.Parallel()
	s := vendorLineScenarios[0]
	own := s.affectedEnvelope(s.purl(s.matchLine))
	own.Payload["affected_versions"] = []any{"1:1.0-99"}
	for _, source := range []string{"", "nvd", "ubuntu", "glad", "ghsa"} {
		for name, other := range map[string]facts.Envelope{
			"scalar fix": nonHostFact(source, []string{"1:1.0-2"}),
			"range fix":  nonHostFact(source, []string{"1:1.0-2"}, [2]string{"0", "1:1.0-2"}),
		} {
			t.Run("source="+source+"/"+name, func(t *testing.T) {
				t.Parallel()
				cve := vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5)
				host := s.row(s.hostRelease).envelope()
				for _, order := range [][]facts.Envelope{{cve, own, other, host}, {host, other, own, cve}} {
					findings := BuildSupplyChainImpactFindings(slices.Clone(order))
					if len(findings) != 1 {
						t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
					}
					got := findings[0]
					if got.Status == SupplyChainImpactNotAffectedKnownFixed {
						t.Fatalf("verdict = %s/%s, want a non-host fix never to read known fixed", got.Status, got.MatchReason)
					}
					if got.Status != SupplyChainImpactPossiblyAffected {
						t.Fatalf("status = %s, want %s", got.Status, SupplyChainImpactPossiblyAffected)
					}
					if got.FixedVersion != "" || got.Remediation.FirstPatchedVersion != "" {
						t.Fatalf("fixed version %q, remediation %q, want both withheld", got.FixedVersion, got.Remediation.FirstPatchedVersion)
					}
				}
			})
		}
	}
}
