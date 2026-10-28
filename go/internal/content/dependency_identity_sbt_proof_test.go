// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestSbtRowsGetSectionKeyedIdentityOverRealManifests is the #8962
// uniqueness proof. It drives the REAL sbt parser over two manifests and
// mints identities from the metadata the parser actually emits — never from
// a fabricated map — so the proof fails if the parser stops emitting what
// the section-keyed path needs (value, sbt_cross_version, section).
//
// Manifest 1 is the checked-in content-version fixture
// (testdata/content_version/jvm/build.sbt): its rows must already be
// pairwise distinct and line-independent.
//
// Manifest 2 is a realistic multi-project build.sbt with the redeclaration
// shapes #8962 exists for: the same org:name twice in one section at two
// revisions, % vs %% of one coordinate at one revision, one coordinate in
// both the compile and Test sections, and per-project settings rows. Every
// row whose (section, name, value, sbt_cross_version) tuple differs from
// every other row's must mint a distinct id, and every id must survive a
// line move.
//
// Known contract limit, documented not fixed: rows carry no project field,
// so the same coordinate declared with the same revision and cross operator
// in two projects of one build.sbt shares one id. That sharing is the
// idiomatic multi-project shape — both declarations are real and coexist in
// sbt — and it is pinned by TestSbtSameCoordinateAcrossProjectsSharesOneId
// below; only a byte-identical redeclaration inside ONE project is the
// pathological duplicate, the same accepted residual as an
// exactly-duplicated gradle line.
func TestSbtRowsGetSectionKeyedIdentityOverRealManifests(t *testing.T) {
	t.Parallel()

	t.Run("content-version fixture", func(t *testing.T) {
		t.Parallel()
		rows := parseSbtManifestRows(t, sbtContentVersionFixture(t))

		if len(rows) != 2 {
			t.Fatalf("fixture build.sbt produced %d variable rows, want 2", len(rows))
		}
		assertDistinctAndStable(t, "jvm/build.sbt", rows)
	})

	t.Run("multi-project build.sbt", func(t *testing.T) {
		t.Parallel()
		rows := parseSbtManifestRows(t, writeMultiProjectBuildSbt(t))

		if len(rows) != 7 {
			t.Fatalf("multi-project build.sbt produced %d variable rows, want 7", len(rows))
		}
		assertDistinctAndStable(t, "build.sbt", rows)
	})
}

// assertDistinctAndStable mints one section-keyed id per row from the row's
// own metadata and proves pairwise distinctness plus line-move stability.
// Every row must also differ from its legacy line-keyed id, proving the row
// actually routes through the section-keyed scheme instead of falling back
// to it (a row missing section, value, or the sbt manager label would fall
// back and this assertion would catch it).
func assertDistinctAndStable(t *testing.T, displayPath string, rows []map[string]any) {
	t.Helper()

	const repoID = "repository:sbt-proof"

	ids := make([]string, len(rows))
	for i, row := range rows {
		name, _ := row["name"].(string)
		line := sbtIntFromRow(row, "line_number")
		ids[i] = content.CanonicalEntityIDWithMetadata(repoID, displayPath, "variable", name, line, row)
		if legacy := content.CanonicalEntityID(repoID, displayPath, "variable", name, line); ids[i] == legacy {
			t.Errorf("row %d (%q) fell back to the legacy line-keyed id; the sbt row is missing what the section-keyed gate needs: %v", i, name, row)
		}
	}

	for i := range rows {
		for j := i + 1; j < len(rows); j++ {
			if sbtIdentityTuple(rows[i]) == sbtIdentityTuple(rows[j]) {
				continue
			}
			if ids[i] == ids[j] {
				t.Errorf("rows %d and %d %v vs %v collapsed into one id %q", i, j, sbtIdentityTuple(rows[i]), sbtIdentityTuple(rows[j]), ids[i])
			}
		}
	}

	for i, row := range rows {
		name, _ := row["name"].(string)
		if moved := content.CanonicalEntityIDWithMetadata(repoID, displayPath, "variable", name, 4242, row); moved != ids[i] {
			t.Errorf("row %d (%q) churned across a line move: %q vs %q", i, name, ids[i], moved)
		}
	}
}

