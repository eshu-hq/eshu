// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// uvFuzzCases is the local case count; ESHU_UV_FUZZ_CASES overrides it for a
// longer run (the PR records one 30,000-case run).
func uvFuzzCases(t *testing.T) int {
	t.Helper()
	if raw := os.Getenv("ESHU_UV_FUZZ_CASES"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			t.Fatalf("ESHU_UV_FUZZ_CASES = %q, want a positive integer", raw)
		}
		return n
	}
	return 4000
}

// uvFuzzExpect is what a clean (unmutated) seed file must parse to.
type uvFuzzExpect struct {
	rows, root, members int
}

// uvFuzzSeedFile builds one valid uv.lock with n entries from a seeded source.
// Some files carry a [manifest] members list; members, the "." root, and
// genuine local path dependencies are mixed in so the omission rule is tested
// against its neighbours.
func uvFuzzSeedFile(rng *rand.Rand, n int) (string, uvFuzzExpect) {
	var b strings.Builder
	var want uvFuzzExpect
	b.WriteString("version = 1\nrevision = 2\nrequires-python = \">=3.12\"\n")
	withMembers := rng.Intn(2) == 0
	if withMembers {
		b.WriteString("\n[manifest]\nmembers = [\n    \"Pkg_0\",\n    \"pkg-1\",\n]\n")
	}
	sources := []string{
		`{ registry = "https://pypi.org/simple" }`,
		`{ registry = "https://index.internal.example/simple" }`,
		`{ git = "https://github.com/acme/x.git?rev=main#0123456789abcdef0123456789abcdef01234567" }`,
		`{ editable = "." }`,
		`{ path = "../local" }`,
		`{ url = "https://example.com/x-1.0.tar.gz" }`,
		`{ editable = "packages/lib" }`,
		`{ virtual = "tools" }`,
	}
	rootSeen := false
	for i := 0; i < n; i++ {
		pick := rng.Intn(len(sources))
		switch {
		case pick == 3 && !rootSeen:
			rootSeen = true
			want.root++
		case (pick == 6 || pick == 7) && withMembers && i <= 1:
			want.members++
		default:
			want.rows++
		}
		fmt.Fprintf(&b, "\n[[package]]\nname = \"pkg-%d\"\nversion = \"%d.%d.%d\"\nsource = %s\n", i, rng.Intn(9), rng.Intn(99), rng.Intn(99), sources[pick])
		if rng.Intn(2) == 0 {
			b.WriteString("dependencies = [\n    { name = \"dep\" },\n    { name = \"other\", marker = \"python_full_version < '3.11'\" },\n]\n")
		}
		if rng.Intn(3) == 0 {
			b.WriteString("wheels = [\n    { url = \"https://files.example/x.whl\", hash = \"sha256:aa\", size = 1 },\n]\n")
		}
		if rng.Intn(4) == 0 {
			b.WriteString("\n[package.optional-dependencies]\nextra = [\n    { name = \"more\" },\n]\n")
		}
	}
	return b.String(), want
}

// uvFuzzUnreadForm rewrites a clean file into a package form the reader does
// not read: a quoted or table header, or a root package key.
func uvFuzzUnreadForm(rng *rand.Rand, src string) string {
	headers := []string{`[["package"]]`, `[['package']]`, `[[ "package" ]]`, `[package]`}
	if rng.Intn(2) == 0 {
		return strings.Replace(src, "[[package]]", headers[rng.Intn(len(headers))], 1)
	}
	roots := []string{
		"package = [{ name = \"a\", version = \"1.0\" }]\n",
		"package = [\n  { name = \"a\" },\n]\n",
		"\"package\" = []\n",
		"package.name = \"a\"\n",
	}
	return strings.Replace(src, "revision = 2\n", "revision = 2\n"+roots[rng.Intn(len(roots))], 1)
}

// uvFuzzMutate applies one random textual mutation.
func uvFuzzMutate(rng *rand.Rand, src string) string {
	if src == "" {
		return src
	}
	noise := []string{"\"", "'", "[", "]", "{", "}", "=", "\n", " ", "#", ".", "\\", "\"\"\"", "[[package]]\n", "version = \"9.9.9\"\n", "name = \"evil\"\n"}
	switch rng.Intn(7) {
	case 0: // delete a span
		i := rng.Intn(len(src))
		j := i + rng.Intn(40)
		if j > len(src) {
			j = len(src)
		}
		return src[:i] + src[j:]
	case 1: // insert noise
		i := rng.Intn(len(src))
		return src[:i] + noise[rng.Intn(len(noise))] + src[i:]
	case 2: // replace a byte with noise
		i := rng.Intn(len(src))
		return src[:i] + noise[rng.Intn(len(noise))] + src[i+1:]
	case 3: // truncate
		return src[:rng.Intn(len(src))]
	case 4: // duplicate a line
		lines := strings.Split(src, "\n")
		i := rng.Intn(len(lines))
		lines = append(lines[:i+1], lines[i:]...)
		return strings.Join(lines, "\n")
	case 5: // swap two lines
		lines := strings.Split(src, "\n")
		i, j := rng.Intn(len(lines)), rng.Intn(len(lines))
		lines[i], lines[j] = lines[j], lines[i]
		return strings.Join(lines, "\n")
	default: // drop a line
		lines := strings.Split(src, "\n")
		i := rng.Intn(len(lines))
		return strings.Join(append(lines[:i], lines[i+1:]...), "\n")
	}
}

