// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestDefaultEngineParsePathNextJSAppRouterComponentRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	pagePath := filepath.Join(repoRoot, "admin-next", "app", "block-lists", "page.tsx")
	layoutPath := filepath.Join(repoRoot, "admin-next", "app", "layout.tsx")
	writeTestFile(
		t,
		pagePath,
		`export default function BlockListsPage() {
  return <BlockListsClient />;
}

function LocalHelper() {
  return null;
}
`,
	)
	writeTestFile(
		t,
		layoutPath,
		`const RootHeader = () => null;

export default function RootLayout({ children }) {
  return <html>{children}</html>;
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	pagePayload, err := engine.ParsePath(repoRoot, pagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(page) error = %v, want nil", err)
	}
	layoutPayload, err := engine.ParsePath(repoRoot, layoutPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(layout) error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, pagePayload, "BlockListsPage"),
		"dead_code_root_kinds",
		[]string{"javascript.nextjs_app_export"},
	)
	localHelperItem := assertFunctionByName(t, pagePayload, "LocalHelper")
	if _, ok := localHelperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for non-exported app helper", localHelperItem["dead_code_root_kinds"])
	}
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, layoutPayload, "RootLayout"),
		"dead_code_root_kinds",
		[]string{"javascript.nextjs_app_export"},
	)
	rootHeaderItem := assertFunctionByName(t, layoutPayload, "RootHeader")
	if _, ok := rootHeaderItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for local layout helper", rootHeaderItem["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathTypeScriptMigrationAndModuleContractRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	migrationPath := filepath.Join(repoRoot, "migrations", "20240705221610_create-status-table.ts")
	rulePath := filepath.Join(repoRoot, "server", "resources", "rules", "spam-email-regex.ts")
	helperPath := filepath.Join(repoRoot, "server", "resources", "rules", "helper.ts")
	writeTestFile(
		t,
		migrationPath,
		`export async function up(db) {
  await db.schema.createTable("request_status").execute();
}

export async function down(db) {
  await db.schema.dropTable("request_status").execute();
}

export function localHelper() {
  return "no";
}
`,
	)
	writeTestFile(
		t,
		rulePath,
		`export const RULE_NAME = "spam-email-regex";

export const validate = (req) => Boolean(req);

export const execute = async (req) => {
  return { name: RULE_NAME, hadErrors: false };
};

export const localHelper = () => "no";
`,
	)
	writeTestFile(
		t,
		helperPath,
		`export const validate = (req) => Boolean(req);
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	migrationPayload, err := engine.ParsePath(repoRoot, migrationPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(migration) error = %v, want nil", err)
	}
	rulePayload, err := engine.ParsePath(repoRoot, rulePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(rule) error = %v, want nil", err)
	}
	helperPayload, err := engine.ParsePath(repoRoot, helperPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(helper) error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, migrationPayload, "up"),
		"dead_code_root_kinds",
		[]string{"javascript.node_migration_export"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, migrationPayload, "down"),
		"dead_code_root_kinds",
		[]string{"javascript.node_migration_export"},
	)
	migrationHelperItem := assertFunctionByName(t, migrationPayload, "localHelper")
	if _, ok := migrationHelperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for non-migration helper", migrationHelperItem["dead_code_root_kinds"])
	}
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, rulePayload, "validate"),
		"dead_code_root_kinds",
		[]string{"typescript.module_contract_export"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, rulePayload, "execute"),
		"dead_code_root_kinds",
		[]string{"typescript.module_contract_export"},
	)
	ruleHelperItem := assertFunctionByName(t, rulePayload, "localHelper")
	if _, ok := ruleHelperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for non-contract helper", ruleHelperItem["dead_code_root_kinds"])
	}
	orphanValidateItem := assertFunctionByName(t, helperPayload, "validate")
	if _, ok := orphanValidateItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent without module contract marker", orphanValidateItem["dead_code_root_kinds"])
	}
}
