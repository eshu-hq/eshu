// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// splitBenchInput is a vendor fact with the given ecosystem and an OSV fact that cannot
// join, over rows installed hosts that each carry their own image digest. The first
// versionless hosts have no installed version.
func splitBenchInput(ownEcosystem string, rows, versionless int) []facts.Envelope {
	out := []facts.Envelope{
		vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5),
		repJoinOtherFact(repDirectOtherID, "osv", "ubuntu"),
		repJoinOwnFact(ownEcosystem),
	}
	for i := range rows {
		version := repJoinVersion
		if i < versionless {
			version = ""
		}
		row := ubuntuCurl("22.04", version).envelope()
		scope := fmt.Sprintf("s%d", i)
		row.FactID, row.ScopeID, row.GenerationID = "row-"+scope, "image://"+scope, "g-"+scope
		digest := fmt.Sprintf("sha256:%064d", i)
		out = append(out, row, scannerWorkerAnalysisFact("image://"+scope, "g-"+scope, digest, "registry.example/"+scope+"@"+digest))
	}
	return out
}

// BenchmarkSplitDirectBlankGroup measures the cost of the row split of ruling 9990-f6 on
// a fleet of 100 hosts. A spelled ecosystem, the only kind a producer writes, pays two
// comparisons and one trim; a blank group pays one more row walk; a blank group with
// mixed rows classifies the versioned rows on one fact instead of two.
func BenchmarkSplitDirectBlankGroup(b *testing.B) {
	for _, c := range []struct {
		name         string
		ecosystem    string
		rows, noVers int
	}{
		{"spelled-all-versioned", "ubuntu", 100, 0},
		{"spelled-one-versionless", "ubuntu", 100, 1},
		{"blank-all-versioned", "", 100, 0},
		{"blank-one-versionless", "", 100, 1},
		{"blank-all-versionless", "", 100, 100},
	} {
		input := splitBenchInput(c.ecosystem, c.rows, c.noVers)
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				BuildSupplyChainImpactFindings(input)
			}
		})
	}
}
