// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomod

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestGomodStateRoundTripThroughTypedContract proves the factschema
// codegraphv1.GomodState struct + DecodeParsedFileDataGomodState accessor
// faithfully model the Go module parser's real gomod_state output for BOTH
// producers (go.mod carries module_path, go.sum does not), recovering the
// state/module_path read-set the reducer joins on and preserving every other
// producer field in the open Attributes pass-through. It binds the typed
// contract to the producer without changing the emitted bytes (issue #4750 S1).
func TestGomodStateRoundTripThroughTypedContract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		filename       string
		source         string
		wantState      string
		wantModulePath string // "" means the accessor must report ModulePath == nil
	}{
		{
			name:     "go_mod_declares_module_path",
			filename: "go.mod",
			source: `module github.com/eshu-hq/eshu

go 1.23

require golang.org/x/mod v0.17.0
`,
			wantState:      "parsed",
			wantModulePath: "github.com/eshu-hq/eshu",
		},
		{
			name:     "go_sum_has_no_module_path",
			filename: "go.sum",
			source: `golang.org/x/mod v0.17.0 h1:aaa=
golang.org/x/mod v0.17.0/go.mod h1:bbb=
`,
			wantState:      "parsed",
			wantModulePath: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, tc.filename)
			if err := os.WriteFile(path, []byte(tc.source), 0o600); err != nil {
				t.Fatalf("write %s: %v", tc.filename, err)
			}

			payload, err := Parse(path, false, shared.Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			state, ok, err := factschema.DecodeParsedFileDataGomodState(payload)
			if err != nil {
				t.Fatalf("DecodeParsedFileDataGomodState() error = %v", err)
			}
			if !ok {
				t.Fatal("DecodeParsedFileDataGomodState() ok = false, want true for an emitted gomod_state")
			}
			if state.State != tc.wantState {
				t.Fatalf("State = %q, want %q", state.State, tc.wantState)
			}
			if tc.wantModulePath == "" {
				if state.ModulePath != nil {
					t.Fatalf("ModulePath = %q, want nil for %s", *state.ModulePath, tc.filename)
				}
			} else {
				if state.ModulePath == nil || *state.ModulePath != tc.wantModulePath {
					t.Fatalf("ModulePath = %v, want %q", state.ModulePath, tc.wantModulePath)
				}
			}

			// Every producer gomod_state key with no named field survives in
			// the open Attributes pass-through with its JSON-native value.
			rawState, _ := payload["gomod_state"].(map[string]any)
			for key, want := range rawState {
				if key == "state" || key == "module_path" {
					continue // named read-set fields
				}
				got, present := state.Attributes[key]
				if !present {
					t.Fatalf("Attributes missing producer gomod_state key %q", key)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("Attributes[%q] = %#v, want %#v", key, got, want)
				}
			}
		})
	}
}
