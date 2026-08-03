// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestGoldenCorpusManifestFixturesGetSectionKeyedIdentity is the durable
// regression #5509 asks for.
//
// #5357 made npm and composer dependency Variables use a line-independent,
// section-keyed canonical identity. The unit coverage was strong, but the
// 20-repo golden corpus contained no package.json or composer.json at all, so
// the B-7/B-12 gate structurally could not exercise it: a change that
// reintroduced line-churn, or worse collapsed two sections onto one identity,
// would leave every gate green.
//
// This drives the REAL parser over the REAL corpus fixtures and mints
// identities from the metadata those parsers actually emit. That matters more
// than it looks. An earlier draft of this test fabricated the metadata, and
// fabricating it wrong is easy: the section-keyed path is gated on config_kind
// AND package_manager together, and a plausible-looking map missing either one
// silently falls back to the line-bearing identity — so a line-independence
// assertion would pass for the wrong reason. Parsing the fixture means the test
// fails if the PARSER stops emitting what the identity path needs, which is the
// coupling that actually breaks.
func TestGoldenCorpusManifestFixturesGetSectionKeyedIdentity(t *testing.T) {
	t.Parallel()

	const repoID = "repository:lib-common"

	for _, tc := range []struct {
		manifest     string
		duplicate    string
		sections     [2]string
		otherPackage string
	}{
		{manifest: "package.json", duplicate: "lodash", sections: [2]string{"dependencies", "devDependencies"}, otherPackage: "express"},
		{manifest: "composer.json", duplicate: "monolog/monolog", sections: [2]string{"require", "require-dev"}, otherPackage: "phpunit/phpunit"},
	} {
		t.Run(tc.manifest, func(t *testing.T) {
			t.Parallel()
			rows := parseCorpusManifestRows(t, tc.manifest)

			idFor := func(name, section string, lineOverride int) string {
				row := findManifestRow(t, rows, name, section)
				line := intFromRow(row, "line_number")
				if lineOverride > 0 {
					line = lineOverride
				}
				return content.CanonicalEntityIDWithMetadata(
					repoID, tc.manifest, "variable", name, line, row)
			}

			// Cross-section distinctness: the same package in two sections is
			// two dependencies, not one. A collapse here is silent truth loss.
			first := idFor(tc.duplicate, tc.sections[0], 0)
			second := idFor(tc.duplicate, tc.sections[1], 0)
			if first == second {
				t.Errorf("%s collapsed across %s and %s into one identity", tc.duplicate, tc.sections[0], tc.sections[1])
			}

			// Line independence: reordering a manifest must not churn identity.
			if moved := idFor(tc.duplicate, tc.sections[0], 4242); moved != first {
				t.Errorf("%s/%s identity moved when only the line changed: %q vs %q",
					tc.duplicate, tc.sections[0], first, moved)
			}

			// Distinct packages stay distinct.
			other := idFor(tc.otherPackage, sectionOf(t, rows, tc.otherPackage), 0)
			if other == first || other == second {
				t.Errorf("%s shares an identity with %s", tc.otherPackage, tc.duplicate)
			}
		})
	}
}

// parseCorpusManifestRows runs the real parser over one corpus fixture manifest
// and returns its dependency Variable rows.
func parseCorpusManifestRows(t *testing.T, manifest string) []map[string]any {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "tests", "fixtures", "ecosystems", "lib-common")
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v", err)
	}
	payload, err := engine.ParsePath(root, filepath.Join(root, manifest), false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v", manifest, err)
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("%s produced no variable rows; the corpus fixture is not being parsed", manifest)
	}
	return rows
}

func findManifestRow(t *testing.T, rows []map[string]any, name, section string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["name"] == name && row["section"] == section {
			return row
		}
	}
	t.Fatalf("no parsed row for %q in section %q", name, section)
	return nil
}

func sectionOf(t *testing.T, rows []map[string]any, name string) string {
	t.Helper()
	for _, row := range rows {
		if row["name"] == name {
			if section, ok := row["section"].(string); ok {
				return section
			}
		}
	}
	t.Fatalf("no parsed row for %q", name)
	return ""
}

func intFromRow(row map[string]any, key string) int {
	switch value := row[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	}
	return 0
}
