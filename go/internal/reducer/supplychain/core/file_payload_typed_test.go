// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
)

// typedAdversarialRaw returns raw file-payload bytes exercising every
// tolerance clause the typed decode must replicate from payloadcore: padded
// strings, numbers/bools/nulls as scalars, mixed-type arrays with nulls,
// non-object parsed_file_data, bare-string decorators, and tombstones.
func typedAdversarialRaw(t *testing.T) ([][]byte, []typedFileTheoryMeta) {
	t.Helper()
	bodies := []string{
		// Padded strings everywhere the readers trim.
		`{"repo_id":"  repo-a  ","repository_id":"  repo-a  ","relative_path":"  src/a.ts  ","parsed_file_data":{"language":"  typescript  ","lang":"  typescript  ","imports":[{"source":"  react  ","name":"  useState  ","alias":"  useState  ","import_type":"  import  ","lang":"  typescript  ","full_import_name":"  react  "}],"function_calls":[{"full_name":"  useState.run  ","name":"run","lang":"typescript","inferred_obj_type":"  react  "}],"function_calls_scip":[{"package":"  npm react 1.0.0  ","package_name":"  react  ","import_source":"  react  ","callee_package":"  react  ","callee_symbol":"  scip-typescript npm react 1.0.0 x/y().  ","callee_name":"  react  "}],"functions":[{"decorators":["  a0.route  ",""]}],"classes":[{"decorators":["  a1.component  ","   "]}]}}`,
		// Numbers, bools, and nulls as scalars must render like PayloadStr.
		`{"repo_id":42,"repository_id":null,"relative_path":true,"parsed_file_data":{"language":7,"lang":false,"imports":[{"source":123,"name":true,"alias":null,"import_type":0,"lang":7,"full_import_name":1.5}],"function_calls":[{"full_name":3.5,"name":false,"lang":null,"inferred_obj_type":9}],"function_calls_scip":[{"package":1,"package_name":2,"import_source":3,"callee_package":4,"callee_symbol":5,"callee_name":6}],"functions":[{"decorators":[null,7,"  app.route  ","",false]}],"classes":[]}}`,
		// Mixed-type arrays with nulls: non-object items are skipped.
		`{"repo_id":"repo-b","relative_path":"pkg/f.py","parsed_file_data":{"language":"python","imports":[null,42,"bare",{"source":"requests","name":"requests","alias":"r","import_type":"import","lang":"python","full_import_name":"requests"}],"function_calls":[null,"x",{"full_name":"requests.get","name":"get","lang":"python","inferred_obj_type":"requests"}],"function_calls_scip":[null],"functions":[null,42,{"decorators":[null,7,"app.route"]}],"classes":[null]}}`,
		// Non-object parsed_file_data decodes as absent, never an error.
		`{"repo_id":"repo-c","relative_path":"src/c.java","parsed_file_data":"oops"}`,
		`{"repo_id":"repo-c","relative_path":"src/d.java","parsed_file_data":42}`,
		`{"repo_id":"repo-c","relative_path":"src/e.java","parsed_file_data":[1,2]}`,
		`{"repo_id":"repo-c","relative_path":"src/f.java","parsed_file_data":null}`,
		`{"repo_id":"repo-c","relative_path":"src/g.java"}`,
		// Bare-string decorators decode as absent, never an error.
		`{"repo_id":"repo-d","relative_path":"pkg/g.py","parsed_file_data":{"language":"python","imports":[],"function_calls":[],"functions":[{"decorators":"app.route"}],"classes":[{"decorators":42}]}}`,
		// JVM-shaped file with hostile scalar types.
		`{"repo_id":"repo-e","relative_path":"src/main/java/example/App.java","parsed_file_data":{"lang":true,"imports":[{"source":123,"name":456,"full_import_name":"org.apache.commons.lang3.StringUtils"}],"function_calls":[{"full_name":null,"inferred_obj_type":"org.apache.commons.lang3.StringUtils","name":7}],"function_calls_scip":[{"callee_symbol":"org.apache.commons.lang3.StringUtils#method().","callee_name":null}]}}`,
	}
	raw := make([][]byte, 0, len(bodies))
	meta := make([]typedFileTheoryMeta, 0, len(bodies))
	for i, body := range bodies {
		if !json.Valid([]byte(body)) {
			t.Fatalf("adversarial body %d is not valid JSON", i)
		}
		raw = append(raw, []byte(body))
		meta = append(meta, typedFileTheoryMeta{
			factID:    "adv-file",
			scopeID:   "scope-adv",
			tombstone: i%3 == 0,
		})
	}
	return raw, meta
}

func decodeTypedAdversarialEnvelopes(t *testing.T, raw [][]byte, meta []typedFileTheoryMeta) []facts.Envelope {
	t.Helper()
	out := make([]facts.Envelope, 0, len(raw))
	for i, data := range raw {
		typed, err := decodeTypedFilePayload(data)
		if err != nil {
			t.Fatalf("decodeTypedFilePayload(adversarial %d) error = %v, want nil (shape mismatch must not fail)", i, err)
		}
		out = append(out, projectTypedFileEnvelope(typed, meta[i].factID, meta[i].scopeID, meta[i].tombstone))
	}
	return out
}

