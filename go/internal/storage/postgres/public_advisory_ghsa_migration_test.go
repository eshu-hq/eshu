// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

const publicAdvisoryUnsupportedText = "public advisory target source unsupported"

// publicAdvisoryFunctionDefinition matches a definition of the reconcile
// function however the SQL spells the keywords, the whitespace, the identifier
// quoting, or the schema qualifier. It needs the CREATE keyword, so a GRANT or
// REVOKE on the function is not a definition.
var publicAdvisoryFunctionDefinition = regexp.MustCompile(`(?i)create\s+(or\s+replace\s+)?function\s+("?eshu_security"?\s*\.\s*)?"?reconcile_public_advisories_v1"?\s*\(`)

// TestPublicAdvisoryFunctionDefinitionMatchesAnySpelling pins that the marker
// finds a definition spelled in lower case, across lines, with quoted
// identifiers, or with no schema qualifier, so a ghsa arm cannot hide behind a
// different spelling of CREATE OR REPLACE FUNCTION. It also pins that a
// GRANT or REVOKE on the function is not a definition.
func TestPublicAdvisoryFunctionDefinitionMatchesAnySpelling(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"CREATE OR REPLACE FUNCTION eshu_security.reconcile_public_advisories_v1(",
		"create or replace function eshu_security.reconcile_public_advisories_v1(",
		"create or replace function\n  eshu_security.reconcile_public_advisories_v1 (",
		"CREATE FUNCTION eshu_security.reconcile_public_advisories_v1(",
		`CREATE OR REPLACE FUNCTION "eshu_security"."reconcile_public_advisories_v1"(`,
		`CREATE OR REPLACE FUNCTION "eshu_security" . reconcile_public_advisories_v1 (`,
		"CREATE OR REPLACE FUNCTION reconcile_public_advisories_v1(",
	} {
		if !publicAdvisoryFunctionDefinition.MatchString(sql) {
			t.Errorf("definition not matched: %q", sql)
		}
	}
	for _, sql := range []string{
		"REVOKE ALL ON FUNCTION eshu_security.reconcile_public_advisories_v1(text) FROM PUBLIC;",
		"GRANT EXECUTE ON FUNCTION eshu_security.reconcile_public_advisories_v1(text) TO eshu_app;",
	} {
		if publicAdvisoryFunctionDefinition.MatchString(sql) {
			t.Errorf("grant or revoke matched as a definition: %q", sql)
		}
	}
}

// TestPublicAdvisoryReconcileFunctionDoesNotAdmitGHSA holds the #10037 decision:
// a ghsa scope is never public-advisory registered, so no embedded migration may
// add a ghsa arm to the reconcile function. Migration bodies are immutable once
// merged, so an arm that landed could only be undone by a corrective migration.
func TestPublicAdvisoryReconcileFunctionDoesNotAdmitGHSA(t *testing.T) {
	t.Parallel()

	definitions := 0
	for _, def := range migrations.BootstrapDefinitions() {
		location := publicAdvisoryFunctionDefinition.FindStringIndex(def.SQL)
		if location == nil {
			continue
		}
		definitions++
		function := def.SQL[location[0]:]
		if strings.Contains(strings.ToLower(function), "ghsa") {
			t.Errorf("%s defines the reconcile function with a ghsa arm", def.Path)
		}
		if !strings.Contains(function, publicAdvisoryUnsupportedText) {
			t.Errorf("%s defines the reconcile function without the %q refusal", def.Path, publicAdvisoryUnsupportedText)
		}
	}
	if definitions == 0 {
		t.Fatal("no embedded migration defines the reconcile function")
	}
}
