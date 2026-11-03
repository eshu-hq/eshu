// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Theory proof for #9861: the promoted typed decode of ONLY the file-payload
// fields the JS/TS, Python, and JVM reachability readers consume (see
// file_payload_typed.go), against the current map[string]any decode. The
// corpus, envelope decoders, and parity assertions below run against the
// production definitions; the structs and decode helpers they replaced now
// live in the non-test file.
package core

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
)

// typedFileTheoryMeta carries the envelope fields that live outside the
// payload column.
type typedFileTheoryMeta struct {
	factID    string
	scopeID   string
	tombstone bool
}

// typedFileTheoryRawCorpus returns raw payload JSON bytes shaped like the
// collector's file facts: the used keys plus heavy unused ballast (source
// snippets, variables, symbols, tokens) that the map decode allocates and a
// typed decode skips.
func typedFileTheoryRawCorpus(seed int64, n int) ([][]byte, []typedFileTheoryMeta) {
	rng := rand.New(rand.NewSource(seed))
	packages := []string{"react", "@scope/api", "lodash", "requests", "org.apache.commons/commons-lang3"}
	languages := []string{"typescript", "javascript", "tsx", "python", "java", "go", ""}
	raw := make([][]byte, 0, n)
	meta := make([]typedFileTheoryMeta, 0, n)
	for i := 0; i < n; i++ {
		var imports, calls, scip, functions, classes []any
		for k := 0; k < rng.Intn(4); k++ {
			imports = append(imports, map[string]any{
				"source": packages[rng.Intn(len(packages))], "name": fmt.Sprintf("n%d", k),
				"alias": fmt.Sprintf("a%d", k), "import_type": "import",
				"full_import_name": packages[rng.Intn(len(packages))],
			})
		}
		for k := 0; k < rng.Intn(6); k++ {
			calls = append(calls, map[string]any{
				"full_name": fmt.Sprintf("a%d.run", rng.Intn(4)), "name": "run",
				"inferred_obj_type": packages[rng.Intn(len(packages))],
			})
		}
		for k := 0; k < rng.Intn(3); k++ {
			scip = append(scip, map[string]any{
				"package":       "npm " + packages[rng.Intn(len(packages))] + " 1.0.0",
				"callee_symbol": "scip-typescript npm " + packages[rng.Intn(len(packages))] + " 1.0.0 x/y().",
				"callee_name":   packages[rng.Intn(len(packages))],
			})
		}
		for k := 0; k < rng.Intn(3); k++ {
			functions = append(functions, map[string]any{"name": "f", "decorators": []any{"a0.route", "plain"}})
		}
		classes = append(classes, map[string]any{"name": "C", "decorators": []any{"a1.component"}})
		// Unused ballast: real parser payloads carry per-entity source and
		// symbol tables no reachability reader touches.
		variables := make([]any, 0, 8)
		for v := 0; v < 8; v++ {
			variables = append(variables, map[string]any{
				"name": fmt.Sprintf("var%d", v), "source": "const var SnippetTextPayloadPadding0123456789",
				"loc": map[string]any{"line": v, "col": 3}, "inferred_type": "string",
			})
		}
		fileData := map[string]any{
			"language": languages[rng.Intn(len(languages))], "path": fmt.Sprintf("src/f%d.ts", i),
			"imports": imports, "function_calls": calls, "function_calls_scip": scip,
			"functions": functions, "classes": classes, "variables": variables,
			"source":  "padding source text body 0123456789 abcdefghijklmnopqrstuvwxyz",
			"symbols": []any{"sym-a", "sym-b", "sym-c", "sym-d"},
			"tokens":  []any{"tok-a", "tok-b", "tok-c", "tok-d", "tok-e", "tok-f"},
		}
		payloadMap := map[string]any{
			"graph_id":   fmt.Sprintf("repo-%d:src/f%d.ts", rng.Intn(3), i),
			"graph_kind": "file", "repo_id": fmt.Sprintf("repo-%d", rng.Intn(3)),
			"relative_path": fmt.Sprintf("src/f%d.ts", i), "parsed_file_data": fileData,
			"is_dependency": false, "language": languages[rng.Intn(len(languages))],
		}
		data, err := json.Marshal(payloadMap)
		if err != nil {
			panic(err)
		}
		raw = append(raw, data)
		meta = append(meta, typedFileTheoryMeta{
			factID: fmt.Sprintf("file-%04d", i), scopeID: fmt.Sprintf("repo-%d", rng.Intn(3)),
			tombstone: rng.Intn(15) == 0,
		})
	}
	return raw, meta
}

func decodeTypedFileTheoryEnvelopes(raw [][]byte, meta []typedFileTheoryMeta) []facts.Envelope {
	out := make([]facts.Envelope, 0, len(raw))
	for i, data := range raw {
		typed, err := decodeTypedFilePayload(data)
		if err != nil {
			panic(err)
		}
		out = append(out, projectTypedFileEnvelope(typed, meta[i].factID, meta[i].scopeID, meta[i].tombstone))
	}
	return out
}

