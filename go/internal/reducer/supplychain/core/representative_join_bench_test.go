// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// repJoinBenchEnvelopes builds one pass of groups CVEs, each with three facts for
// one package (#9990). Every group has an installed row unless the shape is the
// language control, which has rows for other packages only.
//
//   - joined: the own fact wins source order and joins.
//   - reselected: another source wins source order, so the own fact is chosen.
//   - no-joinable: rows exist and every fact is from another source.
//   - language: an npm group, which no OS fact shape applies to.
func repJoinBenchEnvelopes(shape string, groups int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, groups*5)
	for i := range groups {
		cveID := fmt.Sprintf("CVE-2099-%d", 10000+i)
		name := fmt.Sprintf("pkg%d", i)
		packageID := "pkg:deb/ubuntu/" + name
		ecosystem := "ubuntu"
		switch shape {
		case "reselected", "no-joinable":
			ecosystem = "deb"
		case "language":
			packageID, ecosystem = "pkg:npm/"+name, "npm"
		}
		fact := func(factID, source string) facts.Envelope {
			return facts.Envelope{FactID: fmt.Sprintf("%s-%d", factID, i), FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
				"cve_id": cveID, "advisory_id": fmt.Sprintf("%s-%d", factID, i), "source": source,
				"package_id": packageID, "package_name": name, "ecosystem": ecosystem,
				"purl": packageID + "?distro=ubuntu-22.04", "affected_versions": []any{"1.0-3"},
			}}
		}
		envelopes = append(envelopes, vulnerabilityCVEFact(fmt.Sprintf("cve-%d", i), cveID, 7.5), fact("nvd", "nvd"), fact("glad", "glad"))
		if shape != "no-joinable" {
			envelopes = append(envelopes, fact("own", "ubuntu"))
		}
		row := ubuntuCurl("22.04", "1.0-3")
		row.name = name
		envelope := row.envelope()
		envelope.FactID = fmt.Sprintf("os-%d", i)
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

// BenchmarkOSRepresentativeSelection measures the touched join per shape. The
// joined, no-joinable and language shapes run the same classification on the base
// and head, so they carry the no-regression comparison. The reselected shape
// yields no finding on the base, so its head figure is new work, not a slowdown.
func BenchmarkOSRepresentativeSelection(b *testing.B) {
	for _, shape := range []string{"joined", "reselected", "no-joinable", "language"} {
		b.Run(shape, func(b *testing.B) {
			envelopes := repJoinBenchEnvelopes(shape, 500)
			b.ReportAllocs()
			for b.Loop() {
				_ = BuildSupplyChainImpactFindings(envelopes)
			}
		})
	}
}
