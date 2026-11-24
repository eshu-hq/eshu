// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This is test support, not a product command: it renders the ungranted explain statement
// shapes of the supports read as psql-ready files (#10880 PR-C1, arbiter ruling 15 (i)), so a
// read-only run on a replica can prove no shape scans the supports table. Set
// ESHU_RENDER_EXPLAIN_DIR to a directory to write them; without it the test skips.
//
// One file per shape and variant: <shape>.plain.sql runs under the session's plan cache mode,
// <shape>.generic.sql under plan_cache_mode = force_generic_plan (the "$n = '' OR column = $n"
// predicates fold only in a custom plan, so the generic plan is the harder case). Each file sets
// psql variables with defaults (override with -v name=value), prepares the statement exactly as
// the code defines it, and runs EXPLAIN (ANALYZE, BUFFERS) EXECUTE. The statements are read-only;
// a statement that takes longer than the replica's conflict window will be cancelled there, so
// the files set a 20 s statement_timeout and the plain EXPLAIN line is left as a comment.

type explainShape struct {
	name        string
	statement   string
	description string
	// values maps the 1-based bind position to a psql expression; positions not listed take the
	// unfiltered defaults.
	values map[int]string
}

func explainShapes() []explainShape {
	ungranted := ExplainFindingUngrantedFromSupportsQuery
	publicID := ExplainFindingByPublicIDFromSupportsQuery
	return []explainShape{
		{"cve", ungranted, "ungranted selector by CVE id", map[int]string{4: ":'cve'"}},
		{"advisory", ungranted, "ungranted selector by advisory id", map[int]string{3: ":'advisory'"}},
		{"package", ungranted, "ungranted selector by package id", map[int]string{5: ":'package'"}},
		{"repository", ungranted, "ungranted selector by repository id", map[int]string{6: ":'repository'"}},
		{"subject_digest", ungranted, "ungranted selector by subject digest", map[int]string{7: ":'digest'"}},
		{"cve_package", ungranted, "ungranted selector by CVE id plus package id", map[int]string{4: ":'cve'", 5: ":'package'"}},
		{"identity_fact_id", ungranted, "identity lookup by fact id with a CVE and package selector", map[int]string{2: ":'fact_id'", 4: ":'cve'", 5: ":'package'"}},
		{"identity_finding_id", ungranted, "identity lookup by finding id (the fallback of a public-id miss)", map[int]string{2: ":'finding_id'", 4: ":'cve'", 5: ":'package'"}},
		{"identity_canonical_key", ungranted, "identity lookup by canonical key with a CVE and package selector", map[int]string{2: ":'canonical_key'", 4: ":'cve'", 5: ":'package'"}},
		{"public_id_hit", publicID, "public finding id probe, an id that exists", map[int]string{2: ":'finding_id'"}},
		{"public_id_miss", publicID, "public finding id probe, an id that does not exist", map[int]string{2: "'finding:no-such-finding'"}},
	}
}

