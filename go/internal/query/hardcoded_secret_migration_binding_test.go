// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/secret/detect"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

const secretLinesMigrationName = "content_file_secret_lines"

// secretLinesFunctionPattern finds one CREATE OR REPLACE FUNCTION
// eshu_secret_line_findings statement, through its closing $fn$;.
var secretLinesFunctionPattern = regexp.MustCompile(`(?s)CREATE OR REPLACE FUNCTION eshu_secret_line_findings\(.*?\$fn\$;`)

// secretLinesMigrationSQL returns the effective schema of the hardcoded-secret
// side table: migration 138 (the table, its suppressed column, the triggers,
// and readiness) with its eshu_secret_line_findings replaced by the latest
// migration that redefines the function (234, #8833). Bootstrap applies every
// file in path order, so the latest definition is the one that runs.
func secretLinesMigrationSQL(t testing.TB) string {
	t.Helper()
	var table, latest string
	for _, def := range migrations.BootstrapDefinitions() {
		if def.Name == secretLinesMigrationName {
			table = def.SQL
		}
		if function := secretLinesFunctionPattern.FindString(def.SQL); function != "" {
			latest = function
		}
	}
	if table == "" {
		t.Fatalf("embedded migration %q not found; #7125 needs 138_%s.sql", secretLinesMigrationName, secretLinesMigrationName)
	}
	if latest == "" {
		t.Fatal("no migration defines eshu_secret_line_findings")
	}
	return secretLinesFunctionPattern.ReplaceAllLiteralString(table, latest)
}

var (
	secretLinesCaseArmPattern   = regexp.MustCompile(`WHEN\s+lines\.line_text\s+~\*\s+'((?:[^']|'')*)'\s+THEN\s+'([a-z_]+)'`)
	secretLinesGeneratedPattern = regexp.MustCompile(`(?s)GENERATED\s+ALWAYS\s+AS\s*\((.*?)\)\s*STORED`)
)

// secretLineCaseArm is one WHEN regex THEN kind arm of the classification CASE.
type secretLineCaseArm struct{ regex, kind string }

func secretLineCaseArms(sqlText string) []secretLineCaseArm {
	matches := secretLinesCaseArmPattern.FindAllStringSubmatch(sqlText, -1)
	arms := make([]secretLineCaseArm, 0, len(matches))
	for _, match := range matches {
		arms = append(arms, secretLineCaseArm{
			regex: strings.ReplaceAll(match[1], "''", "'"),
			kind:  match[2],
		})
	}
	return arms
}

func collapseSQLSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// verifyHardcodedSecretMigrationBinding checks that the migration's derivation
// function and generated suppression column still say what the Go sources of
// truth say. pattern is hardcodedSecretSQLPattern, predicate is
// hardcodedSecretSQLSuppressionPredicate(), and caseArms is the classification
// CASE frozen in the legacy golden builder. It returns an error naming the
// first divergence so a Go-only edit of policy turns the build red instead of
// silently diverging from the stored derivation (#7125).
func verifyHardcodedSecretMigrationBinding(
	migrationSQL, pattern, predicate string,
	caseArms []secretLineCaseArm,
) error {
	escaped := strings.ReplaceAll(pattern, "'", "''")
	if got := strings.Count(migrationSQL, "~* '"+escaped+"'"); got != 2 {
		return fmt.Errorf("migration applies the secret pattern %d times, want 2 (file prefilter and line filter)", got)
	}
	// The marker arm's place in the gate (#8833): a case-sensitive substring
	// test OR'd with the pattern, at file and line level.
	for _, subject := range []string{"file_content", "lines.line_text"} {
		gate := "(strpos(" + subject + ", '" + detect.MarkerPrefix + "') > 0 OR " + subject + " ~* '" + escaped + "')"
		if !strings.Contains(migrationSQL, gate) {
			return fmt.Errorf("migration does not gate %s on the marker prefix OR the pattern", subject)
		}
	}
	migrationArms := secretLineCaseArms(migrationSQL)
	if !slices.Equal(migrationArms, caseArms) {
		return fmt.Errorf("migration CASE arms = %#v, want the legacy classification %#v", migrationArms, caseArms)
	}
	generated := secretLinesGeneratedPattern.FindStringSubmatch(migrationSQL)
	if generated == nil {
		return fmt.Errorf("migration has no GENERATED ALWAYS AS (...) STORED suppressed column")
	}
	want := strings.NewReplacer("f.relative_path", "relative_path", "lines.line_text", "line_text").Replace(predicate)
	if got := collapseSQLSpace(generated[1]); got != collapseSQLSpace(want) {
		return fmt.Errorf("migration suppressed expression = %q, want %q", got, collapseSQLSpace(want))
	}
	return nil
}

func legacyCaseArms(t testing.TB) []secretLineCaseArm {
	t.Helper()
	query, _ := legacyHardcodedSecretInvestigationQuery(codequery.HardcodedSecretInvestigationRequest{})
	arms := secretLineCaseArms(query)
	if len(arms) != len(detect.ArmSources()) {
		t.Fatalf("legacy golden CASE has %d arms, want %d", len(arms), len(detect.ArmSources()))
	}
	return arms
}

