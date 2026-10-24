// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"strings"
	"testing"
)

const uvLockWorkspace = `version = 1
revision = 3
requires-python = ">=3.12"

[manifest]
members = [
    "app",
    "My_Lib",
    "tools",
]

[[package]]
name = "anyio"
version = "4.4.0"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "app"
version = "0.1.0"
source = { editable = "." }
dependencies = [
    { name = "anyio" },
    { name = "my-lib" },
]

[[package]]
name = "my-lib"
version = "0.2.0"
source = { editable = "packages/lib" }

[[package]]
name = "tools"
source = { virtual = "tools" }

[[package]]
name = "vendored"
version = "1.0.0"
source = { path = "../vendored" }
`

// TestParseUVLockCountsRootAndWorkspaceMembersInsteadOfEmittingRows proves the
// project itself and the [manifest] members are counted, not emitted: as rows
// they would read as unsupported local dependencies in supply-chain readiness
// although they are the repository's own code. A genuine local path dependency
// that is not a member stays an honest path_dependency row.
func TestParseUVLockCountsRootAndWorkspaceMembersInsteadOfEmittingRows(t *testing.T) {
	t.Parallel()

	result := parseUVLockBytes([]byte(uvLockWorkspace))
	names := map[string]string{}
	for _, row := range result.Rows {
		names[row["name"].(string)], _ = row["config_kind"].(string)
	}
	want := map[string]string{"anyio": "dependency", "vendored": "path_dependency"}
	if len(names) != len(want) || names["anyio"] != want["anyio"] || names["vendored"] != want["vendored"] {
		t.Fatalf("rows = %#v, want only %#v (root and members are counted, not emitted)", names, want)
	}
	s := result.State
	if s.Entries != 5 || s.Resolved != 2 || s.RootPackages != 1 || s.WorkspaceMembers != 2 || s.Unresolved != 0 || s.State() != "complete" {
		t.Fatalf("state = %+v, want 5 entries, 2 resolved, 1 root, 2 members, complete", s)
	}
}

