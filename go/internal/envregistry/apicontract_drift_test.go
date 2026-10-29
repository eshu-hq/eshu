// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// apicontractClientNames is the published thin-client variable table: the
// ESHU_* names a thin CLI reads or writes outside local-owner paths. The SDK
// module sdk/go/apicontract ships field-identical copies as ClientEntries.
// Adding a name here requires adding it there (and to the committed
// env_client_entries.json golden) in the same change.
var apicontractClientNames = []string{
	"ESHU_API_KEY",
	"ESHU_HOME",
	"ESHU_QUERY_PROFILE",
	"ESHU_GRAPH_BACKEND",
	"ESHU_NEO4J_DATABASE",
	"ESHU_MCP_ALLOW_UNAUTHENTICATED",
	"ESHU_MCP_RESULT_SHAPE",
	"ESHU_MCP_TOKEN",
	"ESHU_MCP_TOOL_SURFACE",
	"ESHU_MCP_TRANSPORT",
}

// apicontractGoldenDirRel is the golden directory relative to this test
// file's directory (a path-named constant so the lockstep discovery gate
// resolves the caller-relative read target).
const apicontractGoldenDirRel = "../../../sdk/go/apicontract/testdata/golden"

// apicontractGoldenFiles maps each committed golden to its purpose. Golden
// names live here as map values — never as literals at a read site — so the
// lockstep discovery gate classifies the reads through this table.
var apicontractGoldenFiles = map[string]string{
	"entries": "env_client_entries.json",
}

func apicontractGoldenDir(t *testing.T) string {
	t.Helper()
	_, callerFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Join(filepath.Dir(callerFile), apicontractGoldenDirRel)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("golden dir %s: %v", dir, err)
	}
	return dir
}

func apicontractClientSubset(t *testing.T) []Entry {
	t.Helper()
	byName := map[string]Entry{}
	for _, e := range Default().Entries() {
		byName[e.Name] = e
	}
	var subset []Entry
	for _, name := range apicontractClientNames {
		e, ok := byName[name]
		if !ok {
			t.Fatalf("client name %s no longer registered in envregistry", name)
		}
		subset = append(subset, e)
	}
	return subset
}

// TestApicontractEntriesMatchRegistry locks the published client table to the
// live registry: every published name must resolve to a field-identical
// Entry. A registry rename, retype, or description change fails here until
// the SDK table and golden move in the same change.
func TestApicontractEntriesMatchRegistry(t *testing.T) {
	t.Parallel()
	subset := apicontractClientSubset(t)
	raw, err := json.MarshalIndent(subset, "", "  ")
	if err != nil {
		t.Fatalf("marshal subset: %v", err)
	}
	raw = append(raw, '\n')
	want, err := os.ReadFile(filepath.Join(apicontractGoldenDir(t), apicontractGoldenFiles["entries"]))
	if err != nil {
		t.Fatalf("read entries golden: %v", err)
	}
	if string(raw) != string(want) {
		t.Fatalf("registry subset drifted from published golden:\n got: %s\nwant: %s", raw, want)
	}
	clientReg, err := New(subset)
	if err != nil {
		t.Fatalf("New(subset): %v", err)
	}
	if got := clientReg.Entries(); !reflect.DeepEqual(got, subset) {
		t.Fatalf("client subset does not round-trip through New().Entries()")
	}
}

// TestApicontractValidateVectorsMatchClientScope locks the ported SDK
// Validate behavior to the live implementation on the published scope: the
// same inputs through a client-scoped live registry must yield the committed
// expectation goldens the SDK tests check against.
func TestApicontractValidateVectorsMatchClientScope(t *testing.T) {
	t.Parallel()
	clientReg, err := New(apicontractClientSubset(t))
	if err != nil {
		t.Fatalf("New(subset): %v", err)
	}
	vectors := map[string]struct {
		env    map[string]string
		strict bool
	}{
		"validate_client_valid_client_env.json": {
			env: map[string]string{
				"ESHU_API_KEY":          "redacted",
				"ESHU_QUERY_PROFILE":    "production",
				"ESHU_MCP_TRANSPORT":    "stdio",
				"ESHU_GRAPH_BACKEND":    "nornicdb",
				"ESHU_HOME":             "/tmp/x",
				"ESHU_UNRELATED_CUSTOM": "kept-quiet",
			},
		},
		"validate_client_invalid_and_typo.json": {
			env: map[string]string{
				"ESHU_QUERY_PROFILE": "nonsense",
				"ESHU_MCP_TRANSPOR":  "stdio",
				"ESHU_API_KEY":       "redacted",
			},
		},
		"validate_client_strict_unknown.json": {
			env: map[string]string{
				"ESHU_API_KEY":        "redacted",
				"ESHU_CUSTOM_THING_X": "1",
			},
			strict: true,
		},
	}
	for name, vec := range vectors {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			findings := clientReg.Validate(vec.env, vec.strict)
			if findings == nil {
				findings = []Finding{}
			}
			raw, err := json.MarshalIndent(findings, "", "  ")
			if err != nil {
				t.Fatalf("marshal findings: %v", err)
			}
			raw = append(raw, '\n')
			want, err := os.ReadFile(filepath.Join(apicontractGoldenDir(t), name))
			if err != nil {
				t.Fatalf("read golden %s: %v", name, err)
			}
			if string(raw) != string(want) {
				t.Fatalf("live Validate drifted from golden %s:\n got: %s\nwant: %s", name, raw, want)
			}
		})
	}
}
