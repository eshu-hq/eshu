// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

import (
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// documentationSQLReferencedFields and documentationSchemaDir are this
// family's own copies of root's helpers of the same name
// (documentation_sql_schema_lockstep_test.go), needed by
// package_registry_correlations_sql_schema_lockstep_test.go. Go never
// compiles one package's _test.go files into anything another package can
// import, so this family cannot reuse root's copies; both helpers are
// self-contained (a regex extractor and a runtime.Caller-based path walk),
// so this is a faithful duplicate rather than a fork with drift risk.

// documentationSQLReferencedFields extracts every JSONB payload field name a
// SQL source string reads via `payload->>'field'`/`payload->'field'` (any
// depth of nesting), `addPayloadFilter("field", ...)`, or `payload["field"]`.
// See root's copy for the full rationale, including the #4738 nested-chain
// fix this mirrors.
func documentationSQLReferencedFields(sqlText string) map[string]bool {
	referenced := map[string]bool{}
	arrowRef := regexp.MustCompile(`->>?'([a-z_]+)'`)
	for _, m := range arrowRef.FindAllStringSubmatch(sqlText, -1) {
		referenced[m[1]] = true
	}
	otherRef := regexp.MustCompile(`addPayloadFilter\("([a-z_]+)"|payload\["([a-z_]+)"\]`)
	for _, m := range otherRef.FindAllStringSubmatch(sqlText, -1) {
		if m[1] != "" {
			referenced[m[1]] = true
		}
		if m[2] != "" {
			referenced[m[2]] = true
		}
	}
	return referenced
}

// documentationSchemaDir resolves the checked-in schema directory
// (sdk/go/factschema/schema) relative to this test file, walking up to the
// repo root so the test does not depend on the working directory. This
// family's copy walks one directory further than root's
// (go/internal/query/packagereg/<file> vs. go/internal/query/<file>).
func documentationSchemaDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// go/internal/query/packagereg/<file> -> repo root is four levels up.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	return filepath.Join(repoRoot, "sdk", "go", "factschema", "schema")
}