// TestParseUVLockWorkspaceOmissionFailsClosed pins the cases where an editable
// or virtual entry must stay a row: no members list, a second "." root, a
// member listed after the packages, an unreadable members array, and a member
// whose source is not editable or virtual.
func TestParseUVLockWorkspaceOmissionFailsClosed(t *testing.T) {
	t.Parallel()

	entry := func(name, source string) string {
		return "\n[[package]]\nname = \"" + name + "\"\nversion = \"1.0\"\nsource = " + source + "\n"
	}
	cases := []struct {
		name     string
		src      string
		rows     []string
		root     int
		members  int
		complete bool
	}{
		{"virtual root without manifest", "version = 1\n" + entry("proj", `{ virtual = "." }`) + entry("idna", `{ registry = "https://pypi.org/simple" }`), []string{"idna"}, 1, 0, true},
		{"second root stays a row", "version = 1\n" + entry("a", `{ editable = "." }`) + entry("b", `{ editable = "." }`), []string{"b"}, 1, 0, true},
		{"editable outside members stays a row", "version = 1\n[manifest]\nmembers = [\"a\"]\n" + entry("other", `{ editable = "libs/other" }`), []string{"other"}, 0, 0, true},
		{"manifest after packages is not applied", "version = 1\n" + entry("a", `{ registry = "https://pypi.org/simple" }`) + "\n[manifest]\nmembers = [\"lib\"]\n" + entry("lib", `{ editable = "libs/lib" }`), []string{"a", "lib"}, 0, 0, true},
		{"second manifest table is not applied", "version = 1\n[manifest]\nmembers = [\"lib\"]\n[manifest]\n" + entry("lib", `{ editable = "libs/lib" }`), []string{"lib"}, 0, 0, true},
		{"unreadable members array is not applied", "version = 1\n[manifest]\nmembers = [\"lib\", 1]\n" + entry("lib", `{ editable = "libs/lib" }`), []string{"lib"}, 0, 0, true},
		{"escaped member name is not applied", "version = 1\n[manifest]\nmembers = [\"l\\u0069b\"]\n" + entry("lib", `{ editable = "libs/lib" }`), []string{"lib"}, 0, 0, true},
		{"duplicate members key is not applied", "version = 1\n[manifest]\nmembers = [\"lib\"]\nmembers = [\"lib\"]\n" + entry("lib", `{ editable = "libs/lib" }`), []string{"lib"}, 0, 0, true},
		{"unterminated members array is not applied", "version = 1\n[manifest]\nmembers = [\n    \"lib\",\n" + entry("lib", `{ editable = "libs/lib" }`), []string{"lib"}, 0, 0, false},
		{"missing comma between members", "version = 1\n[manifest]\nmembers = [\"a\" \"lib\"]\n" + entry("lib", `{ editable = "libs/lib" }`), []string{"lib"}, 0, 0, true},
		{"missing comma across member lines", "version = 1\n[manifest]\nmembers = [\n    \"a\"\n    \"lib\",\n]\n" + entry("lib", `{ editable = "libs/lib" }`), []string{"lib"}, 0, 0, true},
		{"double comma between members", "version = 1\n[manifest]\nmembers = [\"a\",, \"lib\"]\n" + entry("lib", `{ editable = "libs/lib" }`), []string{"lib"}, 0, 0, true},
		{"leading comma in members", "version = 1\n[manifest]\nmembers = [, \"lib\"]\n" + entry("lib", `{ editable = "libs/lib" }`), []string{"lib"}, 0, 0, true},
		{"trailing comma in members is valid", "version = 1\n[manifest]\nmembers = [\"a\", \"lib\", ] # c\n" + entry("lib", `{ editable = "libs/lib" }`), nil, 0, 1, true},
		{"empty members applies none", "version = 1\n[manifest]\nmembers = []\n" + entry("lib", `{ editable = "libs/lib" }`), []string{"lib"}, 0, 0, true},
		{"member outside the workspace stays a row", "version = 1\n[manifest]\nmembers = [\"lib\"]\n" + entry("lib", `{ editable = "../outside/lib" }`), []string{"lib"}, 0, 0, true},
		{"member with a dot-dot segment stays a row", "version = 1\n[manifest]\nmembers = [\"lib\"]\n" + entry("lib", `{ virtual = "libs/../../lib" }`), []string{"lib"}, 0, 0, true},
		{"member with an absolute path stays a row", "version = 1\n[manifest]\nmembers = [\"lib\"]\n" + entry("lib", `{ editable = "/srv/lib" }`), []string{"lib"}, 0, 0, true},
		{"member with a dotted directory name is applied", "version = 1\n[manifest]\nmembers = [\"lib\"]\n" + entry("lib", `{ editable = "libs/..lib" }`), nil, 0, 1, true},
		{"member with a path source stays a row", "version = 1\n[manifest]\nmembers = [\"lib\"]\n" + entry("lib", `{ path = "libs/lib" }`), []string{"lib"}, 0, 0, true},
		{"member with a registry source stays a row", "version = 1\n[manifest]\nmembers = [\"lib\"]\n" + entry("lib", `{ registry = "https://pypi.org/simple" }`), []string{"lib"}, 0, 0, true},
		{"single-line members", "version = 1\n[manifest]\nmembers = [\"lib\", \"x\"] # c\n" + entry("lib", `{ virtual = "libs/lib" }`), nil, 0, 1, true},
		{"malformed root stays a malformed row", "version = 1\n" + entry("a", `{ editable = "." }`) + "version = \"2.0\"\n", []string{"a"}, 0, 0, false},
		{"root after a malformed root stays a row", "version = 1\n" + entry("a", `{ editable = "." }`) + "version = \"2.0\"\n" + entry("b", `{ editable = "." }`), []string{"a", "b"}, 0, 0, false},
		{"root after an unreadable source stays a row", "version = 1\n" + entry("a", `{ editable = ".", x }`) + entry("b", `{ editable = "." }`), []string{"a", "b"}, 0, 0, false},
		{"root after a root with an unreadable name stays a row", "version = 1\n" + entry("-bad", `{ editable = "." }`) + entry("b", `{ editable = "." }`), []string{"b"}, 0, 0, false},
		{"root after an unreadable header stays a row", "version = 1\n[[\"package\"]]\nname = \"a\"\nsource = { editable = \".\" }\n" + entry("b", `{ editable = "." }`), []string{"b"}, 0, 0, false},
		{"root after a readable malformed entry is still the root", "version = 1\n" + entry("a", `{ registry = "https://pypi.org/simple" }`) + "version = \"2.0\"\n" + entry("b", `{ editable = "." }`), []string{"a"}, 1, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := parseUVLockBytes([]byte(tc.src))
			var got []string
			for _, row := range result.Rows {
				got = append(got, row["name"].(string))
			}
			if strings.Join(got, ",") != strings.Join(tc.rows, ",") {
				t.Fatalf("rows = %v, want %v (state %+v)\n%s", got, tc.rows, result.State, tc.src)
			}
			s := result.State
			if s.RootPackages != tc.root || s.WorkspaceMembers != tc.members || (s.State() == "complete") != tc.complete {
				t.Fatalf("state = %+v, want root=%d members=%d complete=%v", s, tc.root, tc.members, tc.complete)
			}
			if sum := s.Resolved + s.Unresolved + s.Overflow + s.RootPackages + s.WorkspaceMembers; sum != s.Entries {
				t.Fatalf("accounted %d != entries %d in %+v", sum, s.Entries, s)
			}
		})
	}
}

