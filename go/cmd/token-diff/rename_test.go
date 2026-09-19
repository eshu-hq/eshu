// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runRenameCLI is runCLI with the -allow-internal-import-rename flag set.
func runRenameCLI(t *testing.T, base, head string) (code int, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.go")
	headPath := filepath.Join(dir, "head.go")
	if err := os.WriteFile(basePath, []byte(base), 0o600); err != nil {
		t.Fatalf("write base: %v", err)
	}
	if err := os.WriteFile(headPath, []byte(head), 0o600); err != nil {
		t.Fatalf("write head: %v", err)
	}
	var outBuf, errBuf bytes.Buffer
	code = run([]string{"-allow-internal-import-rename", "-base", basePath, "-head", headPath}, &outBuf, &errBuf)
	return code, outBuf.String(), errBuf.String()
}

// renameBase is an importer of an internal package, shaped like
// go/internal/query/language/handler_tracing.go before the #6818 move.
const renameBase = `package language

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
	"go.opentelemetry.io/otel/trace"
)

// Seeding it from queryspan.HandlerTracer keeps the swap private.
var languageHandlerTracer = queryspan.HandlerTracer()

func start(r *http.Request, name string) (*http.Request, trace.Span) {
	return queryspan.StartHandlerSpanWith(languageHandlerTracer, r, name, "route", "cap")
}
`

// renamed applies the full, consistent queryspan -> tracing move to src.
func renamed(src string) string {
	return strings.ReplaceAll(src, "queryspan", "tracing")
}

func TestRename_PureQualifierRenameIsExempt(t *testing.T) {
	code, out, errOut := runRenameCLI(t, renameBase, renamed(renameBase))
	if code != 0 {
		t.Fatalf("pure rename: got exit %d, want 0; stdout=%q stderr=%q", code, out, errOut)
	}
	// scripts/lib/parser_relationship_comment_only_diff.sh parses this exact
	// line to run its package-move check; keep the shape stable.
	want := "token-diff: internal import rename only: " +
		"github.com/eshu-hq/eshu/go/internal/query/queryspan -> " +
		"github.com/eshu-hq/eshu/go/internal/query/tracing (qualifier queryspan. -> tracing.)\n"
	if out != want {
		t.Fatalf("pure rename: stdout = %q, want %q", out, want)
	}
}

func TestRename_FlagOffStillReportsChange(t *testing.T) {
	code, out, errOut := runCLI(t, renameBase, renamed(renameBase))
	if code != 1 {
		t.Fatalf("flag off: got exit %d, want 1; stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestRename_ReorderedImportBlockIsExempt(t *testing.T) {
	// The moved import sorts to a different line of the block, as gofumpt
	// would place it: zspan after mid in the base, aspan before it in head.
	base := `package language

import (
	"github.com/eshu-hq/eshu/go/internal/query/mid"
	"github.com/eshu-hq/eshu/go/internal/query/zspan"
)

var _ = mid.X(zspan.Y())
`
	head := `package language

import (
	"github.com/eshu-hq/eshu/go/internal/query/aspan"
	"github.com/eshu-hq/eshu/go/internal/query/mid"
)

var _ = mid.X(aspan.Y())
`
	code, out, errOut := runRenameCLI(t, base, head)
	if code != 0 {
		t.Fatalf("reordered import: got exit %d, want 0; stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestRename_RefusedCases(t *testing.T) {
	head := renamed(renameBase)
	cases := []struct {
		name, base, head string
	}{
		{
			name: "rename plus a real code change",
			base: renameBase,
			head: strings.Replace(head, `"route"`, `"other-route"`, 1),
		},
		{
			name: "qualifier renamed inconsistently",
			base: renameBase,
			head: strings.Replace(head, "tracing.StartHandlerSpanWith", "tracer.StartHandlerSpanWith", 1),
		},
		{
			name: "qualifier left on the old name",
			base: renameBase,
			head: strings.Replace(head, "tracing.StartHandlerSpanWith", "queryspan.StartHandlerSpanWith", 1),
		},
		{
			name: "non-internal import path changed",
			base: renameBase,
			head: strings.ReplaceAll(renameBase, "go.opentelemetry.io/otel/trace", "go.opentelemetry.io/otel/trace/noop"),
		},
		{
			name: "non-internal package moved with a consistent qualifier rename",
			base: strings.ReplaceAll(renameBase, "github.com/eshu-hq/eshu/go/internal/query/", "example.com/lib/"),
			head: strings.ReplaceAll(head, "github.com/eshu-hq/eshu/go/internal/query/", "example.com/lib/"),
		},
		{
			name: "string literal edited to look like the rename",
			base: strings.Replace(renameBase, `"route"`, `"queryspan.route"`, 1),
			head: head,
		},
		{
			name: "aliased import",
			base: strings.Replace(renameBase, `	"github.com/eshu-hq/eshu/go/internal/query/queryspan"`, `	queryspan "github.com/eshu-hq/eshu/go/internal/query/queryspan"`, 1),
			head: strings.Replace(head, `	"github.com/eshu-hq/eshu/go/internal/query/tracing"`, `	tracing "github.com/eshu-hq/eshu/go/internal/query/tracing"`, 1),
		},
		{
			name: "two internal imports renamed",
			base: strings.Replace(renameBase, `	"net/http"`, `	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"`, 1) + "var _ = querycontract.X\n",
			head: strings.Replace(head, `	"net/http"`, `	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/contract"`, 1) + "var _ = contract.X\n",
		},
		{
			name: "old name also used as a non-qualifier",
			base: renameBase + "var queryspan = 1\n",
			head: head + "var tracing = 1\n",
		},
		{
			name: "new name already used in the base file",
			base: renameBase + "var tracing = 1\n",
			head: head + "var tracing = 1\n",
		},
		{
			name: "old name used as a selector field",
			base: renameBase + "var _ = x.queryspan.Y\n",
			head: head + "var _ = x.tracing.Y\n",
		},
		{
			// A blank import is a side-effect registration: adding one next to
			// the rename is a second import change, never part of a move.
			name: "rename plus an added blank import",
			base: renameBase,
			head: strings.Replace(head, `	"net/http"`, `	"net/http"

	_ "github.com/eshu-hq/eshu/go/internal/query/plugin"`, 1),
		},
		{
			// The region before the imports (build constraints, package
			// clause) must match exactly too.
			name: "rename plus a go:build line change",
			base: "//go:build linux\n\n" + renameBase,
			head: "//go:build darwin\n\n" + head,
		},
		{
			// A path whose last element is not a Go identifier cannot name the
			// qualifier, so the file's qualifier comes from the package clause
			// and the tool cannot prove the swap is a move. Both sides use
			// foo.X, so without the identifier guard the token streams match.
			name: "import path base is not an identifier",
			base: "package query\n\nimport \"github.com/eshu-hq/eshu/go/internal/query/foo-v1\"\n\nvar _ = foo.X\n",
			head: "package query\n\nimport \"github.com/eshu-hq/eshu/go/internal/query/foo-v2\"\n\nvar _ = foo.X\n",
		},
		{
			name: "block comment in the import block",
			base: renameBase,
			head: strings.Replace(head, `	"net/http"`, `	"net/http" /* note */`, 1),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runRenameCLI(t, tc.base, tc.head)
			if code != 1 {
				t.Fatalf("got exit %d, want 1 (a real change, not exempt and not an error); stdout=%q stderr=%q", code, out, errOut)
			}
		})
	}
}
