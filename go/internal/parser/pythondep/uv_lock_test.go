// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"strings"
	"testing"
)

const uvLockRealistic = `version = 1
revision = 2
requires-python = ">=3.12"
resolution-markers = [
    "python_full_version >= '3.13'",
    "python_full_version < '3.13'",
]

[manifest]
members = ["app"]

[[package]]
name = "anyio"
version = "4.4.0"
source = { registry = "https://pypi.org/simple" }
dependencies = [
    { name = "idna" },
    { name = "sniffio", marker = "python_full_version < '3.11'" },
]
sdist = { url = "https://files.pythonhosted.org/packages/anyio-4.4.0.tar.gz", hash = "sha256:aa", size = 1 }
wheels = [
    { url = "https://files.pythonhosted.org/packages/anyio-4.4.0-py3-none-any.whl", hash = "sha256:bb", size = 2 },
]

[package.optional-dependencies]
trio = [
    { name = "trio" },
]

[[package]]
name = "app"
version = "0.1.0"
source = { editable = "." }
dependencies = [
    { name = "anyio" },
]

[package.dev-dependencies]
dev = [
    { name = "pytest" },
]

[package.metadata]
requires-dist = [{ name = "anyio", specifier = ">=4" }]

[package.metadata.requires-dev]
dev = [{ name = "pytest", specifier = ">=8" }]

[[package]]
name = "pytest"
version = "8.3.2"
source = { registry = "https://pypi.org/simple" }
`

func rowByName(t *testing.T, rows []map[string]any, name string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["name"] == name {
			return row
		}
	}
	t.Fatalf("no row named %q in %#v", name, rows)
	return nil
}

// TestParseUVLockEmitsExactVersionRowsForPyPIPackages proves a realistic
// uv.lock yields one exact-version dependency row per [[package]] entry and
// that edge lists and sub-tables never become rows.
func TestParseUVLockEmitsExactVersionRowsForPyPIPackages(t *testing.T) {
	t.Parallel()

	payload, err := ParseUVLock(writeTempFile(t, "uv.lock", uvLockRealistic))
	if err != nil {
		t.Fatalf("ParseUVLock error = %v", err)
	}
	rows := variableRows(t, payload)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3 (anyio, app, pytest): %#v", len(rows), rows)
	}
	anyio := rowByName(t, rows, "anyio")
	want := map[string]any{
		"value":           "4.4.0",
		"section":         "package",
		"config_kind":     "dependency",
		"package_manager": "pypi",
		"lang":            "python_toml",
		"lockfile":        true,
		"source_kind":     "registry",
		"source_url":      "https://pypi.org/simple",
		"line_number":     12,
	}
	for key, value := range want {
		if anyio[key] != value {
			t.Fatalf("anyio[%q] = %#v, want %#v (row %#v)", key, anyio[key], value, anyio)
		}
	}
	if _, ok := anyio["dev_dependency"]; ok {
		t.Fatalf("anyio carries dev_dependency; uv.lock rows never prove dev scope: %#v", anyio)
	}
	if got := rowByName(t, rows, "pytest")["value"]; got != "8.3.2" {
		t.Fatalf("pytest value = %#v, want 8.3.2", got)
	}
	app := rowByName(t, rows, "app")
	if app["config_kind"] != "editable_dependency" || app["source_kind"] != "path" || app["source_url"] != "." {
		t.Fatalf("app must be a non-registry editable row: %#v", app)
	}
	state, _ := payload["uv_lock_state"].(map[string]any)
	if state["state"] != "complete" || state["entries"] != 3 || state["resolved"] != 3 || state["unresolved"] != 0 {
		t.Fatalf("uv_lock_state = %#v, want complete 3/3/0", state)
	}
}