// TestParseUVLockQuotedOrInlinePackageFormsAreNeverSilent proves a package
// table written in a TOML form uv does not emit (quoted header key, table
// header, root inline array, quoted root key) makes the file partial instead
// of reading as a complete lock with zero entries.
func TestParseUVLockQuotedOrInlinePackageFormsAreNeverSilent(t *testing.T) {
	t.Parallel()

	pin := "name = \"a\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n"
	cases := map[string]string{
		"quoted array header":         "version = 1\n[[\"package\"]]\n" + pin,
		"literal-quoted array header": "version = 1\n[['package']]\n" + pin,
		"spaced quoted header":        "version = 1\n[[ \"package\" ]]\n" + pin,
		"table header":                "version = 1\n[package]\n" + pin,
		"root inline array":           "version = 1\npackage = [{ name = \"a\", version = \"1.0\", source = { registry = \"https://pypi.org/simple\" } }]\n",
		"root multi-line array":       "version = 1\npackage = [\n  { name = \"a\", version = \"1.0\" },\n]\n",
		"quoted root key":             "version = 1\n\"package\" = [{ name = \"a\" }]\n",
		"dotted root key":             "version = 1\npackage.name = \"a\"\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := parseUVLockBytes([]byte(src))
			if result.State.State() == "complete" {
				t.Fatalf("state = %+v is complete for a package form the reader cannot read:\n%s", result.State, src)
			}
			for _, row := range result.Rows {
				if row["config_kind"] == "dependency" {
					t.Fatalf("unread package form produced a dependency row %#v", row)
				}
			}
		})
	}
}

// TestParseUVLockCredentialFormsAreRefusedNotStored covers userinfo forms a
// slash-delimited authority cut would miss, and path values that look like
// userinfo. No emitted field may carry the planted secret or an "@".
func TestParseUVLockCredentialFormsAreRefusedNotStored(t *testing.T) {
	t.Parallel()

	sources := []string{
		`{ registry = "https://u:S3/CR3T@private.example/simple" }`,
		`{ url = "https://u:S3/CR3T@files.example/x-1.0.tar.gz" }`,
		`{ git = "https://u:S3/CR3T@github.example/acme/x.git#0123456789abcdef0123456789abcdef01234567" }`,
		`{ registry = "https://u:S3/a/b/CR3T@private.example/simple" }`,
		`{ editable = "u:S3CR3T@host:path" }`,
		`{ path = "u:S3CR3T@host/path" }`,
		`{ directory = "S3CR3T@host" }`,
		`{ virtual = "u:S3CR3T" }`,
		`{ url = "https://files.example/x.tar.gz?mail=S3@b" }`,
		`{ url = "https://u:S3/CR?3T@files.example/x.tar.gz" }`,
		`{ git = "https://u:S3/CR#3T@github.example/acme/x.git" }`,
	}
	for _, source := range sources {
		src := "version = 1\n\n[[package]]\nname = \"x\"\nversion = \"1.0\"\nsource = " + source + "\n"
		result := parseUVLockBytes([]byte(src))
		for _, row := range result.Rows {
			for field, value := range row {
				if text, ok := value.(string); ok && (strings.Contains(text, "S3") || strings.Contains(text, "@")) {
					t.Fatalf("field %q = %q leaked for source %s", field, text, source)
				}
			}
			if row["config_kind"] != "malformed_dependency" {
				t.Fatalf("source %s must be refused as malformed, got %#v", source, row)
			}
		}
		if result.State.State() == "complete" {
			t.Fatalf("source %s refused but state is complete: %+v", source, result.State)
		}
	}
}
