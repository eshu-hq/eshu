// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Theory shim for #9861: typed decode of ONLY the file-payload fields the
// JS/TS, Python, and JVM reachability readers consume, against the current
// map[string]any decode. Production code is untouched; everything here lives
// in this _test.go file.
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

// typedFileStringSlice mirrors payloadcore.SemanticPayloadStringSlice: only
// real strings survive, everything else is skipped rather than failing the
// whole decode. A plain []string field would reject a mixed array that the
// map path tolerates today, so the shared key definition must pin this.
type typedFileStringSlice []string

func (s *typedFileStringSlice) UnmarshalJSON(raw []byte) error {
	var items []any
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	*s = out
	return nil
}

// typedFileImport holds the only import-row keys any of the three readers
// consume: source/name/alias/import_type/lang (JS/TS+Python) plus
// full_import_name (JVM).
type typedFileImport struct {
	Source         string `json:"source"`
	Name           string `json:"name"`
	Alias          string `json:"alias"`
	ImportType     string `json:"import_type"`
	Lang           string `json:"lang"`
	FullImportName string `json:"full_import_name"`
}

// typedFileCall holds the only call-row keys consumed: full_name/name/lang
// (JS/TS+Python) plus inferred_obj_type (JVM).
type typedFileCall struct {
	FullName        string `json:"full_name"`
	Name            string `json:"name"`
	Lang            string `json:"lang"`
	InferredObjType string `json:"inferred_obj_type"`
}

// typedFileSCIPCall holds the only SCIP-row keys consumed: package,
// package_name, import_source, callee_package, callee_symbol (JS/TS) plus
// callee_name (JVM).
type typedFileSCIPCall struct {
	Package       string `json:"package"`
	PackageName   string `json:"package_name"`
	ImportSource  string `json:"import_source"`
	CalleePackage string `json:"callee_package"`
	CalleeSymbol  string `json:"callee_symbol"`
	CalleeName    string `json:"callee_name"`
}

// typedFileEntity holds the only entity keys consumed: decorators
// (Python functions/classes buckets).
type typedFileEntity struct {
	Decorators typedFileStringSlice `json:"decorators"`
}

// typedParsedFileData holds the only parsed_file_data keys consumed:
// language/lang plus the five buckets above. Every other parser key
// (source snippets, variables, symbols, tokens, loc, ...) is skipped by the
// decoder and never allocated as map/object graphs.
type typedParsedFileData struct {
	Language  string              `json:"language"`
	Lang      string              `json:"lang"`
	Imports   []typedFileImport   `json:"imports"`
	Calls     []typedFileCall     `json:"function_calls"`
	SCIPCalls []typedFileSCIPCall `json:"function_calls_scip"`
	Functions []typedFileEntity   `json:"functions"`
	Classes   []typedFileEntity   `json:"classes"`
}

// typedFilePayload holds the only envelope-payload keys consumed: repo_id,
// repository_id (JS/TS fallback), relative_path (JVM), parsed_file_data.
type typedFilePayload struct {
	RepoID       string               `json:"repo_id"`
	RepositoryID string               `json:"repository_id"`
	RelativePath string               `json:"relative_path"`
	Parsed       *typedParsedFileData `json:"parsed_file_data"`
}

func decodeTypedFilePayload(raw []byte) (typedFilePayload, error) {
	var out typedFilePayload
	if err := json.Unmarshal(raw, &out); err != nil {
		return typedFilePayload{}, err
	}
	return out, nil
}

// projectTypedFileEnvelope rebuilds the envelope the existing per-file bodies
// read, carrying ONLY the used keys. Parity between the index built from
// these envelopes and the index built from full map-decoded envelopes proves
// a typed decode of the used fields is sufficient.
func projectTypedFileEnvelope(typed typedFilePayload, factID, scopeID string, tombstone bool) facts.Envelope {
	payloadMap := map[string]any{
		"repo_id":       typed.RepoID,
		"repository_id": typed.RepositoryID,
		"relative_path": typed.RelativePath,
	}
	if typed.Parsed != nil {
		fileData := map[string]any{
			"language": typed.Parsed.Language,
			"lang":     typed.Parsed.Lang,
		}
		imports := make([]any, 0, len(typed.Parsed.Imports))
		for _, entry := range typed.Parsed.Imports {
			imports = append(imports, map[string]any{
				"source": entry.Source, "name": entry.Name, "alias": entry.Alias,
				"import_type": entry.ImportType, "lang": entry.Lang,
				"full_import_name": entry.FullImportName,
			})
		}
		fileData["imports"] = imports
		calls := make([]any, 0, len(typed.Parsed.Calls))
		for _, entry := range typed.Parsed.Calls {
			calls = append(calls, map[string]any{
				"full_name": entry.FullName, "name": entry.Name, "lang": entry.Lang,
				"inferred_obj_type": entry.InferredObjType,
			})
		}
		fileData["function_calls"] = calls
		scip := make([]any, 0, len(typed.Parsed.SCIPCalls))
		for _, entry := range typed.Parsed.SCIPCalls {
			scip = append(scip, map[string]any{
				"package": entry.Package, "package_name": entry.PackageName,
				"import_source": entry.ImportSource, "callee_package": entry.CalleePackage,
				"callee_symbol": entry.CalleeSymbol, "callee_name": entry.CalleeName,
			})
		}
		fileData["function_calls_scip"] = scip
		functions := make([]any, 0, len(typed.Parsed.Functions))
		for _, entity := range typed.Parsed.Functions {
			decorators := make([]any, 0, len(entity.Decorators))
			for _, decorator := range entity.Decorators {
				decorators = append(decorators, decorator)
			}
			functions = append(functions, map[string]any{"decorators": decorators})
		}
		fileData["functions"] = functions
		classes := make([]any, 0, len(typed.Parsed.Classes))
		for _, entity := range typed.Parsed.Classes {
			decorators := make([]any, 0, len(entity.Decorators))
			for _, decorator := range entity.Decorators {
				decorators = append(decorators, decorator)
			}
			classes = append(classes, map[string]any{"decorators": decorators})
		}
		fileData["classes"] = classes
		payloadMap["parsed_file_data"] = fileData
	}
	return facts.Envelope{
		FactID: factID, FactKind: factload.FactKindFile, ScopeID: scopeID,
		Payload: payloadMap, IsTombstone: tombstone,
	}
}

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
