// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
)

// typedFileScanPayloads returns hostile file-payload bytes with the heavy
// ballast real parser rows carry: padded strings, numbers/bools/nulls as
// scalars, mixed-type arrays with nulls, non-object parsed_file_data, and
// bare-string decorators.
func typedFileScanPayloads(t *testing.T) []string {
	t.Helper()
	return []string{
		`{"repo_id":"  repo-a  ","repository_id":"  repo-a  ","relative_path":"  src/a.ts  ","parsed_file_data":{"language":"  typescript  ","lang":"typescript","imports":[{"source":"  react  ","name":"useState","alias":"useState","import_type":"import","lang":"typescript","full_import_name":"react"}],"function_calls":[{"full_name":"useState.run","name":"run"}],"function_calls_scip":[{"package":"npm react 1.0.0","callee_symbol":"scip-typescript npm react 1.0.0 x/y()."}],"functions":[{"decorators":["  a0.route  ",""]}],"classes":[{"name":"C"}],"variables":[{"name":"v","source":"const SnippetTextPayloadPadding0123456789","loc":{"line":1}}],"source":"padding source text body 0123456789","symbols":["sym-a"],"tokens":["tok-a","tok-b"]}}`,
		`{"repo_id":"repo-a","relative_path":"pkg/f.py","parsed_file_data":{"language":"python","imports":[null,42,{"source":7,"name":true,"alias":null,"import_type":"import","lang":"python"}],"function_calls":[null,{"full_name":"requests.get","name":"get","lang":"python"}],"functions":[{"decorators":[null,7,"  app.route  ",""]}],"classes":[{"decorators":"bare-decorator"}],"variables":[{"name":"v","source":"padding"}],"source":"padding"}}`,
		`{"repo_id":"repo-b","relative_path":"src/main/java/example/App.java","parsed_file_data":{"lang":true,"imports":[{"source":123,"name":456,"full_import_name":"org.apache.commons.lang3.StringUtils"}],"function_calls":[{"full_name":null,"inferred_obj_type":"org.apache.commons.lang3.StringUtils","name":7}],"function_calls_scip":[null,{"callee_symbol":"org.apache.commons.lang3.StringUtils#method().","callee_name":null}]}}`,
		`{"repo_id":"repo-b","relative_path":"src/c.java","parsed_file_data":"oops","variables":[{"name":"v","source":"padding"}]}`,
		`{"repo_id":"repo-b","relative_path":"src/d.java"}`,
		`{"repo_id":null,"relative_path":null,"parsed_file_data":null}`,
	}
}

// typedFileScanRow renders one sixteen-column loader row for a payload.
func typedFileScanRow(id, scope, generation, kind string, tombstone bool, raw []byte) []any {
	return []any{
		id, scope, generation, kind, "stable:" + id, "1.0.0", "synthetic",
		int64(1), "reported", "synthetic", "source:" + id, "", "",
		time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC), tombstone, raw,
	}
}

func decodeMapScanEnvelope(t *testing.T, id, scope string, tombstone bool, raw []byte) facts.Envelope {
	t.Helper()
	decoded, err := payloadstore.UnmarshalPayload(raw)
	if err != nil {
		t.Fatal(err)
	}
	return facts.Envelope{
		FactID: id, FactKind: factload.FactKindFile, ScopeID: scope,
		Payload: decoded, IsTombstone: tombstone,
	}
}

