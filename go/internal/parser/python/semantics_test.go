// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"reflect"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// parsePythonForTest parses source into a tree-sitter root node for the
// AST-node semantics tests. The caller closes nothing because the tree is kept
// alive for the duration of the test through the returned closer.
func parsePythonForTest(t *testing.T, source string) (*tree_sitter.Node, []byte, func()) {
	t.Helper()
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	bytes := []byte(source)
	tree := parser.Parse(bytes, nil)
	if tree == nil {
		parser.Close()
		t.Fatalf("Parse() returned nil tree")
	}
	root := tree.RootNode()
	return root, bytes, func() {
		tree.Close()
		parser.Close()
	}
}

func TestBuildPythonFrameworkSemanticsFastAPIRouterPrefix(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from fastapi import APIRouter, FastAPI

app: FastAPI = FastAPI()
router: APIRouter = APIRouter(prefix="/api")

@app.get("/health")
def health():
    return {"ok": True}

@router.post("/predict")
async def predict():
    return {"score": 1.0}
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if !reflect.DeepEqual(frameworks, []string{"fastapi"}) {
		t.Fatalf("frameworks = %#v, want [fastapi]", frameworks)
	}
	fastapi, _ := got["fastapi"].(map[string]any)
	if fastapi == nil {
		t.Fatalf("fastapi semantics missing: %#v", got)
	}
	assertStringSlice(t, fastapi, "route_methods", []string{"GET", "POST"})
	assertStringSlice(t, fastapi, "route_paths", []string{"/health", "/api/predict"})
	assertStringSlice(t, fastapi, "server_symbols", []string{"app", "router"})
	assertRouteEntries(t, fastapi, []map[string]string{
		{"method": "GET", "path": "/health", "handler": "health"},
		{"method": "POST", "path": "/api/predict", "handler": "predict"},
	})
}

func TestBuildPythonFrameworkSemanticsFlaskMultipleMethods(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from flask import Flask

app = Flask(__name__)

@app.route("/health")
def health():
    return "ok"

@app.route("/proxy", methods=["GET", "POST"])
def proxy():
    return "proxied"
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	flask, _ := got["flask"].(map[string]any)
	if flask == nil {
		t.Fatalf("flask semantics missing: %#v", got)
	}
	assertStringSlice(t, flask, "route_methods", []string{"GET", "POST"})
	assertStringSlice(t, flask, "route_paths", []string{"/health", "/proxy"})
	assertStringSlice(t, flask, "server_symbols", []string{"app"})
	assertRouteEntries(t, flask, []map[string]string{
		{"method": "GET", "path": "/health", "handler": "health"},
		{"method": "GET", "path": "/proxy", "handler": "proxy"},
		{"method": "POST", "path": "/proxy", "handler": "proxy"},
	})
}

// TestBuildPythonFrameworkSemanticsOrphanRouteStaysUnbound proves that a route
// decorator with no following def (a syntax-error orphan in the AST) still emits
// the route but never binds a fabricated handler (#2788).
func TestBuildPythonFrameworkSemanticsOrphanRouteStaysUnbound(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from fastapi import FastAPI

app = FastAPI()

@app.get("/health")
@auth_required
async def read_health():
    return {"ok": True}

@app.post("/orphan")
x = 1

@app.put("/update")
def update_item():
    return {"ok": True}
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	fastapi, _ := got["fastapi"].(map[string]any)
	if fastapi == nil {
		t.Fatalf("fastapi semantics missing: %#v", got)
	}
	assertRouteEntries(t, fastapi, []map[string]string{
		{"method": "GET", "path": "/health", "handler": "read_health"},
		{"method": "POST", "path": "/orphan"},
		{"method": "PUT", "path": "/update", "handler": "update_item"},
	})
}

// TestBuildPythonFrameworkSemanticsUnknownDecoratorRemainsUnclassified proves a
// custom `.route` method on a local object is not a Flask server unless an
// allowed server symbol is assigned from Flask/create_app.
func TestBuildPythonFrameworkSemanticsUnknownDecoratorRemainsUnclassified(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `class Router:
    def route(self, _path):
        def decorator(func):
            return func
        return decorator

router = Router()

@router.route("/health")
def health():
    return "ok"
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty", frameworks)
	}
}

// TestBuildPythonORMTableMappingsInheritedModels proves SQLAlchemy
// __tablename__ and Django Meta.db_table are extracted from class-body
// assignment nodes for inherited models.
func TestBuildPythonORMTableMappingsInheritedModels(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from sqlalchemy.orm import DeclarativeBase
from django.db import models

class Base(DeclarativeBase):
    pass

class User(Base):
    __tablename__ = "users"

class AuditEvent(models.Model):
    name = models.CharField(max_length=255)

    class Meta:
        db_table = "audit.events"
`)
	defer closer()

	got := buildPythonORMTableMappings(root, source)
	want := []map[string]any{
		{
			"class_name":        "User",
			"class_line_number": 7,
			"table_name":        "users",
			"framework":         "sqlalchemy",
			"line_number":       8,
		},
		{
			"class_name":        "AuditEvent",
			"class_line_number": 10,
			"table_name":        "audit.events",
			"framework":         "django",
			"line_number":       14,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orm_table_mappings = %#v, want %#v", got, want)
	}
}

