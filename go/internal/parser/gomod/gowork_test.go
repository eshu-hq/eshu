// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomod

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestParseGoWorkListsMembersReplacesAndGoVersion is the positive go.work
// fixture (#8823): a use block, a single-line use, a module-version replace,
// a local-path replace, and comments. Every use path is kept verbatim, every
// replace reuses the go.mod dependency_replace row shape, and gomod_state
// carries the go directive and toolchain.
func TestParseGoWorkListsMembersReplacesAndGoVersion(t *testing.T) {
	t.Parallel()

	path := writeGoTestFile(t, "go.work", `// Workspace for the example services.
go 1.23.0

toolchain go1.23.4

use (
	./api // the HTTP service
	./worker
	../shared
)

use ./tools

replace golang.org/x/text v0.3.7 => golang.org/x/text v0.14.0

replace example.com/lib => ./third_party/lib
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if got, want := payload["lang"], LanguageName; got != want {
		t.Fatalf("lang = %#v, want %q", got, want)
	}

	rows := variableRows(t, payload)
	wantUses := []struct {
		path string
		line int
	}{{"./api", 7}, {"./worker", 8}, {"../shared", 9}, {"./tools", 12}}
	uses := rowsWithSection(rows, "use")
	if len(uses) != len(wantUses) {
		t.Fatalf("use rows = %d (%#v), want %d", len(uses), uses, len(wantUses))
	}
	for i, want := range wantUses {
		row := uses[i]
		if row["name"] != want.path || row["value"] != want.path {
			t.Fatalf("use row %d name/value = %#v/%#v, want %q verbatim", i, row["name"], row["value"], want.path)
		}
		if got := row["line_number"]; got != want.line {
			t.Fatalf("use row %q line_number = %#v, want %d", want.path, got, want.line)
		}
		if got := row["config_kind"]; got != "workspace_use" {
			t.Fatalf("use row %q config_kind = %#v, want workspace_use", want.path, got)
		}
		if row["package_manager"] != PackageManager || row["lang"] != LanguageName || row["lockfile"] != false {
			t.Fatalf("use row %q common fields = %#v", want.path, row)
		}
	}

	versioned, ok := standaloneSectionRow(payload, "replace", "golang.org/x/text")
	if !ok {
		t.Fatalf("versioned replace row missing in %#v", rows)
	}
	if versioned["config_kind"] != "dependency_replace" || versioned["value"] != "v0.3.7" ||
		versioned["target_module"] != "golang.org/x/text" || versioned["target_version"] != "v0.14.0" ||
		versioned["replace_from_version"] != "v0.3.7" {
		t.Fatalf("versioned replace row = %#v", versioned)
	}
	local, ok := standaloneSectionRow(payload, "replace", "example.com/lib")
	if !ok {
		t.Fatalf("local-path replace row missing in %#v", rows)
	}
	if local["config_kind"] != "dependency_replace" || local["target_module"] != "./third_party/lib" ||
		local["target_version"] != "" {
		t.Fatalf("local-path replace row = %#v, want target_module ./third_party/lib with no invented version", local)
	}
	if _, has := local["replace_from_version"]; has {
		t.Fatalf("local-path replace row carries replace_from_version %#v for a path-wide replace", local["replace_from_version"])
	}

	for _, row := range rows {
		if row["config_kind"] == "dependency" {
			t.Fatalf("go.work emitted a consumption row %#v; a workspace declares no requirements", row)
		}
	}

	state := gomodState(t, payload)
	want := map[string]any{
		"state":            "parsed",
		"go_version":       "1.23.0",
		"toolchain":        "go1.23.4",
		"use_count":        4,
		"replace_count":    2,
		"replaced_modules": []string{"example.com/lib", "golang.org/x/text"},
	}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("gomod_state = %#v, want %#v", state, want)
	}
}

// TestParseGoWorkEdgeShapes covers the go.work shapes that carry no members
// or that a hostile checkout could hand the parser: a toolchain-only file,
// CRLF line endings, an empty file, and malformed lines. None may panic or
// return an error; malformed input follows the go.mod convention of zero
// rows plus a malformed gomod_state with the upstream parse error.
func TestParseGoWorkEdgeShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		source        string
		wantState     string
		wantGoVersion string
		wantUses      []string
		wantErrorPart string
	}{
		{
			name:          "go_and_toolchain_only",
			source:        "go 1.22\ntoolchain go1.22.5\n",
			wantState:     "parsed",
			wantGoVersion: "1.22",
			wantUses:      []string{},
		},
		{
			name:          "crlf_line_endings",
			source:        "go 1.21\r\n\r\nuse (\r\n\t./a\r\n\t./b\r\n)\r\nuse ./c\r\n",
			wantState:     "parsed",
			wantGoVersion: "1.21",
			wantUses:      []string{"./a", "./b", "./c"},
		},
		{
			name:      "empty_file",
			source:    "",
			wantState: "parsed",
			wantUses:  []string{},
		},
		{
			name:          "godebug_line_and_block_are_accepted",
			source:        "go 1.23\ngodebug default=go1.21\ngodebug (\n\tpanicnil=1\n)\nuse ./a\n",
			wantState:     "parsed",
			wantGoVersion: "1.23",
			wantUses:      []string{"./a"},
		},
		{
			name:          "require_is_not_a_workspace_directive",
			source:        "go 1.22\nrequire example.com/x v1.0.0\nuse ./a\n",
			wantState:     "malformed",
			wantErrorPart: "unknown directive: require",
		},
		{
			name:          "unknown_verb",
			source:        "go 1.22\nfoo bar\nuse ./a\n",
			wantState:     "malformed",
			wantErrorPart: "unknown directive: foo",
		},
		{
			name:          "ignore_is_not_a_workspace_directive",
			source:        "go 1.22\nignore ./a\nuse ./a\n",
			wantState:     "malformed",
			wantErrorPart: "unknown directive: ignore",
		},
		{
			name:          "use_with_too_many_arguments",
			source:        "go 1.22\nuse ./a ./b\n",
			wantState:     "malformed",
			wantErrorPart: "usage: use local/dir",
		},
		{
			name:          "unterminated_use_block",
			source:        "go 1.22\nuse (\n\t./a\n",
			wantState:     "malformed",
			wantErrorPart: "go.work",
		},
		{
			name:          "invalid_go_version",
			source:        "go one.two\nuse ./a\n",
			wantState:     "malformed",
			wantErrorPart: "invalid go version",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeGoTestFile(t, "go.work", tc.source)
			payload, err := Parse(path, false, shared.Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}
			state := gomodState(t, payload)
			if got := state["state"]; got != tc.wantState {
				t.Fatalf("gomod_state.state = %#v, want %q (state %#v)", got, tc.wantState, state)
			}
			rows := variableRows(t, payload)
			if tc.wantState == "malformed" {
				if len(rows) != 0 {
					t.Fatalf("malformed go.work emitted rows %#v, want none", rows)
				}
				parseError, _ := state["parse_error"].(string)
				if !strings.Contains(parseError, tc.wantErrorPart) {
					t.Fatalf("parse_error = %q, want it to contain %q", parseError, tc.wantErrorPart)
				}
				return
			}
			if got := state["go_version"]; got != tc.wantGoVersion {
				t.Fatalf("go_version = %#v, want %q", got, tc.wantGoVersion)
			}
			assertGoWorkStateKeys(t, state)
			got := make([]string, 0, len(rows))
			for _, row := range rowsWithSection(rows, "use") {
				got = append(got, row["name"].(string))
			}
			if !reflect.DeepEqual(got, tc.wantUses) {
				t.Fatalf("use rows = %#v, want %#v", got, tc.wantUses)
			}
		})
	}
}

// TestParseGoWorkKeepsUsePathsVerbatim pins the verbatim use-path contract
// (review F1 on #8823). ParseWork unquotes a use path but keeps its spaces,
// and the go command joins it to the workspace directory, so `use ""` names
// the workspace root. Every use entry becomes a row and counts in use_count.
// value is always the raw path. name is the raw path too, except when the
// path is blank after trimming: the content writers reject a blank entity
// name (storage/postgres content_writer.go and content_store_writes.go), so
// a blank path is named by its Go-quoted spelling, the way the file writes it.
func TestParseGoWorkKeepsUsePathsVerbatim(t *testing.T) {
	t.Parallel()

	path := writeGoTestFile(t, "go.work", `go 1.23

use ""
use " ./a "
use "./with space"
use "  "
use ./b
`)
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []struct {
		name  string
		value string
		line  int
	}{
		{`""`, "", 3},
		{" ./a ", " ./a ", 4},
		{"./with space", "./with space", 5},
		{`"  "`, "  ", 6},
		{"./b", "./b", 7},
	}
	uses := rowsWithSection(variableRows(t, payload), "use")
	if len(uses) != len(want) {
		t.Fatalf("use rows = %#v, want %d rows", uses, len(want))
	}
	for i, w := range want {
		row := uses[i]
		if row["name"] != w.name || row["value"] != w.value || row["line_number"] != w.line {
			t.Fatalf("use row %d = name %#v value %#v line %#v, want %#v %#v %d",
				i, row["name"], row["value"], row["line_number"], w.name, w.value, w.line)
		}
		if name, _ := row["name"].(string); strings.TrimSpace(name) == "" {
			t.Fatalf("use row %d has a blank name %#v; the content writers reject blank entity names", i, name)
		}
	}
	if got := gomodState(t, payload)["use_count"]; got != len(want) {
		t.Fatalf("use_count = %#v, want %d", got, len(want))
	}
}

// TestGoWorkStateHasNoModulePath is the negative contract with the code-call
// reducer: a go.work is not a module declaration, so its gomod_state must not
// carry module_path. The reducer's goModuleDeclaredPath reads module_path from
// every lang=gomod file to build module roots; a workspace root must never be
// mistaken for one.
func TestGoWorkStateHasNoModulePath(t *testing.T) {
	t.Parallel()

	path := writeGoTestFile(t, "go.work", "go 1.23\n\nuse ./svc\n")
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	decoded, ok, err := factschema.DecodeParsedFileDataGomodState(payload)
	if err != nil || !ok {
		t.Fatalf("DecodeParsedFileDataGomodState() ok=%v err=%v, want a decodable gomod_state", ok, err)
	}
	if decoded.ModulePath != nil {
		t.Fatalf("ModulePath = %q, want nil for go.work", *decoded.ModulePath)
	}
	if got := decoded.Attributes["use_count"]; got != 1 {
		t.Fatalf("Attributes[use_count] = %#v, want 1", got)
	}
	for _, row := range variableRows(t, payload) {
		if row["config_kind"] == "module_declaration" {
			t.Fatalf("go.work emitted a module_declaration row %#v", row)
		}
	}
}

// assertGoWorkStateKeys pins the parsed go.work gomod_state key set. godebug
// directives are accepted by ParseWork but not recorded, matching go.mod.
func assertGoWorkStateKeys(t *testing.T, state map[string]any) {
	t.Helper()
	want := []string{"go_version", "replace_count", "replaced_modules", "state", "toolchain", "use_count"}
	got := make([]string, 0, len(state))
	for key := range state {
		got = append(got, key)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gomod_state keys = %v, want %v", got, want)
	}
}

func variableRows(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	return rows
}

func rowsWithSection(rows []map[string]any, section string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if row["section"] == section {
			out = append(out, row)
		}
	}
	return out
}

func gomodState(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	state, ok := payload["gomod_state"].(map[string]any)
	if !ok {
		t.Fatalf("gomod_state = %#v, want a map", payload["gomod_state"])
	}
	return state
}
