// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifamemaudit

package graphdump

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// TestMemAuditCanonicalizeScale measures peak heap of Canonicalize against
// scale-lab-slot-sized synthetic graphs — the #5009 before/after memory
// evidence. It allocates up to ~1.4 GiB, so it is gated behind the
// `ifamemaudit` build tag (mirroring the `ifafaultinjection` pattern) and runs
// in NO ordinary CI lane: `go test ./...`, `go test -race ./...`, and the
// pre-pr race lane all exclude a tagged file at compile time. Run it explicitly:
//
//	go test -tags ifamemaudit -run TestMemAuditCanonicalizeScale -v ./internal/ifa/graphdump/
func TestMemAuditCanonicalizeScale(t *testing.T) {
	cases := []struct {
		label       string
		nodes, edge int
	}{
		{"small (demo-org x8 synth)", 1000, 4000},
		{"20-repo B-12 tolerance-max", 75000, 300000},
		{"medium slot (~50 repos)", 190000, 760000},
	}
	for _, c := range cases {
		reader := genReader{nodeCount: c.nodes, edgeCount: c.edge}

		var peak uint64
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			var m runtime.MemStats
			for {
				select {
				case <-stop:
					return
				default:
					runtime.ReadMemStats(&m)
					if m.HeapInuse > atomic.LoadUint64(&peak) {
						atomic.StoreUint64(&peak, m.HeapInuse)
					}
					time.Sleep(2 * time.Millisecond)
				}
			}
		}()

		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		out, err := Canonicalize(context.Background(), reader)
		elapsed := time.Since(start)
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		close(stop)
		<-done

		if err != nil {
			t.Fatalf("%s: Canonicalize error: %v", c.label, err)
		}
		mib := func(b uint64) float64 { return float64(b) / (1024 * 1024) }
		t.Logf("%-30s nodes=%d edges=%d out=%.1fMiB | peak HeapInuse=%.0fMiB totalAlloc=%.0fMiB | %s",
			c.label, c.nodes, c.edge, mib(uint64(len(out))),
			mib(atomic.LoadUint64(&peak)), mib(after.TotalAlloc-before.TotalAlloc), elapsed.Round(time.Millisecond))
		runtime.KeepAlive(reader)
	}
}