// TestSbtSameCoordinateAcrossProjectsSharesOneId pins the disclosed residual
// through the real engine: the same coordinate declared in two projects of
// one build.sbt mints one shared id, stable across a line move and distinct
// from the legacy line-keyed id. This is the idiomatic multi-project shape,
// not a pathological duplicate.
func TestSbtSameCoordinateAcrossProjectsSharesOneId(t *testing.T) {
	t.Parallel()

	rows := parseSbtManifestRows(t, writeSharedCoordinateBuildSbt(t))
	if len(rows) != 2 {
		t.Fatalf("shared-coordinate build.sbt produced %d variable rows, want 2", len(rows))
	}

	const repoID = "repository:sbt-proof"
	const displayPath = "shared-build.sbt"
	ids := make([]string, len(rows))
	for i, row := range rows {
		name, _ := row["name"].(string)
		line := sbtIntFromRow(row, "line_number")
		ids[i] = content.CanonicalEntityIDWithMetadata(repoID, displayPath, "variable", name, line, row)
		if legacy := content.CanonicalEntityID(repoID, displayPath, "variable", name, line); ids[i] == legacy {
			t.Fatalf("row %d fell back to the legacy line-keyed id: %v", i, row)
		}
		if moved := content.CanonicalEntityIDWithMetadata(repoID, displayPath, "variable", name, 4242, row); moved != ids[i] {
			t.Fatalf("row %d churned across a line move: %q vs %q", i, ids[i], moved)
		}
	}
	if ids[0] != ids[1] {
		t.Fatalf("same coordinate in two projects minted distinct ids %q vs %q; the disclosed residual says one", ids[0], ids[1])
	}
}

// writeSharedCoordinateBuildSbt writes a two-project build.sbt declaring one
// coordinate at one revision in both projects.
func writeSharedCoordinateBuildSbt(t *testing.T) string {
	t.Helper()
	const body = `scalaVersion := "2.13.12"

lazy val core = project.settings(
  libraryDependencies += "org.typelevel" %% "cats-core" % "2.10.0"
)

lazy val api = project.settings(
  libraryDependencies += "org.typelevel" %% "cats-core" % "2.10.0"
)
`
	dir := t.TempDir()
	manifest := filepath.Join(dir, "build.sbt")
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatalf("os.WriteFile(build.sbt) error = %v", err)
	}
	return manifest
}

// sbtIdentityTuple is the (section, name, value, sbt_cross_version) tuple
// the sbt discriminator folds into the hashed name.
func sbtIdentityTuple(row map[string]any) [4]string {
	str := func(key string) string {
		text, _ := row[key].(string)
		return text
	}
	return [4]string{str("section"), str("name"), str("value"), str("sbt_cross_version")}
}

// parseSbtManifestRows runs the real parser engine over one build.sbt file
// and returns its dependency Variable rows.
func parseSbtManifestRows(t *testing.T, manifest string) []map[string]any {
	t.Helper()
	root := filepath.Dir(manifest)
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v", err)
	}
	payload, err := engine.ParsePath(root, manifest, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v", manifest, err)
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("%s produced no variable rows; the sbt parser is not reading it", manifest)
	}
	return rows
}

// sbtContentVersionFixture resolves the checked-in content-version fixture
// build.sbt beside this test's package.
func sbtContentVersionFixture(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	manifest := filepath.Join(filepath.Dir(thisFile), "..", "collector", "repo", "git", "testdata", "content_version", "jvm", "build.sbt")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("content-version fixture build.sbt not found: %v", err)
	}
	return manifest
}

// writeMultiProjectBuildSbt writes a realistic multi-project build.sbt to a
// fresh temp dir and returns its path. The file exercises every
// same-name collision shape: one org:name twice in one section at two
// revisions, % vs %% of one coordinate at one revision, one coordinate in
// the compile and Test sections, and per-project settings rows.
func writeMultiProjectBuildSbt(t *testing.T) string {
	t.Helper()
	const body = `scalaVersion := "2.13.12"

libraryDependencies += "org.typelevel" %% "cats-core" % "2.10.0"
libraryDependencies += "org.typelevel" %% "cats-core" % "2.9.0"
libraryDependencies += "org.typelevel" % "cats-core" % "2.10.0"
libraryDependencies += "com.typesafe" % "config" % "1.4.2"
libraryDependencies += "com.typesafe" % "config" % "1.4.3" % Test

lazy val core = project.settings(
  libraryDependencies += "org.typelevel" %% "cats-kernel" % "2.10.0"
)

lazy val api = project.settings(
  libraryDependencies += "org.scalatest" %% "scalatest" % "3.2.17" % Test
)
`
	dir := t.TempDir()
	manifest := filepath.Join(dir, "build.sbt")
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatalf("os.WriteFile(build.sbt) error = %v", err)
	}
	return manifest
}

func sbtIntFromRow(row map[string]any, key string) int {
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