func decodeMapFileTheoryEnvelopes(raw [][]byte, meta []typedFileTheoryMeta) []facts.Envelope {
	out := make([]facts.Envelope, 0, len(raw))
	for i, data := range raw {
		decoded, err := payloadstore.UnmarshalPayload(data)
		if err != nil {
			panic(err)
		}
		out = append(out, facts.Envelope{
			FactID: meta[i].factID, FactKind: factload.FactKindFile, ScopeID: meta[i].scopeID,
			Payload: decoded, IsTombstone: meta[i].tombstone,
		})
	}
	return out
}

type typedFileTheoryIndexes struct {
	jsTS   jsTSPackageReachabilityIndex
	python map[string]pythonReachabilityRepositoryEvidence
	jvm    jvmReachabilityIndex
}

func buildTypedFileTheoryIndexes(envelopes []facts.Envelope) typedFileTheoryIndexes {
	return typedFileTheoryIndexes{
		jsTS:   buildJSTSPackageReachabilityIndex(envelopes),
		python: extractPythonReachabilityEvidence(envelopes),
		jvm:    buildJVMReachabilityIndex(envelopes),
	}
}

// TestTypedFilePayloadDecodeParity is the #9861 failing-parity-test-first
// candidate: the same raw payload bytes decoded typed (used fields only,
// projected through the existing per-file bodies) must produce identical
// JS/TS, Python (including the decorator path), and JVM indexes as the map
// decode. It also runs the FileReachabilityFold.Add path on both sides.
func TestTypedFilePayloadDecodeParity(t *testing.T) {
	t.Parallel()

	for _, seed := range []int64{1, 7, 42} {
		raw, meta := typedFileTheoryRawCorpus(seed, 120)

		want := buildTypedFileTheoryIndexes(decodeMapFileTheoryEnvelopes(raw, meta))
		got := buildTypedFileTheoryIndexes(decodeTypedFileTheoryEnvelopes(raw, meta))
		if !reflect.DeepEqual(got.jsTS, want.jsTS) {
			t.Fatalf("seed %d: JS/TS index differs:\n got %#v\nwant %#v", seed, got.jsTS, want.jsTS)
		}
		if !reflect.DeepEqual(got.python, want.python) {
			t.Fatalf("seed %d: Python evidence differs:\n got %#v\nwant %#v", seed, got.python, want.python)
		}
		if !reflect.DeepEqual(got.jvm, want.jvm) {
			t.Fatalf("seed %d: JVM index differs:\n got %#v\nwant %#v", seed, got.jvm, want.jvm)
		}

		mapEnvelopes := decodeMapFileTheoryEnvelopes(raw, meta)
		typedEnvelopes := decodeTypedFileTheoryEnvelopes(raw, meta)
		mapFold, typedFold := NewFileReachabilityFold(nil), NewFileReachabilityFold(nil)
		for _, envelope := range mapEnvelopes {
			mapFold.Add(envelope)
		}
		for _, envelope := range typedEnvelopes {
			typedFold.Add(envelope)
		}
		mapDigest, mapOK := mapFold.DigestEnvelope()
		typedDigest, typedOK := typedFold.DigestEnvelope()
		if mapOK != typedOK {
			t.Fatalf("seed %d: DigestEnvelope ok = %v map, %v typed", seed, mapOK, typedOK)
		}
		if mapOK {
			mapJSTS, mapPython := foldReachabilityIndexes([]facts.Envelope{mapDigest})
			typedJSTS, typedPython := foldReachabilityIndexes([]facts.Envelope{typedDigest})
			if !reflect.DeepEqual(typedJSTS, mapJSTS) {
				t.Fatalf("seed %d: folded JS/TS index differs", seed)
			}
			if !reflect.DeepEqual(typedPython, mapPython) {
				t.Fatalf("seed %d: folded Python evidence differs", seed)
			}
		}
	}
}

// BenchmarkTypedFileDecodeTheory decodes representative parser payloads both
// ways at 1x/5x/10x. The map side is the scanSizedSupplyChainImpactFact path
// (payloadstore.UnmarshalPayload over the raw payload column bytes); the
// typed side decodes only the used fields.
func BenchmarkTypedFileDecodeTheory(b *testing.B) {
	raw, _ := typedFileTheoryRawCorpus(9861, 1000)
	for _, scale := range []struct {
		name string
		n    int
	}{{"1x", 100}, {"5x", 500}, {"10x", 1000}} {
		batch := raw[:scale.n]
		b.Run("map_decode_"+scale.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				for _, data := range batch {
					decoded, err := payloadstore.UnmarshalPayload(data)
					if err != nil {
						b.Fatal(err)
					}
					_ = decoded
				}
			}
		})
		b.Run("typed_decode_"+scale.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				for _, data := range batch {
					typed, err := decodeTypedFilePayload(data)
					if err != nil {
						b.Fatal(err)
					}
					_ = typed
				}
			}
		})
	}
}
