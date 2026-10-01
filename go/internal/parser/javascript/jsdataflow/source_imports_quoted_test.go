// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jsdataflow

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/taint"
)

// TestTSQuotedFrameworkRequestImportIsSource proves a string-literal import
// name ('Request') names the same framework type as the identifier spelling,
// whichever quote style is used (issue #7461).
func TestTSQuotedFrameworkRequestImportIsSource(t *testing.T) {
	t.Parallel()

	for name, importLine := range map[string]string{
		"single": "import type { 'Request' as ExpressRequest } from 'express';\n",
		"double": "import type { \"Request\" as ExpressRequest } from 'express';\n",
	} {
		importLine := importLine
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			node, source, fn := parseFirstFunction(t, importLine+
				"function handler(req: ExpressRequest) {\n"+
				"\tconst q = req.body;\n"+
				"\tdb.query(q);\n"+
				"}")
			facts := TaintFacts(node, source, fn)
			res := taint.Analyze(fn, facts, taint.DefaultLimits())
			if taintedCount(res, "sql") != 1 {
				t.Fatalf("want 1 TAINTED sql finding for quoted express Request, got %+v", res.Findings)
			}
		})
	}
}

// TestTSQuotedFrameworkRequestImportWithStringAliasIsNotSource: a string-literal
// import alias is not valid ECMAScript (the grammar takes an identifier), so the
// import registers no framework type and a parameter typed with the bare name is
// not a source (#7461).
func TestTSQuotedFrameworkRequestImportWithStringAliasIsNotSource(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstFunction(t, "import type { 'Request' as '' } from 'express';\n"+
		"function handler(req: Request) {\n"+
		"\tconst q = req.body;\n"+
		"\tdb.query(q);\n"+
		"}")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if got := taintedCount(res, "sql"); got != 0 {
		t.Fatalf("want 0 TAINTED sql findings for a string import alias, got %d: %+v", got, res.Findings)
	}
}