// uvAssertResolvedRowsAreWrittenVerbatim checks the central invariant: a
// dependency row's name and version appear exactly as written, on lines of
// the same [[package]] entry that begins at the row's line number, with a
// recognised source line and no header in between.
func uvAssertResolvedRowsAreWrittenVerbatim(t *testing.T, src string, result uvLockResult) {
	t.Helper()
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for _, row := range result.Rows {
		if row["config_kind"] != "dependency" {
			continue
		}
		start, _ := row["line_number"].(int)
		if start < 1 || start > len(lines) || strings.TrimSpace(lines[start-1]) != "[[package]]" {
			t.Fatalf("row %#v does not start at a [[package]] line in:\n%s", row, src)
		}
		wantName := fmt.Sprintf("name=%q", row["name"])
		wantVersion := fmt.Sprintf("version=%q", row["value"])
		var sawName, sawVersion, sawSource bool
		for i := start; i < len(lines); i++ {
			// Drop all spacing: TOML allows any spacing around "=" and "{",
			// and the grammar admits no whitespace inside these values.
			line := strings.Join(strings.Fields(lines[i]), "")
			if strings.HasPrefix(line, "[") {
				break
			}
			switch {
			case line == wantName:
				sawName = true
			case line == wantVersion:
				sawVersion = true
			case strings.HasPrefix(line, "source={registry=\"https://pypi.org/simple"):
				sawSource = true
			}
		}
		if !sawName || !sawVersion || !sawSource {
			t.Fatalf("resolved row %#v is not written verbatim (name=%v version=%v source=%v) in:\n%s", row, sawName, sawVersion, sawSource, src)
		}
		if row["source_kind"] != "registry" || !strings.HasPrefix(row["source_url"].(string), "https://pypi.org/simple") {
			t.Fatalf("a dependency row must come from the PyPI index: %#v", row)
		}
	}
}

// TestParseUVLockSeededFuzzKeepsInvariants runs a deterministic generator and
// mutator for a few thousand cases and asserts the fail-closed invariants.
func TestParseUVLockSeededFuzzKeepsInvariants(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(8821))
	var clean, unreadForms, roots, members, partial int
	cases := uvFuzzCases(t)
	for c := 0; c < cases; c++ {
		src, want := uvFuzzSeedFile(rng, 1+rng.Intn(4))
		mutations := rng.Intn(4)
		unread := mutations == 0 && rng.Intn(8) == 0
		if unread {
			src = uvFuzzUnreadForm(rng, src)
		}
		for m := 0; m < mutations; m++ {
			src = uvFuzzMutate(rng, src)
		}
		result := parseUVLockBytes([]byte(src))
		s := result.State
		roots, members = roots+s.RootPackages, members+s.WorkspaceMembers
		if s.State() != "complete" {
			partial++
		}
		if len(result.Rows) > uvLockMaxRows {
			t.Fatalf("case %d: %d rows exceed the cap", c, len(result.Rows))
		}
		if got := s.Resolved + s.Unresolved + s.Overflow + s.RootPackages + s.WorkspaceMembers; got != s.Entries {
			t.Fatalf("case %d: accounted=%d != entries=%d in %+v\n%s", c, got, s.Entries, s, src)
		}
		switch {
		case unread:
			unreadForms++
			if s.State() == "complete" {
				t.Fatalf("case %d: unread package form reads complete %+v\n%s", c, s, src)
			}
		case mutations == 0:
			clean++
			if len(result.Rows) != want.rows || s.RootPackages != want.root || s.WorkspaceMembers != want.members || s.State() != "complete" {
				t.Fatalf("case %d: rows=%d state=%+v, want %+v complete\n%s", c, len(result.Rows), s, want, src)
			}
		}
		if s.RootPackages > 1 || s.RootPackages+s.WorkspaceMembers > strings.Count(src, "editable = ")+strings.Count(src, "virtual = ") {
			t.Fatalf("case %d: omitted more entries than editable/virtual sources: %+v\n%s", c, s, src)
		}
		if result.Scanned > 3*len(src)+1024 {
			t.Fatalf("case %d: scanned %d > 3x input %d", c, result.Scanned, len(src))
		}
		for _, row := range result.Rows {
			if row["lockfile"] != true || row["package_manager"] != PackageManager {
				t.Fatalf("case %d: row lost lockfile/package_manager: %#v", c, row)
			}
		}
		uvAssertResolvedRowsAreWrittenVerbatim(t, src, result)
		again := parseUVLockBytes([]byte(src))
		if !reflect.DeepEqual(result.Rows, again.Rows) || result.State != again.State {
			t.Fatalf("case %d: parse is not deterministic", c)
		}
	}
	t.Logf("%d cases: %d clean exact, %d unread package forms, %d partial; omitted %d roots and %d members",
		cases, clean, unreadForms, partial, roots, members)
}