// TestParseUVLockSourceKindsNeverMasqueradeAsRegistryConsumption pins the
// config_kind for every source form the grammar admits.
func TestParseUVLockSourceKindsNeverMasqueradeAsRegistryConsumption(t *testing.T) {
	t.Parallel()

	body := func(source string) string {
		return "version = 1\n\n[[package]]\nname = \"x\"\nversion = \"1.2.3\"\nsource = " + source + "\n"
	}
	cases := []struct {
		name       string
		source     string
		configKind string
		sourceKind string
		sourceURL  string
		sourceRef  string
		redacted   int
	}{
		{"pypi", `{ registry = "https://pypi.org/simple" }`, "dependency", "registry", "https://pypi.org/simple", "", 0},
		{"pypi trailing slash", `{ registry = "https://pypi.org/simple/" }`, "dependency", "registry", "https://pypi.org/simple/", "", 0},
		{"private index", `{ registry = "https://index.internal.example/simple" }`, "url_dependency", "registry", "https://index.internal.example/simple", "", 0},
		{"flat path index", `{ registry = "../wheels" }`, "url_dependency", "registry", "../wheels", "", 0},
		{"git with commit", `{ git = "https://github.com/acme/x.git?rev=main#0123456789abcdef0123456789abcdef01234567" }`, "vcs_dependency", "vcs", "https://github.com/acme/x.git", "0123456789abcdef0123456789abcdef01234567", 0},
		{"git credentials stripped", `{ git = "https://tok:en@github.com/acme/x.git#0123456789abcdef0123456789abcdef01234567" }`, "vcs_dependency", "vcs", "https://github.com/acme/x.git", "0123456789abcdef0123456789abcdef01234567", 1},
		{"git subdirectory", `{ git = "https://github.com/acme/x.git#0123456789abcdef0123456789abcdef01234567", subdirectory = "pkg" }`, "vcs_dependency", "vcs", "https://github.com/acme/x.git", "0123456789abcdef0123456789abcdef01234567", 0},
		{"path", `{ path = "../x" }`, "path_dependency", "path", "../x", "", 0},
		{"directory", `{ directory = "libs/x" }`, "path_dependency", "path", "libs/x", "", 0},
		{"virtual", `{ virtual = "." }`, "path_dependency", "path", ".", "", 0},
		{"editable", `{ editable = "../x" }`, "editable_dependency", "path", "../x", "", 0},
		{"url signed query dropped", `{ url = "https://example.com/x-1.2.3.tar.gz?token=SECRET#frag" }`, "url_dependency", "url", "https://example.com/x-1.2.3.tar.gz", "", 1},
		{"url userinfo and query", `{ url = "https://user:SECRET@example.com/x.tar.gz?sig=SECRET" }`, "url_dependency", "url", "https://example.com/x.tar.gz", "", 1},
		{"at sign in query is not userinfo", `{ url = "https://example.com/x.tar.gz?mail=a@b" }`, "url_dependency", "url", "https://example.com/x.tar.gz", "", 1},
		{"registry query dropped and not pypi", `{ registry = "https://pypi.org/simple?token=SECRET" }`, "url_dependency", "registry", "https://pypi.org/simple", "", 1},
		{"registry userinfo host is not pypi", `{ registry = "https://pypi.org@evil.example/simple" }`, "url_dependency", "registry", "https://evil.example/simple", "", 1},
		{"git query dropped", `{ git = "https://github.com/acme/x.git?rev=main&token=SECRET" }`, "vcs_dependency", "vcs", "https://github.com/acme/x.git", "main", 1},
		{"url", `{ url = "https://example.com/x-1.2.3.tar.gz" }`, "url_dependency", "url", "https://example.com/x-1.2.3.tar.gz", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := parseUVLockBytes([]byte(body(tc.source)))
			if len(result.Rows) != 1 {
				t.Fatalf("rows = %#v, want exactly 1", result.Rows)
			}
			row := result.Rows[0]
			if row["config_kind"] != tc.configKind || row["source_kind"] != tc.sourceKind || row["source_url"] != tc.sourceURL {
				t.Fatalf("row = %#v, want config_kind=%s source_kind=%s source_url=%s", row, tc.configKind, tc.sourceKind, tc.sourceURL)
			}
			if tc.sourceRef != "" && row["source_ref"] != tc.sourceRef {
				t.Fatalf("source_ref = %#v, want %q", row["source_ref"], tc.sourceRef)
			}
			if tc.sourceRef == "" {
				if _, ok := row["source_ref"]; ok {
					t.Fatalf("unexpected source_ref on %#v", row)
				}
			}
			if result.State.RedactedURLs != tc.redacted {
				t.Fatalf("RedactedURLs = %d, want %d", result.State.RedactedURLs, tc.redacted)
			}
			if strings.Contains(row["source_url"].(string), "tok") || strings.Contains(row["source_url"].(string), "SECRET") {
				t.Fatalf("credential leaked into source_url: %#v", row)
			}
		})
	}
}

