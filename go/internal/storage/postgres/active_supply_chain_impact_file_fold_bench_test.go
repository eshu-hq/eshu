// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// foldBenchPayload renders one parser-shaped JS/TS file payload: the
// calls-heavy shape the #9781 QA heap profile implies (roughly 140 KB decoded),
// with import aliases and calls the reachability readers consume and the empty
// buckets real parser rows carry.
func foldBenchPayload() []byte {
	rows := func(n int, build func(k int) map[string]any) []map[string]any {
		out := make([]map[string]any, 0, n)
		for k := 0; k < n; k++ {
			out = append(out, build(k))
		}
		return out
	}
	generic := func(kind string) func(int) map[string]any {
		return func(k int) map[string]any {
			return map[string]any{
				"name": kind + strconv.Itoa(k), "line_number": k + 1, "end_line": k + 9, "type": "const",
				"args": []any{"a", "b", "c"}, "docstring": "does a thing for " + kind, "decorators": []any{},
				"source": "export const x = (a, b) => a + b + " + strconv.Itoa(k),
			}
		}
	}
	parsed := map[string]any{
		"language": "typescript", "lang": "typescript",
		"imports": rows(8, func(k int) map[string]any {
			return map[string]any{"source": "@scope/pkg" + strconv.Itoa(k%3), "name": "n" + strconv.Itoa(k), "alias": "a" + strconv.Itoa(k), "import_type": "import", "line_number": k + 1, "lang": "typescript"}
		}),
		"function_calls": rows(130, func(k int) map[string]any {
			return map[string]any{"name": "fn" + strconv.Itoa(k), "full_name": "a" + strconv.Itoa(k%8) + ".fn" + strconv.Itoa(k), "call_kind": "function_call", "line_number": k + 3, "args": []any{"x", "y"}, "inferred_obj_type": "Obj" + strconv.Itoa(k%9), "lang": "typescript"}
		}),
		"function_calls_scip": rows(10, func(k int) map[string]any {
			return map[string]any{"callee_symbol": "scip-typescript npm @scope/pkg" + strconv.Itoa(k%3) + " 1.0.0 src/`x`/f" + strconv.Itoa(k) + "().", "callee_name": "f" + strconv.Itoa(k), "line": k + 1}
		}),
		"functions": rows(6, generic("func")), "variables": rows(8, generic("var")), "classes": rows(8, generic("cls")),
		"cfg_blocks": rows(10, generic("blk")),
	}
	for k := 0; k < 70; k++ {
		parsed["empty_bucket_"+strconv.Itoa(k)] = []any{}
	}
	raw, err := json.Marshal(map[string]any{
		"repo_id": "repo-a", "relative_path": "src/f.ts", "language": "typescript", "parsed_file_data": parsed,
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func foldBenchDB(rows int, raw []byte) *supplyChainImpactRoutingDB {
	routed := &supplyChainImpactRoutingDB{
		branchRows: map[string][][2]string{"vulnerability_cve_id": {{"vuln-affected-package:core", "vulnerability.affected_package"}}},
		facts:      map[string]string{"vuln-affected-package:core": "vulnerability.affected_package"},
		payloads:   make(map[string][]byte, rows),
	}
	fileRows := make([][2]string, 0, rows)
	for n := 0; n < rows; n++ {
		id := fmt.Sprintf("file:%07d", n)
		routed.facts[id], routed.payloads[id] = factload.FactKindFile, raw
		fileRows = append(fileRows, [2]string{id, factload.FactKindFile})
	}
	routed.branchRows["file_repo_id"] = fileRows
	return routed
}

// BenchmarkSupplyChainImpactFileEvidenceLoad is the #9781 before/after on the
// production loader path. "before" is a load with no FileFold, which keeps
// every decoded file payload for the pass; "after" folds them. The
// live_heap_bytes metric is heap still reachable after the load returns, with
// the loaded envelopes and the fold held, which is what a pass carries until
// its index builders run. Run with -benchtime=1x: each op is one full pass.
func BenchmarkSupplyChainImpactFileEvidenceLoad(b *testing.B) {
	raw := foldBenchPayload()
	for _, rows := range []int{2000, 10000, 20000} {
		for _, mode := range []string{"before", "after"} {
			b.Run(fmt.Sprintf("rows=%d/%s", rows, mode), func(b *testing.B) {
				routed := foldBenchDB(rows, raw)
				store := NewFactStore(routed)
				b.ReportAllocs()
				var live int64
				for i := 0; i < b.N; i++ {
					filter := supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-9781"}, FileRepositoryIDs: []string{"repo-a"}}
					if mode == "after" {
						filter.FileFold = supplychaincore.NewFileReachabilityFold(nil)
					}
					runtime.GC()
					var before runtime.MemStats
					runtime.ReadMemStats(&before)
					loaded, _, err := store.ListActiveSupplyChainImpactFactsWithStats(context.Background(), filter)
					if err != nil {
						b.Fatal(err)
					}
					runtime.GC()
					runtime.GC()
					var after runtime.MemStats
					runtime.ReadMemStats(&after)
					live = int64(after.HeapAlloc) - int64(before.HeapAlloc)
					runtime.KeepAlive(loaded)
					runtime.KeepAlive(filter)
				}
				b.ReportMetric(float64(live), "live_heap_bytes")
				b.ReportMetric(float64(live)/float64(rows), "live_heap_bytes/row")
			})
		}
	}
}

// BenchmarkSupplyChainImpactFileEvidenceClone measures what the authorized
// loader's cloneAuthorizedImpactEnvelopes adds on top of a loaded pass: the
// deep copy of every returned envelope. After the fold the file payloads are
// not in the result, so the copy no longer scales with file bytes (#9781).
// live_heap_bytes holds both the loaded envelopes and their clone.
func BenchmarkSupplyChainImpactFileEvidenceClone(b *testing.B) {
	raw := foldBenchPayload()
	for _, rows := range []int{2000, 10000} {
		for _, mode := range []string{"before", "after"} {
			b.Run(fmt.Sprintf("rows=%d/%s", rows, mode), func(b *testing.B) {
				store := NewFactStore(foldBenchDB(rows, raw))
				var live int64
				for i := 0; i < b.N; i++ {
					filter := supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-9781"}, FileRepositoryIDs: []string{"repo-a"}}
					if mode == "after" {
						filter.FileFold = supplychaincore.NewFileReachabilityFold(nil)
					}
					runtime.GC()
					var before runtime.MemStats
					runtime.ReadMemStats(&before)
					loaded, _, err := store.ListActiveSupplyChainImpactFactsWithStats(context.Background(), filter)
					if err != nil {
						b.Fatal(err)
					}
					if digest, ok := filter.FileFold.DigestEnvelope(); ok {
						loaded = append(loaded, digest)
					}
					cloned := cloneAuthorizedImpactEnvelopes(loaded)
					runtime.GC()
					runtime.GC()
					var after runtime.MemStats
					runtime.ReadMemStats(&after)
					live = int64(after.HeapAlloc) - int64(before.HeapAlloc)
					runtime.KeepAlive(loaded)
					runtime.KeepAlive(cloned)
					runtime.KeepAlive(filter)
				}
				b.ReportMetric(float64(live), "live_heap_bytes")
			})
		}
	}
}

// BenchmarkSupplyChainImpactFileEvidenceConcurrent runs eight passes at once,
// the reducer's worker count, and reports the highest heap still reachable
// after a GC sampled while they ran (peak_live_heap_bytes), the number that
// decides whether eight workers fit a container (#9781). Each worker has its
// own routing fake because the fake records calls without a lock.
func BenchmarkSupplyChainImpactFileEvidenceConcurrent(b *testing.B) {
	raw := foldBenchPayload()
	const workers = 8
	for _, rows := range []int{2000, 20000} {
		for _, mode := range []string{"before", "after"} {
			b.Run(fmt.Sprintf("workers=%d/rows=%d/%s", workers, rows, mode), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stores := make([]*FactStore, workers)
					for w := range stores {
						stores[w] = NewFactStore(foldBenchDB(rows, raw))
					}
					runtime.GC()
					var base runtime.MemStats
					runtime.ReadMemStats(&base)
					var peak int64
					stop, done := make(chan struct{}), make(chan struct{})
					go func() {
						defer close(done)
						for {
							select {
							case <-stop:
								return
							case <-time.After(250 * time.Millisecond):
							}
							runtime.GC()
							var ms runtime.MemStats
							runtime.ReadMemStats(&ms)
							if live := int64(ms.HeapAlloc) - int64(base.HeapAlloc); live > peak {
								peak = live
							}
						}
					}()
					var wg sync.WaitGroup
					for w := range stores {
						wg.Add(1)
						go func() {
							defer wg.Done()
							filter := supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-9781"}, FileRepositoryIDs: []string{"repo-a"}}
							if mode == "after" {
								filter.FileFold = supplychaincore.NewFileReachabilityFold(nil)
							}
							loaded, _, err := stores[w].ListActiveSupplyChainImpactFactsWithStats(context.Background(), filter)
							if err != nil {
								b.Error(err)
							}
							runtime.KeepAlive(loaded)
							runtime.KeepAlive(filter)
						}()
					}
					wg.Wait()
					close(stop)
					<-done
					b.ReportMetric(float64(peak), "peak_live_heap_bytes")
				}
			})
		}
	}
}