// TestParseUVLockUnintendedConstructsNeverResolve mutates a known-good entry
// one construct at a time; no mutation may still yield a registry row with a
// version the file does not state verbatim.
func TestParseUVLockUnintendedConstructsNeverResolve(t *testing.T) {
	t.Parallel()

	good := "version = 1\n\n[[package]]\nname = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n"
	mutations := []string{
		strings.Replace(good, "version = \"1.0\"", "version.x = \"1.0\"", 1),
		strings.Replace(good, "version = \"1.0\"", "version = \"1.0\\\"\"", 1),
		strings.Replace(good, "version = \"1.0\"", "version = \"1.0\" # c\nversion = \"2.0\"", 1),
		strings.Replace(good, "version = \"1.0\"", "version = [\"1.0\"]", 1),
		strings.Replace(good, "version = \"1.0\"", "version = { v = \"1.0\" }", 1),
		strings.Replace(good, "version = \"1.0\"", "version = \"\"\"1.0\"\"\"", 1),
		strings.Replace(good, "source = {", "source = [{", 1),
		strings.Replace(good, "registry =", "registry.x =", 1),
		strings.Replace(good, "registry =", "registry = \"a\", registry =", 1),
		strings.Replace(good, "https://pypi.org/simple", "https://pypi.org/simple\" , url = \"x", 1),
		strings.Replace(good, "https://pypi.org/simple", "https://pypi.org/simplex", 1),
		strings.Replace(good, "https://pypi.org/simple", "https://evil.pypi.org/simple", 1),
		strings.Replace(good, "https://pypi.org/simple", "https://pypi.org@evil.example/simple", 1),
		strings.Replace(good, "https://pypi.org/simple", "http://pypi.org/simple", 1),
		strings.Replace(good, "name = \"x\"", "name = \"x\" \"y\"", 1),
		"version = 1\n[[package]]\nname = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n[[package]]]\nname = \"z\"\nversion = \"3.0\"\n",
	}
	for i, src := range mutations {
		result := parseUVLockBytes([]byte(src))
		uvAssertResolvedRowsAreWrittenVerbatim(t, src, result)
		for _, row := range result.Rows {
			if row["config_kind"] == "dependency" && row["name"] == "x" && src != good && i != len(mutations)-1 {
				t.Fatalf("mutation %d still resolved: %#v\n%s", i, row, src)
			}
		}
	}
}

// TestParseUVLockSeededCredentialsNeverReachARow generates userinfo with
// delimiter bytes inside the password and asserts the secret text never
// appears in any persisted row field, whatever the source kind.
func TestParseUVLockSeededCredentialsNeverReachARow(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(88210))
	delimiters := []string{"#", "?", "@", ":", "%40", "!", "$", "&", "=", ";", "/", "%2F", "\\\\", "@@", "/@"}
	keys := []string{"git", "url", "registry", "editable", "path", "directory", "virtual"}
	prefixes := []string{"https://user:", "git+ssh://user:", "user:", ""}
	for c := 0; c < 3000; c++ {
		secret := "SECRET" + delimiters[rng.Intn(len(delimiters))] + "TOKEN" + delimiters[rng.Intn(len(delimiters))] + "ZZ"
		tail := []string{"", "/x.git", "/simple", "/a/b.tar.gz?sig=SECRETQ#SECRETF", ":path"}[rng.Intn(5)]
		src := fmt.Sprintf("version = 1\n\n[[package]]\nname = \"x\"\nversion = \"1.0\"\nsource = { %s = \"%s%s@host.example%s\" }\n",
			keys[rng.Intn(len(keys))], prefixes[rng.Intn(len(prefixes))], secret, tail)
		for _, row := range parseUVLockBytes([]byte(src)).Rows {
			for field, value := range row {
				if text, ok := value.(string); ok && (strings.Contains(text, "SECRET") || strings.Contains(text, "TOKEN") || strings.Contains(text, "@")) {
					t.Fatalf("case %d: credential text reached row field %q = %q for source:\n%s", c, field, text, src)
				}
			}
		}
	}
}