// TestParseUVLockFailsClosedOnUnknownConstructs proves each construct outside
// the documented grammar yields a malformed row (or no row) and never a
// registry-admissible dependency row.
func TestParseUVLockFailsClosedOnUnknownConstructs(t *testing.T) {
	t.Parallel()

	pypi := `source = { registry = "https://pypi.org/simple" }`
	cases := []struct {
		name      string
		entry     string
		wantRows  int
		wantState int // unresolved count
	}{
		{"dotted source key", "name = \"x\"\nversion = \"1.0\"\nsource.registry = \"https://pypi.org/simple\"", 1, 1},
		{"dotted name key", "name.x = \"x\"\nversion = \"1.0\"\n" + pypi, 0, 1},
		{"duplicate version", "name = \"x\"\nversion = \"1.0\"\nversion = \"2.0\"\n" + pypi, 1, 1},
		{"duplicate name", "name = \"x\"\nname = \"y\"\nversion = \"1.0\"\n" + pypi, 0, 1},
		{"version with space", "name = \"x\"\nversion = \"1.0 beta\"\n" + pypi, 1, 1},
		{"version with escape", "name = \"x\"\nversion = \"1.0\\u0041\"\n" + pypi, 1, 1},
		{"version trailing junk", "name = \"x\"\nversion = \"1.0\" extra\n" + pypi, 1, 1},
		{"version literal string", "name = \"x\"\nversion = '1.0'\n" + pypi, 1, 1},
		{"version not a string", "name = \"x\"\nversion = 1.0\n" + pypi, 1, 1},
		{"version not digit-led", "name = \"x\"\nversion = \"latest\"\n" + pypi, 1, 1},
		{"registry without version", "name = \"x\"\n" + pypi, 1, 1},
		{"missing source", "name = \"x\"\nversion = \"1.0\"", 1, 1},
		{"unknown source key", "name = \"x\"\nversion = \"1.0\"\nsource = { mirror = \"https://pypi.org/simple\" }", 1, 1},
		{"two primary source keys", "name = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/simple\", git = \"https://h/x\" }", 1, 1},
		{"source not an inline table", "name = \"x\"\nversion = \"1.0\"\nsource = \"https://pypi.org/simple\"", 1, 1},
		{"source split over lines", "name = \"x\"\nversion = \"1.0\"\nsource = {\n  registry = \"https://pypi.org/simple\" }", 1, 1},
		{"source value with escape", "name = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/si\\u006dple\" }", 1, 1},
		{"source url with space", "name = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/ simple\" }", 1, 1},
		{"invalid name chars", "name = \"x y\"\nversion = \"1.0\"\n" + pypi, 0, 1},
		{"missing name", "version = \"1.0\"\n" + pypi, 0, 1},
		{"unterminated name string", "name = \"x\nversion = \"1.0\"\n" + pypi, 0, 1},
		{"quoted key", "\"name\" = \"x\"\nversion = \"1.0\"\n" + pypi, 0, 1},
		{"dotted extra key", "name = \"x\"\nversion = \"1.0\"\n" + pypi + "\nfoo.bar = \"z\"", 1, 1},
		{"path with url scheme", "name = \"x\"\nversion = \"1.0\"\nsource = { path = \"https://user:SECRET@host/x\" }", 1, 1},
		{"editable with url scheme", "name = \"x\"\nversion = \"1.0\"\nsource = { editable = \"file://host/x\" }", 1, 1},
		{"scp style git", "name = \"x\"\nversion = \"1.0\"\nsource = { git = \"deploy:SECRET@github.com:acme/x.git\" }", 1, 1},
		{"url without scheme", "name = \"x\"\nversion = \"1.0\"\nsource = { url = \"user:SECRET@example.com/x.tar.gz\" }", 1, 1},
		{"registry credential without scheme", "name = \"x\"\nversion = \"1.0\"\nsource = { registry = \"user:SECRET@host/simple\" }", 1, 1},
		{"raw hash inside password", "name = \"x\"\nversion = \"1.0\"\nsource = { git = \"https://user:tok#en@github.com/acme/x.git\" }", 1, 1},
		{"raw question mark inside password", "name = \"x\"\nversion = \"1.0\"\nsource = { url = \"https://user:SECRET?x@example.com/x.tar.gz\" }", 1, 1},
		{"query smuggles a pypi host", "name = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://evil.example?x@pypi.org/simple\" }", 1, 1},
		{"fragment smuggles a pypi host", "name = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://evil.example#x@pypi.org/simple\" }", 1, 1},
		{"scheme not at start", "name = \"x\"\nversion = \"1.0\"\nsource = { git = \"user:SECRET@host/x://y\" }", 1, 1},
		{"junk before scheme", "name = \"x\"\nversion = \"1.0\"\nsource = { url = \"u:SECRET@h/x https://y\" }", 1, 1},
		{"digit-led scheme", "name = \"x\"\nversion = \"1.0\"\nsource = { url = \"1http://example.com/x\" }", 1, 1},
		{"empty scheme", "name = \"x\"\nversion = \"1.0\"\nsource = { url = \"://example.com/x\" }", 1, 1},
		{"bare junk line", "name = \"x\"\nversion = \"1.0\"\n" + pypi + "\nthis is not toml", 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := parseUVLockBytes([]byte("version = 1\n\n[[package]]\n" + tc.entry + "\n"))
			if len(result.Rows) != tc.wantRows {
				t.Fatalf("rows = %#v, want %d", result.Rows, tc.wantRows)
			}
			for _, row := range result.Rows {
				if row["config_kind"] != "malformed_dependency" || row["malformed"] != true {
					t.Fatalf("row must be malformed: %#v", row)
				}
			}
			if result.State.Unresolved != tc.wantState || result.State.Resolved != 0 {
				t.Fatalf("state = %+v, want unresolved=%d resolved=0", result.State, tc.wantState)
			}
		})
	}
}