// TestScanTypedSizedSupplyChainImpactFactMatchesMap proves the file-fold row
// scan carries the same envelope metadata and raw-byte size as the map path
// while projecting only the used payload keys.
func TestScanTypedSizedSupplyChainImpactFactMatchesMap(t *testing.T) {
	t.Parallel()

	for i, body := range typedFileScanPayloads(t) {
		raw := []byte(body)
		rows := &queueFakeRows{rows: [][]any{typedFileScanRow("file-scan", "scope-s", "gen-g", factload.FactKindFile, false, raw)}}
		typed, typedSize, err := scanTypedSizedSupplyChainImpactFact(rows)
		if err != nil {
			t.Fatalf("payload %d: scanTypedSizedSupplyChainImpactFact() error = %v", i, err)
		}
		mapRows := &queueFakeRows{rows: [][]any{typedFileScanRow("file-scan", "scope-s", "gen-g", factload.FactKindFile, false, raw)}}
		want, wantSize, err := scanSizedSupplyChainImpactFact(mapRows)
		if err != nil {
			t.Fatalf("payload %d: scanSizedSupplyChainImpactFact() error = %v", i, err)
		}
		if typedSize != wantSize || typedSize != len(raw) {
			t.Fatalf("payload %d: size = %d, want raw %d", i, typedSize, len(raw))
		}
		for _, field := range []struct {
			name string
			got  any
			want any
		}{
			{"FactID", typed.FactID, want.FactID},
			{"ScopeID", typed.ScopeID, want.ScopeID},
			{"GenerationID", typed.GenerationID, want.GenerationID},
			{"FactKind", typed.FactKind, want.FactKind},
			{"ObservedAt", typed.ObservedAt, want.ObservedAt},
			{"IsTombstone", typed.IsTombstone, want.IsTombstone},
			{"SourceRef", typed.SourceRef, want.SourceRef},
		} {
			if !reflect.DeepEqual(field.got, field.want) {
				t.Fatalf("payload %d: %s = %#v, want %#v", i, field.name, field.got, field.want)
			}
		}
		if _, ok := typed.Payload["parsed_file_data"]; (body == `{"repo_id":"repo-b","relative_path":"src/c.java","parsed_file_data":"oops","variables":[{"name":"v","source":"padding"}]}`) && ok {
			t.Fatalf("payload %d: non-object parsed_file_data must project absent", i)
		}
		for _, ballast := range []string{"variables", "source", "symbols", "tokens"} {
			if fileData, ok := typed.Payload["parsed_file_data"].(map[string]any); ok {
				if _, ok := fileData[ballast]; ok {
					t.Fatalf("payload %d: ballast key %q survives the typed projection", i, ballast)
				}
			}
			if _, ok := typed.Payload[ballast]; ok {
				t.Fatalf("payload %d: top-level ballast key %q survives the typed projection", i, ballast)
			}
		}
	}
}

// TestFoldSupplyChainImpactFilePageTypedDecodeMatchesMap proves the wired
// file-fold page folds the same index as map-decoded fold.Add calls with
// identical raw-byte accounting.
func TestFoldSupplyChainImpactFilePageTypedDecodeMatchesMap(t *testing.T) {
	t.Parallel()

	bodies := typedFileScanPayloads(t)
	routed := &supplyChainImpactRoutingDB{
		branchRows: map[string][][2]string{},
		facts:      map[string]string{},
		payloads:   map[string][]byte{},
	}
	var page []string
	var wantBytes int64
	for i, body := range bodies {
		id := string(rune('a'+i)) + "-file"
		routed.facts[id] = factload.FactKindFile
		routed.payloads[id] = []byte(body)
		page = append(page, id)
		wantBytes += int64(len(body))
	}
	sort.Strings(page)

	fold := supplychaincore.NewFileReachabilityFold(nil)
	stats := &supplychaincore.SupplyChainImpactActiveEvidenceLoadStats{}
	gotBytes, err := foldSupplyChainImpactFilePage(context.Background(), &supplyChainImpactRoutingTx{db: routed}, page, fold, stats)
	if err != nil {
		t.Fatalf("foldSupplyChainImpactFilePage() error = %v", err)
	}
	if gotBytes != wantBytes {
		t.Fatalf("page bytes = %d, want raw total %d", gotBytes, wantBytes)
	}
	if stats.FoldedPayloadBytes != wantBytes {
		t.Fatalf("FoldedPayloadBytes = %d, want raw total %d", stats.FoldedPayloadBytes, wantBytes)
	}

	wantFold := supplychaincore.NewFileReachabilityFold(nil)
	for _, id := range page {
		wantFold.Add(decodeMapScanEnvelope(t, id, "scope:"+id, false, routed.payloads[id]))
	}
	if fold.Files() != wantFold.Files() {
		t.Fatalf("fold files = %d, want %d", fold.Files(), wantFold.Files())
	}
	if !reflect.DeepEqual(fold, wantFold) {
		t.Fatalf("typed fold differs from map fold:\n got %#v\nwant %#v", fold, wantFold)
	}

	// Malformed JSON still fails the page exactly like the map path.
	bad := &supplyChainImpactRoutingDB{
		branchRows: map[string][][2]string{},
		facts:      map[string]string{"bad-file": factload.FactKindFile},
		payloads:   map[string][]byte{"bad-file": []byte(`{"repo_id":`)},
	}
	if _, err := foldSupplyChainImpactFilePage(context.Background(), &supplyChainImpactRoutingTx{db: bad}, []string{"bad-file"}, supplychaincore.NewFileReachabilityFold(nil), &supplychaincore.SupplyChainImpactActiveEvidenceLoadStats{}); err == nil {
		t.Fatal("foldSupplyChainImpactFilePage(malformed) error = nil, want failure")
	}
}

