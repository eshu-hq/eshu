// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Go module paths are package identity in reachability matching, so they fold
// ASCII letters only (#9584). A Unicode fold lowered U+0130 to i and U+212A to
// k, so a lookalike module merged with the real one: it inherited the real
// module's advisory, or the real module read the lookalike's evidence.
const (
	goRealModule      = "github.com/serilog/sink"
	goLookalikeModule = "github.com/ser\u0130log/sink"
	goKelvinModule    = "github.com/\u212Aafka/client"
	goRealKafkaModule = "github.com/kafka/client"
)

func TestGoReachabilityKeepsLookalikeModulesApart(t *testing.T) {
	t.Parallel()

	safeRange := []map[string]any{{
		"type":   "SEMVER",
		"events": []map[string]any{{"introduced": "0.0.0"}, {"fixed": "0.0.1"}},
	}}
	for _, tc := range []struct {
		name      string
		envelopes []facts.Envelope
		want      int
	}{
		{
			name: "lookalike module does not inherit the real advisory",
			envelopes: []facts.Envelope{
				goModuleEvidenceEnvelope(t, "repo-1", "go.mod", goLookalikeModule, "v1.0.0", false, "", ""),
				goAffectedPackageEnvelope(t, "GO-2026-0001", goRealModule),
			},
			want: 0,
		},
		{
			name: "real module does not inherit a lookalike advisory",
			envelopes: []facts.Envelope{
				goModuleEvidenceEnvelope(t, "repo-1", "go.mod", goRealKafkaModule, "v1.0.0", false, "", ""),
				goAffectedPackageEnvelope(t, "GO-2026-0002", goKelvinModule),
			},
			want: 0,
		},
		{
			name: "lookalike module reads its own advisory rows, not the real module's range",
			envelopes: []facts.Envelope{
				goModuleEvidenceEnvelope(t, "repo-1", "go.mod", goLookalikeModule, "v1.0.0", false, "", ""),
				goAffectedPackageEnvelope(t, "GO-2026-0003", goLookalikeModule),
				goAffectedPackageEnvelopeWithRange(t, "GO-2026-0003", goRealModule, safeRange, nil),
			},
			want: 1,
		},
		{
			name: "ASCII case variants still match",
			envelopes: []facts.Envelope{
				goModuleEvidenceEnvelope(t, "repo-1", "go.mod", "GitHub.com/Serilog/Sink", "v1.0.0", false, "", ""),
				goAffectedPackageEnvelope(t, "GO-2026-0004", goRealModule),
			},
			want: 1,
		},
	} {
		if got := len(ClassifyGoVulnerabilityReachability(tc.envelopes)); got != tc.want {
			t.Errorf("%s: findings = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestGoReachabilityHelpersKeepLookalikeModulesApart(t *testing.T) {
	t.Parallel()

	groups := groupGoModuleEvidence([]goModuleEvidenceRow{
		{repositoryID: "repo-1", modulePath: goRealModule},
		{repositoryID: "repo-1", modulePath: goLookalikeModule},
		{repositoryID: "repo-1", modulePath: "GitHub.com/Serilog/Sink"},
	})
	if len(groups) != 2 {
		t.Fatalf("groupGoModuleEvidence groups = %d, want 2 (real with its ASCII case variant, lookalike alone)", len(groups))
	}

	advisoryRows := indexGoAdvisoryRowsByModuleOSV([]goAffectedPackageRow{
		{osvID: "GO-1", modulePath: goRealModule},
		{osvID: "GO-1", modulePath: goLookalikeModule},
	})
	if got := len(advisoryRows[goRealModule+"\x00GO-1"]); got != 1 {
		t.Fatalf("indexGoAdvisoryRowsByModuleOSV real-module rows = %d, want 1", got)
	}

	byModule := groupGoAdvisoryByModule([]goAffectedPackageRow{
		{osvID: "GO-1", modulePath: goRealKafkaModule},
		{osvID: "GO-2", modulePath: goKelvinModule},
	})
	if got := byModule[goRealKafkaModule]; len(got) != 1 || got[0] != "GO-1" {
		t.Fatalf("groupGoAdvisoryByModule real-module ids = %q, want [GO-1]", got)
	}

	reach := []goReachabilityRow{{deepestModule: goLookalikeModule}, {deepestModule: goRealKafkaModule}}
	if got := goReachabilityRowsForModule(reach, goRealModule); len(got) != 0 {
		t.Fatalf("goReachabilityRowsForModule(real) = %d rows, want 0 (only a lookalike was reached)", len(got))
	}
	if got := goReachabilityRowsForModule(reach, goKelvinModule); len(got) != 0 {
		t.Fatalf("goReachabilityRowsForModule(lookalike) = %d rows, want 0 (only the real module was reached)", len(got))
	}
	if got := goReachabilityRowsForModule(reach, "GITHUB.COM/KAFKA/CLIENT"); len(got) != 1 {
		t.Fatalf("goReachabilityRowsForModule(ASCII case variant) = %d rows, want 1", len(got))
	}

	if goSupplyChainReachabilityKey("GO-1", goLookalikeModule, "repo-1") == goSupplyChainReachabilityKey("GO-1", goRealModule, "repo-1") {
		t.Fatal("goSupplyChainReachabilityKey gives a lookalike module the real module's key")
	}
	if goSupplyChainReachabilityKey("GO-1", "GitHub.com/Serilog/Sink", "repo-1") != goSupplyChainReachabilityKey("GO-1", goRealModule, "repo-1") {
		t.Fatal("goSupplyChainReachabilityKey splits ASCII case variants")
	}
}
