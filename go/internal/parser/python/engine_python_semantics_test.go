// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPythonFastAPISemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "fastapi_app.py")
	writeTestFile(
		t,
		filePath,
		`from fastapi import APIRouter, FastAPI, Request

app: FastAPI = FastAPI()
router: APIRouter = APIRouter(prefix="/api")

@app.get("/health")
def health():
    return {"ok": True}

@router.post("/predict")
async def predict(_request: Request):
    return {"score": 1.0}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertFrameworksEqual(t, got, "fastapi")
	parsertest.AssertNestedStringSliceEqual(t, got, "fastapi", "route_methods", []string{"GET", "POST"})
	parsertest.AssertNestedStringSliceEqual(t, got, "fastapi", "route_paths", []string{"/health", "/api/predict"})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "fastapi", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "health"},
		{"method": "POST", "path": "/api/predict", "handler": "predict"},
	})
	parsertest.AssertNestedStringSliceEqual(t, got, "fastapi", "server_symbols", []string{"app", "router"})
}

func TestDefaultEngineParsePathPythonFlaskSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "flask_app.py")
	writeTestFile(
		t,
		filePath,
		`from lib.factory import create_app

app = create_app(__name__)

@app.route("/health")
def health():
    return "ok"

@app.route("/proxy", methods=["GET", "POST"])
def proxy():
    return "proxied"
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertFrameworksEqual(t, got, "flask")
	parsertest.AssertNestedStringSliceEqual(t, got, "flask", "route_methods", []string{"GET", "POST"})
	parsertest.AssertNestedStringSliceEqual(t, got, "flask", "route_paths", []string{"/health", "/proxy"})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "flask", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "health"},
		{"method": "GET", "path": "/proxy", "handler": "proxy"},
		{"method": "POST", "path": "/proxy", "handler": "proxy"},
	})
	parsertest.AssertNestedStringSliceEqual(t, got, "flask", "server_symbols", []string{"app"})
}

func TestDefaultEngineParsePathPythonORMMappings(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sqlAlchemyPath := filepath.Join(repoRoot, "sqlalchemy_models.py")
	writeTestFile(
		t,
		sqlAlchemyPath,
		`from sqlalchemy.orm import DeclarativeBase

class Base(DeclarativeBase):
    pass

class User(Base):
    __tablename__ = "users"
`,
	)

	djangoPath := filepath.Join(repoRoot, "django_models.py")
	writeTestFile(
		t,
		djangoPath,
		`from django.db import models

class AuditEvent(models.Model):
    name = models.CharField(max_length=255)

    class Meta:
        db_table = "audit.events"
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	sqlAlchemyPayload, err := engine.ParsePath(repoRoot, sqlAlchemyPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", sqlAlchemyPath, err)
	}
	assertORMMappingsEqual(
		t,
		sqlAlchemyPayload,
		[]map[string]any{{
			"class_name":        "User",
			"class_line_number": 6,
			"table_name":        "users",
			"framework":         "sqlalchemy",
			"line_number":       7,
		}},
	)

	djangoPayload, err := engine.ParsePath(repoRoot, djangoPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", djangoPath, err)
	}
	assertORMMappingsEqual(
		t,
		djangoPayload,
		[]map[string]any{{
			"class_name":        "AuditEvent",
			"class_line_number": 3,
			"table_name":        "audit.events",
			"framework":         "django",
			"line_number":       7,
		}},
	)
}

func TestDefaultEngineParsePathPythonUnknownRouteDecoratorRemainsUnclassified(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "custom_router.py")
	writeTestFile(
		t,
		filePath,
		`class Router:
    def route(self, _path):
        def decorator(func):
            return func
        return decorator

router = Router()

@router.route("/health")
def health():
    return "ok"
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertFrameworksEqual(t, got)
}

func TestDefaultEngineParsePathPythonDecoratedFunctionsEmitDecoratorMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "decorated.py")
	writeTestFile(
		t,
		filePath,
		`def traced(func):
    return func

@traced
def greet(name):
    return name
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	greet := parsertest.AssertBucketItemByName(t, got, "functions", "greet")
	decorators, ok := greet["decorators"].([]string)
	if !ok {
		t.Fatalf(`functions["greet"]["decorators"] = %T, want []string`, greet["decorators"])
	}
	if !reflect.DeepEqual(decorators, []string{"@traced"}) {
		t.Fatalf(`functions["greet"]["decorators"] = %#v, want []string{"@traced"}`, decorators)
	}
}

func TestDefaultEngineParsePathPythonAsyncFunctionsEmitAsyncFlag(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "async_fn.py")
	writeTestFile(
		t,
		filePath,
		`async def fetch_remote():
    return "ok"
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	fetchRemote := parsertest.AssertBucketItemByName(t, got, "functions", "fetch_remote")
	asyncFlag, ok := fetchRemote["async"].(bool)
	if !ok {
		t.Fatalf(`functions["fetch_remote"]["async"] = %T, want bool`, fetchRemote["async"])
	}
	if !asyncFlag {
		t.Fatalf(`functions["fetch_remote"]["async"] = false, want true`)
	}
}

func TestDefaultEngineParsePathPythonEmitsTypeAnnotationsBucket(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "annotations.py")
	writeTestFile(
		t,
		filePath,
		`def greet(name: str, excited: bool = False) -> str:
    return name
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	annotations, ok := got["type_annotations"].([]map[string]any)
	if !ok {
		t.Fatalf(`type_annotations = %T, want []map[string]any`, got["type_annotations"])
	}
	want := []map[string]any{
		{
			"name":            "excited",
			"line_number":     1,
			"type":            "bool",
			"annotation_kind": "parameter",
			"context":         "greet",
			"lang":            "python",
		},
		{
			"name":            "greet",
			"line_number":     1,
			"type":            "str",
			"annotation_kind": "return",
			"context":         "greet",
			"lang":            "python",
		},
		{
			"name":            "name",
			"line_number":     1,
			"type":            "str",
			"annotation_kind": "parameter",
			"context":         "greet",
			"lang":            "python",
		},
	}
	if !reflect.DeepEqual(annotations, want) {
		t.Fatalf("type_annotations = %#v, want %#v", annotations, want)
	}
}

func TestDefaultEngineParsePathPythonRichSemanticMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "rich.py")
	writeTestFile(
		t,
		filePath,
		`class Greeter:
    """Greeter docs."""

    def greet(self, name):
        """Greet a person."""
        if name:
            for letter in name:
                print(letter)
        return name
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	classItem := parsertest.AssertBucketItemByName(t, got, "classes", "Greeter")
	parsertest.AssertStringFieldValue(t, classItem, "docstring", "Greeter docs.")

	functionItem := parsertest.AssertBucketItemByName(t, got, "functions", "greet")
	parsertest.AssertStringFieldValue(t, functionItem, "docstring", "Greet a person.")
	parsertest.AssertIntFieldValue(t, functionItem, "cyclomatic_complexity", 3)
}

func assertORMMappingsEqual(t *testing.T, payload map[string]any, want []map[string]any) {
	t.Helper()

	got, ok := payload["orm_table_mappings"].([]map[string]any)
	if !ok {
		t.Fatalf("orm_table_mappings = %T, want []map[string]any", payload["orm_table_mappings"])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orm_table_mappings = %#v, want %#v", got, want)
	}
}