// TestPythonModuleAllNamesAcceptsListAndTuple proves __all__ names are read from
// the assignment value AST node for both list and tuple literals, keeping only
// identifier-shaped string entries.
func TestPythonModuleAllNamesAcceptsListAndTuple(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{name: "list", source: "__all__ = [\"alpha\", \"beta\", \"/not-an-ident\"]\n"},
		{name: "tuple", source: "__all__ = ('alpha', 'beta', '/not-an-ident')\n"},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			root, source, closer := parsePythonForTest(t, testCase.source)
			defer closer()

			names := buildPythonPrimaryIndexes(root, source).moduleAllNames
			if _, ok := names["alpha"]; !ok {
				t.Fatalf("__all__ names missing alpha: %#v", names)
			}
			if _, ok := names["beta"]; !ok {
				t.Fatalf("__all__ names missing beta: %#v", names)
			}
			if _, ok := names["/not-an-ident"]; ok {
				t.Fatalf("__all__ names should drop non-identifier entries: %#v", names)
			}
			if len(names) != 2 {
				t.Fatalf("__all__ names = %#v, want exactly alpha and beta", names)
			}
		})
	}
}

// TestPythonModuleAllNamesAcceptsConcatenatedLiterals proves __all__ names are
// collected from both operands of a concatenated literal RHS (a binary_operator
// node), including nested concatenation and mixed list/tuple containers, while
// ignoring non-literal operands such as `base.__all__`.
func TestPythonModuleAllNamesAcceptsConcatenatedLiterals(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "two lists",
			source: "__all__ = [\"foo\"] + [\"bar\"]\n",
			want:   []string{"foo", "bar"},
		},
		{
			name:   "nested mixed containers",
			source: "__all__ = [\"a\"] + (\"b\", \"c\") + [\"d\"]\n",
			want:   []string{"a", "b", "c", "d"},
		},
		{
			name:   "ignores non-literal operand",
			source: "__all__ = [\"a\"] + base.__all__ + [\"b\"]\n",
			want:   []string{"a", "b"},
		},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			root, source, closer := parsePythonForTest(t, testCase.source)
			defer closer()

			names := buildPythonPrimaryIndexes(root, source).moduleAllNames
			if len(names) != len(testCase.want) {
				t.Fatalf("__all__ names = %#v, want %v", names, testCase.want)
			}
			for _, want := range testCase.want {
				if _, ok := names[want]; !ok {
					t.Fatalf("__all__ names missing %q: %#v", want, names)
				}
			}
		})
	}
}

// TestPythonScriptMainGuardConditionAST proves the script-main guard is detected
// from the if-statement condition AST in both operand orders and parenthesized
// form, and that an inequality guard is rejected.
func TestPythonScriptMainGuardConditionAST(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   bool
	}{
		{name: "direct", source: "if __name__ == \"__main__\":\n    run()\n", want: true},
		{name: "reversed", source: "if \"__main__\" == __name__:\n    run()\n", want: true},
		{name: "parenthesized", source: "if (\"__main__\" == __name__):\n    run()\n", want: true},
		{name: "inequality", source: "if __name__ != \"__main__\":\n    run()\n", want: false},
		{name: "other", source: "if ready:\n    run()\n", want: false},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			root, source, closer := parsePythonForTest(t, testCase.source)
			defer closer()

			var found bool
			walkNamed(root, func(node *tree_sitter.Node) {
				if node.Kind() == "if_statement" && pythonIsScriptMainGuard(node, source) {
					found = true
				}
			})
			if found != testCase.want {
				t.Fatalf("pythonIsScriptMainGuard = %v, want %v", found, testCase.want)
			}
		})
	}
}

func assertStringSlice(t *testing.T, semantics map[string]any, key string, want []string) {
	t.Helper()
	got, ok := semantics[key].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", key, semantics[key])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", key, got, want)
	}
}

func assertRouteEntries(t *testing.T, semantics map[string]any, want []map[string]string) {
	t.Helper()
	got, ok := semantics["route_entries"].([]map[string]string)
	if !ok {
		t.Fatalf("route_entries = %T, want []map[string]string", semantics["route_entries"])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("route_entries = %#v, want %#v", got, want)
	}
}
