package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestJavaScriptExpressServerSymbols(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		express map[string]any
		want    []string
	}{
		{
			name: "typed server symbols",
			express: map[string]any{
				"server_symbols": []string{"app", "router"},
			},
			want: []string{"app", "router"},
		},
		{
			name: "missing server symbols",
			express: map[string]any{
				"route_methods": []string{"GET"},
			},
			want: nil,
		},
		{
			name: "wrong server symbols shape",
			express: map[string]any{
				"server_symbols": []any{"app"},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := javaScriptExpressServerSymbols(tt.express)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("javaScriptExpressServerSymbols(%#v) = %#v, want %#v", tt.express, got, tt.want)
			}
		})
	}
}

func TestDefaultEngineParsePathJavaScriptEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	nextPath := filepath.Join(repoRoot, "app", "api", "health", "route.ts")
	nextTSXPath := filepath.Join(repoRoot, "app", "api", "profile", "route.tsx")
	nextEnumPath := filepath.Join(repoRoot, "app", "api", "enum", "route.ts")
	expressPath := filepath.Join(repoRoot, "server", "routes.ts")
	writeTestFile(
		t,
		nextPath,
		`export async function GET() {
  return Response.json({ ok: true });
}

async function helper() {
  return Response.json({ ok: true });
}
`,
	)
	writeTestFile(
		t,
		nextTSXPath,
		`export const POST = async () => {
  return Response.json({ ok: true });
};

const localHelper = () => Response.json({ ok: true });
`,
	)
	writeTestFile(
		t,
		nextEnumPath,
		`export enum GET {
  Read = "read",
}
`,
	)
	writeTestFile(
		t,
		expressPath,
		`import express from "express";

const router = express.Router();

function login(req, res) {
  return res.send("ok");
}

const createVideo = (req, res) => res.send("ok");

function requireAuth(req, res, next) {
  return next();
}

function updateProfile(req, res) {
  return res.send("ok");
}

function arrayMiddleware(req, res, next) {
  return next();
}

function listUsers(req, res) {
  return res.send("ok");
}

router.get("/auth/login", login);
router.post("/", createVideo);
router.put("/profile", requireAuth, updateProfile);
router.get("/users", [arrayMiddleware], listUsers);
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	nextPayload, err := engine.ParsePath(repoRoot, nextPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(next) error = %v, want nil", err)
	}
	expressPayload, err := engine.ParsePath(repoRoot, expressPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(express) error = %v, want nil", err)
	}
	nextTSXPayload, err := engine.ParsePath(repoRoot, nextTSXPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(next tsx) error = %v, want nil", err)
	}
	nextEnumPayload, err := engine.ParsePath(repoRoot, nextEnumPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(next enum) error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, nextPayload, "GET"),
		"dead_code_root_kinds",
		[]string{"javascript.nextjs_route_export"},
	)
	helperItem := assertFunctionByName(t, nextPayload, "helper")
	if _, ok := helperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for non-exported route helper", helperItem["dead_code_root_kinds"])
	}
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, nextTSXPayload, "POST"),
		"dead_code_root_kinds",
		[]string{"javascript.nextjs_route_export"},
	)
	localHelperItem := assertFunctionByName(t, nextTSXPayload, "localHelper")
	if _, ok := localHelperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for non-exported TSX route helper", localHelperItem["dead_code_root_kinds"])
	}
	enumItem := assertBucketItemByName(t, nextEnumPayload, "enums", "GET")
	if _, ok := enumItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for route enum", enumItem["dead_code_root_kinds"])
	}
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "login"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "createVideo"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "requireAuth"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "updateProfile"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "arrayMiddleware"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "listUsers"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
}

func TestDefaultEngineParsePathTypeScriptDeadCodeRootsReuseJavaScriptFamilyPolicy(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("deadcode", "typescript")
	expressPath := filepath.Join(repoRoot, "src", "service.ts")
	nextPath := filepath.Join(repoRoot, "src", "app", "api", "accounts", "route.ts")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	expressPayload, err := engine.ParsePath(repoRoot, expressPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(express ts) error = %v, want nil", err)
	}
	nextPayload, err := engine.ParsePath(repoRoot, nextPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(next ts) error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "saveAccount"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, nextPayload, "GET"),
		"dead_code_root_kinds",
		[]string{"javascript.nextjs_route_export"},
	)
	localRouteHelperItem := assertFunctionByName(t, nextPayload, "localRouteHelper")
	if _, ok := localRouteHelperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for TypeScript local route helper", localRouteHelperItem["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathTSXDeadCodeRootsReuseJavaScriptFamilyPolicy(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("deadcode", "tsx")
	nextPath := filepath.Join(repoRoot, "src", "app", "api", "profile", "route.tsx")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	nextPayload, err := engine.ParsePath(repoRoot, nextPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(next tsx) error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, nextPayload, "POST"),
		"dead_code_root_kinds",
		[]string{"javascript.nextjs_route_export"},
	)
	localRouteHelperItem := assertFunctionByName(t, nextPayload, "localRouteHelper")
	if _, ok := localRouteHelperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for TSX local route helper", localRouteHelperItem["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathJavaScriptMarksCommonJSDefaultExportClassMethods(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "core.js")
	writeTestFile(
		t,
		filePath,
		`'use strict';

const internals = {};

exports = module.exports = internals.Core = class {
    registerServer(server) {
        this.instances.add(server);
    }

    start() {
        return true;
    }
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "registerServer"),
		"dead_code_root_kinds",
		[]string{"javascript.commonjs_default_export"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "start"),
		"dead_code_root_kinds",
		[]string{"javascript.commonjs_default_export"},
	)
}

func TestDefaultEngineParsePathJavaScriptDoesNotRootNestedCommonJSDefaultExportClassMethods(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "factory.js")
	writeTestFile(
		t,
		filePath,
		`'use strict';

module.exports = factory(class Internal {
    cleanup() {
        return true;
    }
});
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if cleanup := assertFunctionByName(t, got, "cleanup"); cleanup["dead_code_root_kinds"] != nil {
		t.Fatalf("cleanup dead_code_root_kinds = %#v, want nil", cleanup["dead_code_root_kinds"])
	}
}

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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	pagePayload, err := engine.ParsePath(repoRoot, pagePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(page) error = %v, want nil", err)
	}
	layoutPayload, err := engine.ParsePath(repoRoot, layoutPath, false, Options{})
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	migrationPayload, err := engine.ParsePath(repoRoot, migrationPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(migration) error = %v, want nil", err)
	}
	rulePayload, err := engine.ParsePath(repoRoot, rulePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(rule) error = %v, want nil", err)
	}
	helperPayload, err := engine.ParsePath(repoRoot, helperPath, false, Options{})
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
