// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestEmitBenchmarkCoversAllCollectorKinds is the coverage guard for B-4. It
// fails if any scope.AllCollectorKinds() entry lacks either an emit benchmark
// case (emitBenchCases) or a documented exemption (emitBenchExemptions). The
// decision for #3797 was to cover all collector kinds, so the only acceptable
// gap is a kind with a non-empty exemption reason. The guard also rejects a
// kind that is both benchmarked and exempted, an unknown kind in either list,
// and an exemption with an empty reason.
func TestEmitBenchmarkCoversAllCollectorKinds(t *testing.T) {
	t.Parallel()

	known := make(map[scope.CollectorKind]struct{})
	for _, k := range scope.AllCollectorKinds() {
		known[k] = struct{}{}
	}

	covered := make(map[scope.CollectorKind]string)

	for _, c := range emitBenchCases() {
		if _, ok := known[c.Kind]; !ok {
			t.Errorf("emit benchmark case references unknown collector kind %q", c.Kind)
		}
		if prev, dup := covered[c.Kind]; dup {
			t.Errorf("collector kind %q covered twice (already by %s, again by benchmark)", c.Kind, prev)
		}
		covered[c.Kind] = "benchmark"
	}

	for _, e := range emitBenchExemptions() {
		if _, ok := known[e.Kind]; !ok {
			t.Errorf("emit benchmark exemption references unknown collector kind %q", e.Kind)
		}
		if e.Reason == "" {
			t.Errorf("collector kind %q exemption has an empty reason; exemptions must document why", e.Kind)
		}
		if prev, dup := covered[e.Kind]; dup {
			t.Errorf("collector kind %q covered twice (already by %s, again by exemption)", e.Kind, prev)
		}
		covered[e.Kind] = "exemption"
	}

	for _, k := range scope.AllCollectorKinds() {
		if _, ok := covered[k]; !ok {
			t.Errorf("collector kind %q has no emit benchmark case and no documented exemption", k)
		}
	}
}

// TestEmitBenchmarkCassettesLoad proves every benchmarked cassette parses and
// carries at least one fact, so the benchmark cannot silently measure an empty
// generation. It exercises the real cassette loader the benchmark uses.
func TestEmitBenchmarkCassettesLoad(t *testing.T) {
	t.Parallel()

	for _, c := range emitBenchCases() {
		c := c
		t.Run(string(c.Kind), func(t *testing.T) {
			t.Parallel()

			file, err := cassette.LoadFile(c.CassettePath)
			if err != nil {
				t.Fatalf("LoadFile(%q) error = %v, want nil", c.CassettePath, err)
			}
			factCount := 0
			for _, sc := range file.Scopes {
				factCount += len(sc.Facts)
			}
			if factCount == 0 {
				t.Fatalf("cassette %q for %q has zero facts; emit benchmark would measure nothing", c.CassettePath, c.Kind)
			}
		})
	}
}