// TestFoldPythonReachabilityFilesTypedDecodeMatchesMap proves the wired
// Python fold page folds the same index as map-decoded fold.Add calls with
// identical raw-byte accounting.
func TestFoldPythonReachabilityFilesTypedDecodeMatchesMap(t *testing.T) {
	t.Parallel()

	// Bodies 1..4 carry exact repo_id filter keys; body 0's padded repo_id
	// and body 5's null repo_id are excluded by the repo_id = ANY(...)
	// predicate itself, identically on both paths.
	bodies := typedFileScanPayloads(t)[1:5]
	routed := &supplyChainImpactRoutingDB{
		branchRows: map[string][][2]string{},
		facts:      map[string]string{},
		payloads:   map[string][]byte{},
	}
	var wantFiles int
	var wantBytes int64
	n := 0
	for _, body := range bodies {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Fatal(err)
		}
		repo, _ := decoded["repo_id"].(string)
		if repo == "" {
			continue
		}
		id := repo + "-pyfile-" + string(rune('a'+n))
		n++
		routed.facts[id] = factload.FactKindFile
		routed.payloads[id] = []byte(body)
		wantFiles++
		wantBytes += int64(len(body))
	}
	store := NewFactStore(routed)
	fold := supplychaincore.NewFileReachabilityFold(nil)
	files, payloadBytes, err := store.FoldPythonReachabilityFiles(
		context.Background(), "scope-py", "gen-py", "repo_id", []string{"repo-a", "repo-b"}, fold,
	)
	if err != nil {
		t.Fatalf("FoldPythonReachabilityFiles() error = %v", err)
	}
	if files != wantFiles {
		t.Fatalf("folded files = %d, want %d", files, wantFiles)
	}
	if payloadBytes != wantBytes {
		t.Fatalf("folded bytes = %d, want raw total %d", payloadBytes, wantBytes)
	}

	wantFold := supplychaincore.NewFileReachabilityFold(nil)
	for id, raw := range routed.payloads {
		wantFold.Add(decodeMapScanEnvelope(t, id, "scope-py", false, raw))
	}
	if !reflect.DeepEqual(fold, wantFold) {
		t.Fatalf("typed python fold differs from map fold:\n got %#v\nwant %#v", fold, wantFold)
	}
}