func TestHardcodedSecretMigrationIsBoundToGoSourcesOfTruth(t *testing.T) {
	t.Parallel()

	err := verifyHardcodedSecretMigrationBinding(
		secretLinesMigrationSQL(t),
		hardcodedSecretSQLPattern,
		hardcodedSecretSQLSuppressionPredicate(),
		legacyCaseArms(t),
	)
	if err != nil {
		t.Fatalf("migration is not bound to the Go policy: %v", err)
	}
}

// TestHardcodedSecretMigrationBindingFailsOnGoOnlyPolicyEdits is the seeded
// violation for the binding guard: each Go-only edit of policy must be
// rejected, and the unedited inputs must pass.
func TestHardcodedSecretMigrationBindingFailsOnGoOnlyPolicyEdits(t *testing.T) {
	t.Parallel()

	migrationSQL := secretLinesMigrationSQL(t)
	arms := legacyCaseArms(t)
	pattern := hardcodedSecretSQLPattern
	predicate := hardcodedSecretSQLSuppressionPredicate()
	if err := verifyHardcodedSecretMigrationBinding(migrationSQL, pattern, predicate, arms); err != nil {
		t.Fatalf("clean inputs must pass: %v", err)
	}

	changedArms := slices.Clone(arms)
	apiKeyArm := slices.IndexFunc(changedArms, func(arm secretLineCaseArm) bool { return strings.Contains(arm.regex, "apikey") })
	if apiKeyArm < 0 {
		t.Fatal("no CASE arm names apikey; the seeded edit has nothing to change")
	}
	changedArms[apiKeyArm].regex = strings.Replace(changedArms[apiKeyArm].regex, "apikey", "apikey|bearer", 1)
	for name, run := range map[string]func() error{
		"pattern gains a branch in Go only": func() error {
			return verifyHardcodedSecretMigrationBinding(migrationSQL, pattern+"|glpat-[A-Za-z0-9]{20}", predicate, arms)
		},
		"suppression fragment added in Go only": func() error {
			return verifyHardcodedSecretMigrationBinding(migrationSQL, pattern,
				predicate+" OR "+hardcodedSecretSQLContains("lines.line_text", "sample"), arms)
		},
		"suppression fragment removed in Go only": func() error {
			return verifyHardcodedSecretMigrationBinding(migrationSQL, pattern,
				strings.Replace(predicate, " OR "+hardcodedSecretSQLContains("lines.line_text", "changeme"), "", 1), arms)
		},
		"classification arm changed in Go only": func() error {
			return verifyHardcodedSecretMigrationBinding(migrationSQL, pattern, predicate, changedArms)
		},
		"migration drops the marker gate": func() error {
			trimmed := strings.Replace(migrationSQL, "strpos(lines.line_text, '"+detect.MarkerPrefix+"') > 0 OR ", "", 1)
			return verifyHardcodedSecretMigrationBinding(trimmed, pattern, predicate, arms)
		},
		"migration drops the file prefilter": func() error {
			trimmed := strings.Replace(migrationSQL, "file_content ~* '"+strings.ReplaceAll(pattern, "'", "''")+"'", "TRUE", 1)
			return verifyHardcodedSecretMigrationBinding(trimmed, pattern, predicate, arms)
		},
	} {
		if err := run(); err == nil {
			t.Fatalf("%s: binding guard accepted the divergence", name)
		}
	}
}

func TestHardcodedSecretMigrationKindsMatchRiskClassification(t *testing.T) {
	t.Parallel()

	arms := secretLineCaseArms(secretLinesMigrationSQL(t))
	if len(arms) == 0 {
		t.Fatal("migration has no CASE arms")
	}
	for _, arm := range arms {
		if confidence, severity := hardcodedSecretRisk(arm.kind); confidence == "low" || severity == "medium" {
			t.Fatalf("migration kind %q falls through hardcodedSecretRisk to the default tier (%s/%s)", arm.kind, confidence, severity)
		}
	}
}

// TestHardcodedSecretMigrationFencesUnchangedRowsBeforeDerivation pins the
// measured shape of the UPDATE trigger (#7125 profile): the changed set is a
// MATERIALIZED CTE, so the planner cannot push the derivation function's inlined
// file-level regex below the unchanged-row anti join. Without the fence an
// unchanged re-upsert paid the regex on every file in the batch (auto_explain
// showed the regex filter on the new-rows scan). The fence is part of the
// contract here because removing it silently restores that cost.
func TestHardcodedSecretMigrationFencesUnchangedRowsBeforeDerivation(t *testing.T) {
	t.Parallel()

	migrationSQL := secretLinesMigrationSQL(t)
	start := strings.Index(migrationSQL, "FUNCTION content_file_secret_lines_derive_updated()")
	if start < 0 {
		t.Fatal("migration has no content_file_secret_lines_derive_updated function")
	}
	body := migrationSQL[start:]
	if end := strings.Index(body, "DROP TRIGGER"); end > 0 {
		body = body[:end]
	}
	fence := strings.Index(body, "WITH changed AS MATERIALIZED")
	derive := strings.Index(body, "eshu_secret_line_findings(")
	if fence < 0 || derive < 0 || fence > derive {
		t.Fatalf("update trigger must derive from a MATERIALIZED changed set (fence at %d, derive at %d)", fence, derive)
	}
	if strings.Count(body, "eshu_secret_line_findings(") != 1 || !strings.Contains(body, "FROM changed c CROSS JOIN LATERAL eshu_secret_line_findings(c.content)") {
		t.Fatal("update trigger must call the derivation function only over the materialized changed set")
	}
}
