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
		expressPath,
		`import express from "express";

const router = express.Router();

function login(req, res) {
  return res.send("ok");
}

const createVideo = (req, res) => res.send("ok");

router.get("/auth/login", login);
router.post("/", createVideo);
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
}