// explainDefaults are the psql variable defaults a file carries; they are real-looking values
// the reader replaces with -v.
var explainDefaults = [][2]string{
	{"cve", "CVE-2026-33349"},
	{"advisory", "GHSA-xxxx-xxxx-xxxx"},
	{"package", "pkg:npm/axios"},
	{"repository", "repository:example"},
	{"digest", "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
	{"fact_id", "fact:example"},
	{"finding_id", "finding:example"},
	{"canonical_key", "example-canonical-key"},
}

func renderExplainShape(shape explainShape, generic bool) string {
	// $1 fact kind, $2 finding id, $3 advisory or cve, $4 cve, $5 package, $6 repository,
	// $7 subject digest, $8 workload, $9 service, $10 image ref, $11 allowed repositories,
	// $12 allowed scopes, $13 read-at, $14 caller scoped, $15..$18 caller identity, $19 allowed scopes.
	defaults := map[int]string{
		1: "'reducer_supply_chain_impact_finding'", 2: "''", 3: "''", 4: "''", 5: "''", 6: "''", 7: "''", 8: "''", 9: "''", 10: "''",
		11: "'{}'::text[]", 12: "'{}'::text[]", 13: "now()", 14: "false", 15: "''", 16: "''", 17: "''", 18: "''", 19: "'{}'::text[]",
	}
	args := make([]string, 19)
	for position := 1; position <= 19; position++ {
		if v, ok := shape.values[position]; ok {
			args[position-1] = v
		} else {
			args[position-1] = defaults[position]
		}
	}
	var b strings.Builder
	variant := "plain (the session's plan cache mode)"
	if generic {
		variant = "force_generic_plan"
	}
	fmt.Fprintf(&b, "-- Shape: %s. Variant: %s.\n-- Rendered from the C1 head by explain_shapes_render_test.go; read-only. Override a value with psql -v name=value.\n", shape.description, variant)
	b.WriteString("\\set ON_ERROR_STOP on\n")
	for _, d := range explainDefaults {
		fmt.Fprintf(&b, "\\if :{?%s}\n\\else\n\\set %s '%s'\n\\endif\n", d[0], d[0], d[1])
	}
	b.WriteString("SET statement_timeout = '20s';\n")
	if generic {
		b.WriteString("SET plan_cache_mode = force_generic_plan;\n")
	}
	b.WriteString("PREPARE q AS\n")
	b.WriteString(strings.TrimSpace(shape.statement))
	b.WriteString(";\n")
	b.WriteString("-- for the plan without running the statement, drop ANALYZE from the line below\n")
	fmt.Fprintf(&b, "EXPLAIN (ANALYZE, BUFFERS) EXECUTE q(%s);\n", strings.Join(args, ", "))
	b.WriteString("DEALLOCATE q;\n")
	return b.String()
}

// TestRenderUngrantedExplainShapes writes the shape files to ESHU_RENDER_EXPLAIN_DIR. It also
// checks, without a directory, that every shape renders with the 19 binds the statement takes.
func TestRenderUngrantedExplainShapes(t *testing.T) {
	for _, shape := range explainShapes() {
		for _, generic := range []bool{false, true} {
			text := renderExplainShape(shape, generic)
			if !strings.Contains(text, "PREPARE q AS") || strings.Count(text, "EXECUTE q(") != 1 {
				t.Fatalf("shape %s did not render a PREPARE and one EXECUTE", shape.name)
			}
			if strings.Contains(text, "fact_records AS fact\n  JOIN ingestion_scopes") {
				t.Fatalf("shape %s still reads the finding facts as its candidate source", shape.name)
			}
			if generic != strings.Contains(text, "force_generic_plan;") {
				t.Fatalf("shape %s: the generic variant flag and the SET plan_cache_mode line disagree", shape.name)
			}
		}
	}
	dir := os.Getenv("ESHU_RENDER_EXPLAIN_DIR")
	if dir == "" {
		t.Skip("set ESHU_RENDER_EXPLAIN_DIR to write the explain shape files")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	var index strings.Builder
	index.WriteString("# Ungranted explain shapes (supports read)\n\nRendered from the C1 head. Run each with psql -f on a read-only replica; override values with -v name=value (cve, advisory, package, repository, digest, fact_id, finding_id, canonical_key). Every file prepares the statement as the code defines it and runs EXPLAIN (ANALYZE, BUFFERS) EXECUTE under a 20 s statement_timeout; the .generic.sql variant sets plan_cache_mode = force_generic_plan first. The pass condition is no Seq Scan on supply_chain_impact_finding_supports in any plan.\n\n")
	for _, shape := range explainShapes() {
		for _, generic := range []bool{false, true} {
			name := shape.name + ".plain.sql"
			if generic {
				name = shape.name + ".generic.sql"
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte(renderExplainShape(shape, generic)), 0o644); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		fmt.Fprintf(&index, "- %s: %s\n", shape.name, shape.description)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(index.String()), 0o644); err != nil {
		t.Fatalf("write the index: %v", err)
	}
}
