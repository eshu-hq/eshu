package parser

import (
	"path/filepath"
	"reflect"
	"testing"
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertFrameworksEqual(t, got, "fastapi")
	assertNestedStringSliceEqual(t, got, "fastapi", "route_methods", []string{"GET", "POST"})
	assertNestedStringSliceEqual(t, got, "fastapi", "route_paths", []string{"/health", "/api/predict"})
	assertNestedRouteEntriesEqual(t, got, "fastapi", []map[string]string{
		{"method": "GET", "path": "/health"},
		{"method": "POST", "path": "/api/predict"},
	})
	assertNestedStringSliceEqual(t, got, "fastapi", "server_symbols", []string{"app", "router"})
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertFrameworksEqual(t, got, "flask")
	assertNestedStringSliceEqual(t, got, "flask", "route_methods", []string{"GET", "POST"})
	assertNestedStringSliceEqual(t, got, "flask", "route_paths", []string{"/health", "/proxy"})
	assertNestedRouteEntriesEqual(t, got, "flask", []map[string]string{
		{"method": "GET", "path": "/health"},
		{"method": "GET", "path": "/proxy"},
		{"method": "POST", "path": "/proxy"},
	})
	assertNestedStringSliceEqual(t, got, "flask", "server_symbols", []string{"app"})
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	sqlAlchemyPayload, err := engine.ParsePath(repoRoot, sqlAlchemyPath, false, Options{})
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

	djangoPayload, err := engine.ParsePath(repoRoot, djangoPath, false, Options{})
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertFrameworksEqual(t, got)
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	greet := assertFunctionByName(t, got, "greet")
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	fetchRemote := assertFunctionByName(t, got, "fetch_remote")
	asyncFlag, ok := fetchRemote["async"].(bool)
	if !ok {
		t.Fatalf(`functions["fetch_remote"]["async"] = %T, want bool`, fetchRemote["async"])
	}
	if !asyncFlag {
		t.Fatalf(`functions["fetch_remote"]["async"] = false, want true`)
	}
}

func TestDefaultEngineParsePathPythonEmitsDottedCallMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "dotted_calls.py")
	writeTestFile(
		t,
		filePath,
		`class Client:
    def service(self):
        return self

client = Client()
client.service.request()
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

	call := assertBucketItemByName(t, got, "function_calls", "request")
	assertStringFieldValue(t, call, "full_name", "client.service.request")
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	classItem := assertBucketItemByName(t, got, "classes", "Greeter")
	assertStringFieldValue(t, classItem, "docstring", "Greeter docs.")

	functionItem := assertFunctionByName(t, got, "greet")
	assertStringFieldValue(t, functionItem, "docstring", "Greet a person.")
	assertIntFieldValue(t, functionItem, "cyclomatic_complexity", 3)
}

func TestDefaultEngineParsePathPythonModuleDocstringEmitsModuleMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "module_docstring.py")
	writeTestFile(
		t,
		filePath,
		`"""Utilities for payments."""

def ping():
    return True
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

	moduleItem := assertBucketItemByName(t, got, "modules", "module_docstring")
	assertStringFieldValue(t, moduleItem, "docstring", "Utilities for payments.")
	assertStringFieldValue(t, moduleItem, "lang", "python")
}

func TestDefaultEngineParsePathGoRichSemanticMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "worker.go")
	writeTestFile(
		t,
		filePath,
		`package worker

type Worker struct{}

// Work handles queued jobs.
func (w *Worker) Work(name string) int {
	if name == "" {
		return 0
	}
	for range name {
	}
	return len(name)
}
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

	functionItem := assertFunctionByName(t, got, "Work")
	assertStringFieldValue(t, functionItem, "docstring", "Work handles queued jobs.")
	assertStringFieldValue(t, functionItem, "class_context", "Worker")
	assertIntFieldValue(t, functionItem, "cyclomatic_complexity", 3)
}

func assertFunctionByName(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, function := range functions {
		functionName, _ := function["name"].(string)
		if functionName == name {
			return function
		}
	}
	t.Fatalf("functions missing name %q in %#v", name, functions)
	return nil
}

func assertBucketItemByName(t *testing.T, payload map[string]any, bucket string, name string) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		itemName, _ := item["name"].(string)
		if itemName == name {
			return item
		}
	}
	t.Fatalf("%s missing name %q in %#v", bucket, name, items)
	return nil
}

func assertStringFieldValue(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, _ := item[field].(string)
	if got != want {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

func assertIntFieldValue(t *testing.T, item map[string]any, field string, want int) {
	t.Helper()

	got, ok := item[field].(int)
	if !ok {
		t.Fatalf("%s = %T, want int", field, item[field])
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", field, got, want)
	}
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
