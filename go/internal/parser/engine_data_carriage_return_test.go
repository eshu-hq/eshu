// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"testing"
)

// goDataCarriageReturnRouteFixture is an ordinary LF-authored Go file that
// happens to carry a raw carriage-return BYTE as data inside a backquoted
// route string. Go's own spec discards '\r' when evaluating a raw string
// literal, so the route's runtime value is "/foobar" -- and strconv.Unquote,
// which goStringLiteralValue calls, implements exactly that rule.
//
// The file is the counter-example to a byte-level CR rewrite: this '\r' is
// not a line terminator, it is payload, and the file's real terminators are
// the '\n' bytes around it.
const goDataCarriageReturnRouteFixture = "package test\n" +
	"\n" +
	"import \"net/http\"\n" +
	"\n" +
	"func Register() {\n" +
	"\tmux := http.NewServeMux()\n" +
	"\tmux.HandleFunc(`GET /foo\rbar`, listUsers)\n" +
	"}\n" +
	"\n" +
	"func listUsers(w http.ResponseWriter, r *http.Request) {}\n"

// TestParsePathPreservesDataCarriageReturnInsideGoRawString pins what the
// read-boundary normalization must NOT do (issue #6306, PR review). A bare
// '\r' inside a quoted literal of a file that already terminates its lines
// with '\n' is data, not a line break. Rewriting it to '\n' changes the
// parsed VALUE of the literal: strconv.Unquote drops a '\r' from a raw string
// but keeps a '\n', so the route "/foobar" would be recorded as "/foo\nbar"
// -- and because "GET /foo\nbar" then splits into three fields rather than
// two, the method degrades from "GET" to "ANY" as well.
//
// The rule this test locks in is file-scoped: a source containing any '\n'
// has an established LF/CRLF line convention, so every '\r' in it is left
// exactly as the author wrote it.
func TestParsePathPreservesDataCarriageReturnInsideGoRawString(t *testing.T) {
	repoRoot, path := writeRepoFile(t, "routes.go", goDataCarriageReturnRouteFixture)
	payload := mustParsePath(t, repoRoot, path)

	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("payload[%q] missing or wrong type, want map[string]any (%#v)", "framework_semantics", payload["framework_semantics"])
	}
	netHTTP, ok := semantics["net_http"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics[%q] missing or wrong type, want map[string]any (%#v)", "net_http", semantics)
	}

	paths, _ := netHTTP["route_paths"].([]string)
	if len(paths) != 1 || paths[0] != "/foobar" {
		t.Fatalf("net_http route_paths = %#v, want [\"/foobar\"]; a data '\\r' inside a raw string must not be rewritten to '\\n'", paths)
	}
	methods, _ := netHTTP["route_methods"].([]string)
	if len(methods) != 1 || methods[0] != "GET" {
		t.Fatalf("net_http route_methods = %#v, want [\"GET\"]", methods)
	}
}