// TestParseUVLockIgnoresKeysInsideSubTablesAndArrays proves name/version
// look-alikes in edge lists or sub-tables cannot override the entry's own.
func TestParseUVLockIgnoresKeysInsideSubTablesAndArrays(t *testing.T) {
	t.Parallel()

	src := `version = 1

[[package]]
name = "real"
dependencies = [
    { name = "decoy", version = "9.9.9" },
]
version = "1.0.0"
source = { registry = "https://pypi.org/simple" }

[package.metadata]
name = "decoy2"
version = "8.8.8"
`
	result := parseUVLockBytes([]byte(src))
	if len(result.Rows) != 1 || result.Rows[0]["name"] != "real" || result.Rows[0]["value"] != "1.0.0" {
		t.Fatalf("rows = %#v, want only real 1.0.0", result.Rows)
	}
	if result.State.State() != "complete" {
		t.Fatalf("state = %+v, want complete", result.State)
	}
}

// TestParseUVLockUnterminatedArrayDoesNotSwallowLaterEntries proves a broken
// array marks only its own entry unresolved and the scanner resynchronises at
// the next [[package]] header.
func TestParseUVLockUnterminatedArrayDoesNotSwallowLaterEntries(t *testing.T) {
	t.Parallel()

	src := `version = 1

[[package]]
name = "broken"
version = "1.0"
source = { registry = "https://pypi.org/simple" }
dependencies = [
    { name = "a" },

[[package]]
name = "fine"
version = "2.0"
source = { registry = "https://pypi.org/simple" }
`
	result := parseUVLockBytes([]byte(src))
	if len(result.Rows) != 2 {
		t.Fatalf("rows = %#v, want 2", result.Rows)
	}
	if got := rowByName(t, result.Rows, "broken")["config_kind"]; got != "malformed_dependency" {
		t.Fatalf("broken config_kind = %#v, want malformed_dependency", got)
	}
	fine := rowByName(t, result.Rows, "fine")
	if fine["config_kind"] != "dependency" || fine["value"] != "2.0" {
		t.Fatalf("fine = %#v, want resolved 2.0", fine)
	}
	if result.State.Resolved != 1 || result.State.Unresolved != 1 || result.State.State() != "partial" {
		t.Fatalf("state = %+v, want 1 resolved 1 unresolved partial", result.State)
	}
}

