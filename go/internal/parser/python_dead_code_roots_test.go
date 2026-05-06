package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPythonEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "roots.py")
	writeTestFile(
		t,
		filePath,
		`from celery import shared_task
from fastapi import APIRouter, FastAPI
from flask import Flask

app = FastAPI()
router = APIRouter(prefix="/payments")
flask_app = Flask(__name__)

@app.get("/health")
async def fastapi_health():
    return {"ok": True}

@router.post("/charge")
def fastapi_charge():
    return {"ok": True}

@flask_app.route("/status", methods=["GET"])
def flask_status():
    return "ok"

@shared_task
def celery_shared():
    return "ok"

@app.task(bind=True)
def celery_bound(self):
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

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "fastapi_health"),
		"dead_code_root_kinds",
		[]string{"python.fastapi_route_decorator"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "fastapi_charge"),
		"dead_code_root_kinds",
		[]string{"python.fastapi_route_decorator"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "flask_status"),
		"dead_code_root_kinds",
		[]string{"python.flask_route_decorator"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "celery_shared"),
		"dead_code_root_kinds",
		[]string{"python.celery_task_decorator"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "celery_bound"),
		"dead_code_root_kinds",
		[]string{"python.celery_task_decorator"},
	)
}

func TestDefaultEngineParsePathPythonDoesNotMarkUnknownDecoratorsAsDeadCodeRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "decorators.py")
	writeTestFile(
		t,
		filePath,
		`def tracked(fn):
    return fn

@tracked
def helper():
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

	functionItem := assertFunctionByName(t, got, "helper")
	if _, ok := functionItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for unknown decorator", functionItem["dead_code_root_kinds"])
	}
}
