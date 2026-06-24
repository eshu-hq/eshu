// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomod

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseGoModEmitsRequireDependencyRows(t *testing.T) {
	t.Parallel()

	path := writeGoTestFile(t, "go.mod", `module example.com/app

go 1.22

require (
	golang.org/x/text v0.3.7
	golang.org/x/sys v0.10.0 // indirect
)
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := dependencyRowsByName(t, payload)

	text, ok := rows["golang.org/x/text"]
	if !ok {
		t.Fatalf("require row for golang.org/x/text missing in %#v", rows)
	}
	if got, want := text["config_kind"], "dependency"; got != want {
		t.Fatalf("config_kind = %#v, want %q", got, want)
	}
	if got, want := text["package_manager"], "go"; got != want {
		t.Fatalf("package_manager = %#v, want %q", got, want)
	}
	if got, want := text["section"], "require"; got != want {
		t.Fatalf("section = %#v, want %q", got, want)
	}
	if got, want := text["value"], "v0.3.7"; got != want {
		t.Fatalf("value = %#v, want %q for source-truth require version", got, want)
	}
	if got := text["direct_dependency"]; got != true {
		t.Fatalf("direct_dependency = %#v, want true for a require line without // indirect", got)
	}
	if got, want := text["lockfile"], false; got != want {
		t.Fatalf("lockfile = %#v, want %v for manifest evidence", got, want)
	}
	if got, want := text["dependency_path"], []string{"golang.org/x/text"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependency_path = %#v, want %#v", got, want)
	}
	if got, want := text["dependency_depth"], 1; got != want {
		t.Fatalf("dependency_depth = %#v, want %d", got, want)
	}

	sys, ok := rows["golang.org/x/sys"]
	if !ok {
		t.Fatalf("indirect require row for golang.org/x/sys missing")
	}
	if got, want := sys["section"], "require-indirect"; got != want {
		t.Fatalf("indirect section = %#v, want %q so indirect/direct evidence stays separate", got, want)
	}
	if got := sys["direct_dependency"]; got != false {
		t.Fatalf("indirect direct_dependency = %#v, want false", got)
	}
	if got := sys["indirect"]; got != true {
		t.Fatalf("indirect flag = %#v, want true so reducers can scope to direct dependencies", got)
	}
}

func TestParseGoModResolvesReplaceDirectiveToEffectiveVersion(t *testing.T) {
	t.Parallel()

	path := writeGoTestFile(t, "go.mod", `module example.com/app

go 1.22

require example.com/old v1.0.0

replace example.com/old => example.com/new v2.0.0
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	rows := dependencyRowsByName(t, payload)

	old, ok := rows["example.com/old"]
	if !ok {
		t.Fatalf("require row missing for replaced module")
	}
	if got, want := old["value"], "v1.0.0"; got != want {
		t.Fatalf("value = %#v, want %q for source-truth require version (replacement is separate)", got, want)
	}
	if got, want := old["replacement_path"], "example.com/new"; got != want {
		t.Fatalf("replacement_path = %#v, want %q", got, want)
	}
	if got, want := old["replacement_version"], "v2.0.0"; got != want {
		t.Fatalf("replacement_version = %#v, want %q", got, want)
	}
	if got, want := old["resolved_module_path"], "example.com/new"; got != want {
		t.Fatalf("resolved_module_path = %#v, want %q so vulnerability reducer matches the effective module", got, want)
	}
	if got, want := old["resolved_version"], "v2.0.0"; got != want {
		t.Fatalf("resolved_version = %#v, want %q so advisory match uses the version the resolver wrote", got, want)
	}

	standalone, ok := standaloneSectionRow(payload, "replace", "example.com/old")
	if !ok {
		t.Fatalf("standalone replace row missing for example.com/old")
	}
	if got, want := standalone["config_kind"], "dependency_replace"; got != want {
		t.Fatalf("replace config_kind = %#v, want %q so consumption reducer does not admit replace directives as consumption", got, want)
	}
	if got, want := standalone["target_module"], "example.com/new"; got != want {
		t.Fatalf("replace target_module = %#v, want %q", got, want)
	}
	if got, want := standalone["target_version"], "v2.0.0"; got != want {
		t.Fatalf("replace target_version = %#v, want %q", got, want)
	}
}

func TestParseGoModResolvesLocalPathReplaceWithoutInventingVersion(t *testing.T) {
	t.Parallel()

	path := writeGoTestFile(t, "go.mod", `module example.com/app

go 1.22

require example.com/local v1.0.0

replace example.com/local => ../local
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	rows := dependencyRowsByName(t, payload)

	row, ok := rows["example.com/local"]
	if !ok {
		t.Fatalf("require row missing for locally-replaced module")
	}
	if got, want := row["replacement_path"], "../local"; got != want {
		t.Fatalf("replacement_path = %#v, want %q so local-replace state stays explicit", got, want)
	}
	if got, want := row["replacement_version"], ""; got != want {
		t.Fatalf("replacement_version = %#v, want %q so local replace does not invent a version", got, want)
	}
	if got, want := row["resolved_module_path"], "example.com/local"; got != want {
		t.Fatalf("resolved_module_path = %#v, want %q so missing version falls back to source identity instead of an invented coordinate", got, want)
	}
	if got, want := row["resolved_version"], "v1.0.0"; got != want {
		t.Fatalf("resolved_version = %#v, want %q so reducer does not silently lose the source-truth version", got, want)
	}
}

func TestParseGoModRecordsExcludeAsNonConsumptionRow(t *testing.T) {
	t.Parallel()

	path := writeGoTestFile(t, "go.mod", `module example.com/app

go 1.22

exclude example.com/bad v1.2.3

require example.com/keeper v1.0.0
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	row, ok := standaloneSectionRow(payload, "exclude", "example.com/bad")
	if !ok {
		t.Fatalf("exclude row missing")
	}
	if got, want := row["config_kind"], "dependency_exclude"; got != want {
		t.Fatalf("config_kind = %#v, want %q so exclude is not admitted as consumption", got, want)
	}
	if got, want := row["value"], "v1.2.3"; got != want {
		t.Fatalf("value = %#v, want %q so the excluded version stays auditable", got, want)
	}

	rows := dependencyRowsByName(t, payload)
	if _, ok := rows["example.com/bad"]; ok {
		t.Fatalf("exclude row was admitted as a config_kind=dependency row; excludes must never be admitted as consumption")
	}
}

func TestParseGoModRecordsMalformedStateExplicitly(t *testing.T) {
	t.Parallel()

	path := writeGoTestFile(t, "go.mod", `not a valid go.mod`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil; malformed go.mod must not break the parse pipeline", err)
	}

	rows, _ := payload["variables"].([]map[string]any)
	for _, row := range rows {
		if row["config_kind"] == "dependency" {
			t.Fatalf("malformed go.mod emitted a config_kind=dependency row %#v; gap-grade input must not turn into consumption truth", row)
		}
	}

	state, _ := payload["gomod_state"].(map[string]any)
	if state == nil {
		t.Fatalf("payload[gomod_state] missing; malformed go.mod must surface a state envelope so operators can diagnose")
	}
	if got, want := state["state"], "malformed"; got != want {
		t.Fatalf("gomod_state.state = %#v, want %q so missing/ambiguous module evidence stays missing", got, want)
	}
	if parseError, _ := state["parse_error"].(string); parseError == "" {
		t.Fatalf("gomod_state.parse_error empty; the upstream modfile error must be preserved for diagnosis")
	}
}

func TestParseGoSumEmitsAmbiguousChecksumRowsAndNoConsumption(t *testing.T) {
	t.Parallel()

	path := writeGoTestFile(t, "go.sum", `golang.org/x/text v0.3.7 h1:olpwvP2KacW1ZWvsR7uQhoyTYvKAupfQrRGBFM352Gk=
golang.org/x/text v0.3.7/go.mod h1:5Zf9MlPGSHRzGAY0xqgNYbsmkNibR7P++ZRPSqVbA0Q=
golang.org/x/sys v0.10.0 h1:SqB5JfTtaUKvLLOZpd3KO6tjg2Ws9Vc4VYx7g2OdN90=
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, _ := payload["variables"].([]map[string]any)
	if len(rows) == 0 {
		t.Fatalf("go.sum produced no variable rows; expected checksum-only evidence rows")
	}
	for _, row := range rows {
		if row["config_kind"] == "dependency" {
			t.Fatalf("go.sum emitted a config_kind=dependency row %#v; checksum-only evidence must never be admitted as consumption truth", row)
		}
		if row["config_kind"] != "dependency_checksum" {
			t.Fatalf("unexpected config_kind in go.sum row %#v", row)
		}
		if row["ambiguous"] != true {
			t.Fatalf("ambiguous = %#v in row %#v, want true so checksum-only ambiguity stays explicit", row["ambiguous"], row)
		}
		if row["package_manager"] != "go" {
			t.Fatalf("package_manager = %#v, want %q so reducer wiring stays consistent", row["package_manager"], "go")
		}
		if row["lockfile"] != true {
			t.Fatalf("lockfile = %#v, want true so the row is recognized as checksum corroboration", row["lockfile"])
		}
		if _, ok := row["checksum"].(string); !ok {
			t.Fatalf("checksum missing on row %#v; verbatim h1 hash must be preserved", row)
		}
		kind, _ := row["checksum_kind"].(string)
		if kind != "module" && kind != "gomod" {
			t.Fatalf("checksum_kind = %q, want \"module\" or \"gomod\"", kind)
		}
	}
}

// TestParseGoSumSurfacesScannerErrorAsMalformedState guards against the
// silent-truncation failure mode Copilot called out: bufio.Scanner can hit
// an error (for example, a line longer than its buffer) and stop scanning
// without panicking. If we ignored scanner.Err() the payload would still
// claim state=parsed and a partial set of rows, masking missing-evidence
// risk. This test forces a scanner.ErrTooLong by writing a single line that
// exceeds the 1 MiB scan buffer and asserts the state envelope flips to
// malformed with a populated parse_error.
func TestParseGoSumSurfacesScannerErrorAsMalformedState(t *testing.T) {
	t.Parallel()

	// One line longer than the scanner's 1 MiB max-token buffer.
	hugeHash := strings.Repeat("a", 2*1024*1024)
	body := "golang.org/x/text v0.3.7 h1:" + hugeHash + "\n"

	path := writeGoTestFile(t, "go.sum", body)
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil; malformed go.sum must not break the parse pipeline", err)
	}

	state, _ := payload["gomod_state"].(map[string]any)
	if state == nil {
		t.Fatalf("payload[gomod_state] missing; truncated go.sum must surface a state envelope")
	}
	if got, want := state["state"], "malformed"; got != want {
		t.Fatalf("gomod_state.state = %#v, want %q; scanner errors must flip state to malformed so readiness treats the file as missing/ambiguous", got, want)
	}
	if parseError, _ := state["parse_error"].(string); parseError == "" {
		t.Fatalf("gomod_state.parse_error empty; scanner.Err() must be preserved for diagnosis")
	}
}

func TestParseRejectsUnknownFile(t *testing.T) {
	t.Parallel()

	path := writeGoTestFile(t, "go.work", `go 1.22

use ./module
`)

	_, err := Parse(path, false, shared.Options{})
	if err == nil {
		t.Fatalf("Parse() error = nil, want error for unsupported go-module file")
	}
}

func writeGoTestFile(t *testing.T, name string, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}
	return path
}

func dependencyRowsByName(t *testing.T, payload map[string]any) map[string]map[string]any {
	t.Helper()

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[variables] = %T, want []map[string]any", payload["variables"])
	}
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		if row["config_kind"] != "dependency" {
			continue
		}
		name, _ := row["name"].(string)
		if name != "" {
			out[name] = row
		}
	}
	return out
}

func standaloneSectionRow(payload map[string]any, section string, name string) (map[string]any, bool) {
	rows, _ := payload["variables"].([]map[string]any)
	for _, row := range rows {
		if rowName, _ := row["name"].(string); rowName != name {
			continue
		}
		if rowSection, _ := row["section"].(string); rowSection != section {
			continue
		}
		return row, true
	}
	return nil, false
}
