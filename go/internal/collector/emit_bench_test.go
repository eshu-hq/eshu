// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// drainCommitter is a hermetic collector.Committer that counts the facts it
// drains. It performs no Postgres, network, or disk I/O, so the emit benchmark
// measures only the collector Claim -> ingest -> emit-facts path, not durable
// commit cost.
type drainCommitter struct {
	facts int
}

// CommitScopeGeneration drains the fact stream and counts envelopes, modeling
// the durable commit seam without any external dependency.
func (d *drainCommitter) CommitScopeGeneration(
	_ context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	for range factStream {
		d.facts++
	}
	return nil
}

// BenchmarkEmit measures the credential-free Claim -> ingest -> emit-facts
// micro-benchmark for every collector kind. One b.Run subtest per kind replays
// that kind's cassette through the real collector.Service against an in-memory
// drain committer, reporting ns/op, allocs/op (b.ReportAllocs), and the number
// of facts emitted per full drain (b.ReportMetric).
//
// Cassette parsing happens once before b.ResetTimer; each iteration constructs a
// fresh in-memory cassette.Source over the already-parsed file and drives the
// service until the batch is exhausted, so the timed region is the collector
// emit path and not file I/O.
//
// Scope of the measurement. This is the uniform, cross-collector Claim ->
// ingest -> emit-facts SERVICE-path benchmark: it times collector.Service
// draining a recorded fact-envelope generation (Source.Next -> committer fact
// stream), i.e. the lifecycle, fact-stream channel, and per-fact drain cost that
// every collector shares. It deliberately does NOT exercise a collector's
// provider-response -> facts.Envelope builder, because the provider-API
// collectors (aws, azure, gcp, jira, pagerduty, oci_registry, ...) cannot run
// their real emitters credential-free: that path needs recorded provider HTTP
// responses replayed through the real source, which is the deterministic-replay
// framework tracked in #4102 (a recorded-transport tape, distinct from these
// fact-level cassettes). Until that lands, per-collector fact-builder cost is
// measured by the dedicated parse/build benchmarks where the input is already
// credential-free and local — BenchmarkParseStream_LargeState
// (collector/terraformstate), the SBOM document parser bench
// (collector/sbomdocument), and the git snapshot benches
// (git_snapshot_delta_bench_test.go, git_selection_scale_bench_test.go) — not by
// this service-path table.
func BenchmarkEmit(b *testing.B) {
	for _, c := range emitBenchCases() {
		c := c
		b.Run(string(c.Kind), func(b *testing.B) {
			file, err := cassette.LoadFile(c.CassettePath)
			if err != nil {
				b.Fatalf("LoadFile(%q) error = %v, want nil", c.CassettePath, err)
			}

			wantFacts := 0
			for _, sc := range file.Scopes {
				wantFacts += len(sc.Facts)
			}
			if wantFacts == 0 {
				b.Fatalf("cassette %q for %q has zero facts", c.CassettePath, c.Kind)
			}

			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				source := &cassette.Source{File: file}
				committer := &drainCommitter{}
				service := collector.Service{
					Source:       source,
					Committer:    committer,
					PollInterval: time.Hour,
				}
				drainEmit(b, ctx, service)
				if committer.facts != wantFacts {
					b.Fatalf("drained facts = %d, want %d", committer.facts, wantFacts)
				}
			}

			// Report after the timed loop so the metric survives the
			// framework's iteration-count discovery runs and appears in the
			// final benchmark line alongside ns/op and allocs/op.
			b.ReportMetric(float64(wantFacts), "facts")
		})
	}
}

// drainEmit runs the service's Claim -> ingest -> commit cycle once per cassette
// scope until the in-memory source reports the batch is exhausted. It avoids
// Service.Run so the benchmark does not block on the poll interval after the
// batch drains.
func drainEmit(b *testing.B, ctx context.Context, service collector.Service) {
	b.Helper()
	for {
		collected, ok, err := service.Source.Next(ctx)
		if err != nil {
			b.Fatalf("Source.Next() error = %v, want nil", err)
		}
		if !ok {
			return
		}
		if err := service.Committer.CommitScopeGeneration(
			ctx,
			collected.Scope,
			collected.Generation,
			collected.Facts,
		); err != nil {
			b.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
		}
	}
}