// TestListActiveJVMReachabilityFactsTypedDecodeMatchesMap proves the wired
// JVM page carries the same metadata as the map path while projecting only
// the used payload keys with trimmed values.
func TestListActiveJVMReachabilityFactsTypedDecodeMatchesMap(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"repo_id":"  repo://example/jvm  ","relative_path":"src/main/java/example/App.java","parsed_file_data":{"lang":"  java  ","imports":[{"source":123,"name":"  org.apache.logging.log4j.Logger  ","full_import_name":"org.apache.logging.log4j.Logger","line_number":3}],"function_calls":[{"full_name":null,"inferred_obj_type":"  org.apache.commons.lang3.StringUtils  ","name":7}],"function_calls_scip":[{"callee_symbol":"  org.apache.commons.lang3.StringUtils#method().  ","callee_name":null}],"variables":[{"name":"v","source":"padding"}],"source":"padding source text"}}`)
	observedAt := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"fact-jvm-match",
					"scope-jvm",
					"generation-jvm",
					"file",
					"file:repo:jvm:App.java",
					"1.0.0",
					"git",
					int64(0),
					"observed",
					"git",
					"file-key",
					"file:///repo/src/main/java/example/App.java",
					"record-jvm",
					observedAt,
					false,
					raw,
				}},
			},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActiveJVMReachabilityFacts(context.Background(), supplychaincore.JVMReachabilityFactFilter{
		RepositoryIDs: []string{"repo://example/jvm"},
		APIPackages:   []string{"org.apache.logging.log4j"},
	})
	if err != nil {
		t.Fatalf("ListActiveJVMReachabilityFacts() error = %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d facts, want 1", len(loaded))
	}
	got := loaded[0]
	if got.FactID != "fact-jvm-match" || got.ScopeID != "scope-jvm" || got.GenerationID != "generation-jvm" || got.FactKind != factload.FactKindFile {
		t.Fatalf("identity = %+v, want the scanned row identity", got)
	}
	if !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("ObservedAt = %v, want %v", got.ObservedAt, observedAt)
	}
	if got.SourceRef.SourceSystem != "git" || got.SourceRef.FactKey != "file-key" {
		t.Fatalf("SourceRef = %+v, want the scanned row ref", got.SourceRef)
	}
	wantPayload := map[string]any{
		"repo_id": "repo://example/jvm", "repository_id": "", "relative_path": "src/main/java/example/App.java",
		"parsed_file_data": map[string]any{
			"language": "", "lang": "java",
			"imports": []any{map[string]any{
				"source": "123", "name": "org.apache.logging.log4j.Logger", "alias": "",
				"import_type": "", "lang": "", "full_import_name": "org.apache.logging.log4j.Logger",
			}},
			"function_calls": []any{map[string]any{
				"full_name": "", "name": "7", "lang": "", "inferred_obj_type": "org.apache.commons.lang3.StringUtils",
			}},
			"function_calls_scip": []any{map[string]any{
				"package": "", "package_name": "", "import_source": "", "callee_package": "",
				"callee_symbol": "org.apache.commons.lang3.StringUtils#method().", "callee_name": "",
			}},
			"functions": []any{},
			"classes":   []any{},
		},
	}
	if !reflect.DeepEqual(got.Payload, wantPayload) {
		t.Fatalf("typed payload differs:\n got %#v\nwant %#v", got.Payload, wantPayload)
	}

	// The map path over the same bytes names the same used values: padded
	// strings trimmed, numbers rendered, nulls empty.
	decoded, err := payloadstore.UnmarshalPayload(raw)
	if err != nil {
		t.Fatal(err)
	}
	fileData, ok := decoded["parsed_file_data"].(map[string]any)
	if !ok {
		t.Fatal("map path parsed_file_data is not an object")
	}
	imports, ok := fileData["imports"].([]any)
	if !ok || len(imports) != 1 {
		t.Fatalf("map path imports = %#v, want one row", fileData["imports"])
	}
	first, ok := imports[0].(map[string]any)
	if !ok {
		t.Fatalf("map path import row = %T, want object", imports[0])
	}
	gotFileData, ok := got.Payload["parsed_file_data"].(map[string]any)
	if !ok {
		t.Fatal("typed parsed_file_data is not an object")
	}
	gotImports, ok := gotFileData["imports"].([]any)
	if !ok || len(gotImports) != 1 {
		t.Fatalf("typed imports = %#v, want one row", gotFileData["imports"])
	}
	gotFirst, ok := gotImports[0].(map[string]any)
	if !ok {
		t.Fatalf("typed import row = %T, want object", gotImports[0])
	}
	// Every used scalar renders like PayloadStr (fmt.Sprint plus TrimSpace)
	// on the map-decoded value.
	for _, key := range []string{"source", "name", "alias", "import_type", "lang", "full_import_name"} {
		rendered := strings.TrimSpace(fmt.Sprint(first[key]))
		if rendered == "<nil>" || first[key] == nil {
			rendered = ""
		}
		if got, want := gotFirst[key], rendered; got != want {
			t.Fatalf("import %s = %q, want map-rendered %q", key, got, want)
		}
	}
}
