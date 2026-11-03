// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

func jsonUnmarshal(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}

// pythonFoldTheoryRawCorpus renders the theory corpus as raw JSON payloads, so
// the benchmark decodes exactly like a loader row scan does.
func pythonFoldTheoryRawCorpus(n int) [][]byte {
	envelopes := pythonFoldTheoryCorpus(n)
	raws := make([][]byte, 0, n)
	for _, envelope := range envelopes {
		raw, err := json.Marshal(envelope.Payload)
		if err != nil {
			panic(err)
		}
		raws = append(raws, raw)
	}
	return raws
}

// pythonFoldTheoryCorpus builds deterministic Python parser file facts that
// exercise every key the Python reader consumes: imports with aliases,
// calls (plain, dynamic-import, plugin-loading), the functions/classes
// decorator buckets, SCIP edges, language-less rows, tombstones, and
// repository ID fallbacks.
func pythonFoldTheoryCorpus(n int) []facts.Envelope {
	out := make([]facts.Envelope, 0, n)
	for i := 0; i < n; i++ {
		imports := []any{
			map[string]any{"source": "requests", "name": "requests", "alias": "rq", "lang": "python"},
			map[string]any{"source": "flask", "name": "Flask", "lang": "python"},
		}
		calls := []any{
			map[string]any{"full_name": "requests.get", "name": "get", "lang": "python"},
			map[string]any{"full_name": "importlib.import_module", "name": "import_module", "lang": "python"},
		}
		functions := []any{
			map[string]any{"name": "view", "decorators": []any{"app.route", "login_required"}},
		}
		classes := []any{
			map[string]any{"name": "Handler", "decorators": []any{"dataclass"}},
		}
		scip := []any{
			map[string]any{"callee_symbol": "scip-python pypi requests 2.31.0 src/requests.get()."},
		}
		fileData := map[string]any{
			"language": "python", "imports": imports, "function_calls": calls,
			"function_calls_scip": scip, "functions": functions, "classes": classes,
		}
		payload := map[string]any{
			"repo_id": fmt.Sprintf("repo-%d", i%4), "relative_path": fmt.Sprintf("pkg/f%d.py", i),
			"parsed_file_data": fileData,
		}
		switch i % 10 {
		case 3:
			payload["repository_id"] = payload["repo_id"]
			delete(payload, "repo_id")
		case 7:
			delete(payload, "parsed_file_data")
		}
		out = append(out, facts.Envelope{
			FactID: fmt.Sprintf("pyfile-%05d", i), FactKind: factload.FactKindFile,
			ScopeID: "scope-py", GenerationID: "gen-1",
			IsTombstone: i%25 == 24, Payload: payload,
		})
	}
	return out
}

// TestPythonFoldTheoryParity is the #9860 theory shim: the Python evidence the
// standalone reachability stage builds from retained envelopes must equal what
// a page-by-page fold of the same envelopes carries in one digest.
func TestPythonFoldTheoryParity(t *testing.T) {
	t.Parallel()

	all := pythonFoldTheoryCorpus(400)
	want := extractPythonReachabilityEvidence(all)

	fold := NewFileReachabilityFold(nil)
	for _, envelope := range all {
		fold.Add(envelope)
	}
	digest, ok := fold.DigestEnvelope()
	if !ok {
		t.Fatal("DigestEnvelope() ok = false, want a digest")
	}
	got := extractPythonReachabilityEvidence([]facts.Envelope{digest})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("folded Python evidence differs:\n got %#v\nwant %#v", got, want)
	}
}

// BenchmarkPythonFoldTheoryRetainedVsFolded is the #9860 theory measurement:
// one pass over raw file payloads that either retains every decoded envelope
// (today's python stage) or folds each envelope and drops it. Live heap is
// what the pass still holds after the load returns. Run with -benchtime=1x:
// each op is one full pass.
func BenchmarkPythonFoldTheoryRetainedVsFolded(b *testing.B) {
	for _, rows := range []int{2000, 10000, 20000} {
		raws := pythonFoldTheoryRawCorpus(rows)
		for _, mode := range []string{"retained", "folded"} {
			b.Run(fmt.Sprintf("rows=%d/%s", rows, mode), func(b *testing.B) {
				b.ReportAllocs()
				var live int64
				for i := 0; i < b.N; i++ {
					runtime.GC()
					var before runtime.MemStats
					runtime.ReadMemStats(&before)
					var held []facts.Envelope
					fold := NewFileReachabilityFold(nil)
					for n, raw := range raws {
						var payload map[string]any
						if err := jsonUnmarshal(raw, &payload); err != nil {
							b.Fatal(err)
						}
						envelope := facts.Envelope{
							FactID: fmt.Sprintf("pyfile-%05d", n), FactKind: factload.FactKindFile,
							ScopeID: "scope-py", GenerationID: "gen-1", Payload: payload,
						}
						if mode == "retained" {
							held = append(held, envelope)
						} else {
							fold.Add(envelope)
						}
					}
					var kept any = held
					if mode == "folded" {
						digest, ok := fold.DigestEnvelope()
						if !ok {
							b.Fatal("no digest")
						}
						kept = digest
					}
					runtime.GC()
					runtime.GC()
					var after runtime.MemStats
					runtime.ReadMemStats(&after)
					live = int64(after.HeapAlloc) - int64(before.HeapAlloc)
					runtime.KeepAlive(kept)
				}
				b.ReportMetric(float64(live), "live_heap_bytes")
				b.ReportMetric(float64(live)/float64(rows), "live_heap_bytes/row")
			})
		}
	}
}