// TestParseUVLockUnsupportedLockVersionEmitsNoRows proves a lock format other
// than version 1 is not read with the version-1 grammar.
func TestParseUVLockUnsupportedLockVersionEmitsNoRows(t *testing.T) {
	t.Parallel()

	entry := "\n[[package]]\nname = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n"
	for name, header := range map[string]string{
		"version 2":       "version = 2\n",
		"missing version": "",
		"string version":  "version = \"1\"\n",
		"float version":   "version = 1.0\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := parseUVLockBytes([]byte(header + entry + entry))
			if len(result.Rows) != 0 {
				t.Fatalf("rows = %#v, want none", result.Rows)
			}
			if !result.State.Unsupported || result.State.Entries != 2 || result.State.Unresolved != 2 || result.State.State() != "partial" {
				t.Fatalf("state = %+v, want unsupported with 2 unresolved entries", result.State)
			}
		})
	}
}

// TestParseUVLockTripleQuotedStringMakesWholeFilePartial proves a multi-line
// string, which the grammar cannot track line by line, voids every later row.
func TestParseUVLockTripleQuotedStringMakesWholeFilePartial(t *testing.T) {
	t.Parallel()

	src := "version = 1\n\n[[package]]\nname = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\nnote = \"\"\"\n[[package]]\nname = \"y\"\n\"\"\"\n"
	result := parseUVLockBytes([]byte(src))
	if len(result.Rows) != 0 || !result.State.Unsupported {
		t.Fatalf("rows=%#v state=%+v, want no rows and unsupported", result.Rows, result.State)
	}
}

// TestParseUVLockHandlesCRLFAndEmptyInput covers line-ending and empty-file
// edges.
func TestParseUVLockHandlesCRLFAndEmptyInput(t *testing.T) {
	t.Parallel()

	crlf := strings.ReplaceAll(uvLockRealistic, "\n", "\r\n")
	if got := len(parseUVLockBytes([]byte(crlf)).Rows); got != 3 {
		t.Fatalf("CRLF rows = %d, want 3", got)
	}
	for _, empty := range []string{"", "   \n\n", "# only a comment\n", "version = 1\n"} {
		result := parseUVLockBytes([]byte(empty))
		if len(result.Rows) != 0 || result.State.Entries != 0 || result.State.State() != "complete" {
			t.Fatalf("input %q: rows=%#v state=%+v, want empty complete", empty, result.Rows, result.State)
		}
	}
}

// TestParseUVLockDuplicateNamesKeepEveryForkVersion proves resolution forks
// that lock one package at two versions emit both rows.
func TestParseUVLockDuplicateNamesKeepEveryForkVersion(t *testing.T) {
	t.Parallel()

	entry := func(version string) string {
		return "\n[[package]]\nname = \"numpy\"\nversion = \"" + version + "\"\nsource = { registry = \"https://pypi.org/simple\" }\nresolution-markers = [\n    \"python_full_version < '3.10'\",\n]\n"
	}
	result := parseUVLockBytes([]byte("version = 1\n" + entry("1.26.4") + entry("2.1.0")))
	if len(result.Rows) != 2 || result.Rows[0]["value"] != "1.26.4" || result.Rows[1]["value"] != "2.1.0" {
		t.Fatalf("rows = %#v, want both numpy versions in file order", result.Rows)
	}
	if _, ok := result.Rows[0]["marker"]; ok {
		t.Fatalf("fork resolution-markers must not become a row marker: %#v", result.Rows[0])
	}
}

// TestParseUVLockRowCapCountsOverflow proves the row cap counts rather than
// silently drops entries.
func TestParseUVLockRowCapCountsOverflow(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("version = 1\n")
	total := uvLockMaxRows + 25
	for i := 0; i < total; i++ {
		b.WriteString("\n[[package]]\nname = \"p\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n")
	}
	result := parseUVLockBytes([]byte(b.String()))
	if len(result.Rows) != uvLockMaxRows || result.State.Overflow != 25 || result.State.Entries != total {
		t.Fatalf("rows=%d state=%+v, want %d rows overflow 25 entries %d", len(result.Rows), result.State, uvLockMaxRows, total)
	}
	if result.State.State() != "partial" {
		t.Fatalf("state = %q, want partial", result.State.State())
	}
}