func decodeMapAdversarialEnvelopes(t *testing.T, raw [][]byte, meta []typedFileTheoryMeta) []facts.Envelope {
	t.Helper()
	out := make([]facts.Envelope, 0, len(raw))
	for i, data := range raw {
		decoded, err := payloadstore.UnmarshalPayload(data)
		if err != nil {
			t.Fatalf("UnmarshalPayload(adversarial %d) error = %v", i, err)
		}
		out = append(out, facts.Envelope{
			FactID: meta[i].factID, FactKind: factload.FactKindFile, ScopeID: meta[i].scopeID,
			Payload: decoded, IsTombstone: meta[i].tombstone,
		})
	}
	return out
}

// TestTypedFilePayloadDecodeAdversarialParity proves the typed decode
// replicates payloadcore tolerance exactly: every hostile shape decodes
// without error and yields identical JS/TS, Python (including decorators),
// and JVM indexes plus an identical fold digest to the map decode.
func TestTypedFilePayloadDecodeAdversarialParity(t *testing.T) {
	t.Parallel()

	raw, meta := typedAdversarialRaw(t)
	want := decodeMapAdversarialEnvelopes(t, raw, meta)
	got := decodeTypedAdversarialEnvelopes(t, raw, meta)

	if gotJS, wantJS := buildJSTSPackageReachabilityIndex(got), buildJSTSPackageReachabilityIndex(want); !reflect.DeepEqual(gotJS, wantJS) {
		t.Fatalf("adversarial JS/TS index differs:\n got %#v\nwant %#v", gotJS, wantJS)
	}
	if gotPy, wantPy := extractPythonReachabilityEvidence(got), extractPythonReachabilityEvidence(want); !reflect.DeepEqual(gotPy, wantPy) {
		t.Fatalf("adversarial Python evidence differs:\n got %#v\nwant %#v", gotPy, wantPy)
	}
	if gotJVM, wantJVM := buildJVMReachabilityIndex(got), buildJVMReachabilityIndex(want); !reflect.DeepEqual(gotJVM, wantJVM) {
		t.Fatalf("adversarial JVM index differs:\n got %#v\nwant %#v", gotJVM, wantJVM)
	}

	mapFold, typedFold := NewFileReachabilityFold(nil), NewFileReachabilityFold(nil)
	for _, envelope := range want {
		mapFold.Add(envelope)
	}
	for _, envelope := range got {
		typedFold.Add(envelope)
	}
	mapDigest, mapOK := mapFold.DigestEnvelope()
	typedDigest, typedOK := typedFold.DigestEnvelope()
	if mapOK != typedOK {
		t.Fatalf("DigestEnvelope ok = %v map, %v typed", mapOK, typedOK)
	}
	if mapOK {
		mapJSTS, mapPython := foldReachabilityIndexes([]facts.Envelope{mapDigest})
		typedJSTS, typedPython := foldReachabilityIndexes([]facts.Envelope{typedDigest})
		if !reflect.DeepEqual(typedJSTS, mapJSTS) {
			t.Fatalf("adversarial folded JS/TS index differs")
		}
		if !reflect.DeepEqual(typedPython, mapPython) {
			t.Fatalf("adversarial folded Python evidence differs")
		}
	}
}

// TestTypedFilePayloadDecodeErrorContract proves decodeTypedFilePayload
// errors ONLY on malformed JSON, preserving UnmarshalPayload's contract: a
// shape mismatch must never dead-letter a generation.
func TestTypedFilePayloadDecodeErrorContract(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"repo_id":`,
		`not json at all`,
		`{"parsed_file_data":{"imports":[}}`,
	} {
		if _, err := decodeTypedFilePayload([]byte(raw)); err == nil {
			t.Fatalf("decodeTypedFilePayload(%q) error = nil, want malformed-JSON error", raw)
		}
		if _, err := payloadstore.UnmarshalPayload([]byte(raw)); err == nil {
			t.Fatalf("UnmarshalPayload(%q) error = nil, want malformed-JSON error (contract reference)", raw)
		}
	}

	for _, raw := range []string{
		``,
		`null`,
		`{}`,
		`{"repo_id":null,"parsed_file_data":null}`,
		`{"parsed_file_data":{"imports":"bare","function_calls":42,"functions":null,"classes":"x"}}`,
		`{"parsed_file_data":{"functions":[{"decorators":"bare"}]}}`,
		`{"parsed_file_data":[1,2,3]}`,
		`{"parsed_file_data":"oops"}`,
	} {
		if _, err := decodeTypedFilePayload([]byte(raw)); err != nil {
			t.Fatalf("decodeTypedFilePayload(%q) error = %v, want nil (shape mismatch must not fail)", raw, err)
		}
	}
}
