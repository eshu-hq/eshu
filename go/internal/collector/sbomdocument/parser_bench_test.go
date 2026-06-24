// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomdocument

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkCycloneDXFixtureEnvelopes(b *testing.B) {
	raw, err := os.ReadFile(filepath.Clean("testdata/cyclonedx_image_subject.json"))
	if err != nil {
		b.Fatalf("read fixture: %v", err)
	}
	ctx := FixtureContext{
		ScopeID:             "sbom://bench",
		GenerationID:        "gen-1",
		CollectorInstanceID: "fixture-bench",
		FencingToken:        1,
		ObservedAt:          time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := CycloneDXFixtureEnvelopes(raw, ctx); err != nil {
			b.Fatalf("parser error: %v", err)
		}
	}
}

func BenchmarkSPDXFixtureEnvelopes(b *testing.B) {
	raw, err := os.ReadFile(filepath.Clean("testdata/spdx_image_subject.json"))
	if err != nil {
		b.Fatalf("read fixture: %v", err)
	}
	ctx := FixtureContext{
		ScopeID:             "sbom://bench",
		GenerationID:        "gen-1",
		CollectorInstanceID: "fixture-bench",
		FencingToken:        1,
		ObservedAt:          time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := SPDXFixtureEnvelopes(raw, ctx); err != nil {
			b.Fatalf("parser error: %v", err)
		}
	}
}
