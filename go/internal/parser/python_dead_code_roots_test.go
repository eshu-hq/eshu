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

func TestDefaultEngineParsePathPythonEmitsDeadCodeCLIRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "cli.py")
	writeTestFile(
		t,
		filePath,
		`import click
import typer

cli = click.Group()
app = typer.Typer()

@click.command()
def click_standalone():
    return "ok"

@cli.command("sync")
def click_group_command():
    return "ok"

@app.command()
def typer_command():
    return "ok"

@app.callback()
def typer_callback():
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
		assertFunctionByName(t, got, "click_standalone"),
		"dead_code_root_kinds",
		[]string{"python.click_command_decorator"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "click_group_command"),
		"dead_code_root_kinds",
		[]string{"python.click_command_decorator"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "typer_command"),
		"dead_code_root_kinds",
		[]string{"python.typer_command_decorator"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "typer_callback"),
		"dead_code_root_kinds",
		[]string{"python.typer_callback_decorator"},
	)
}

func TestDefaultEngineParsePathPythonEmitsScriptMainGuardRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "script.py")
	writeTestFile(
		t,
		filePath,
		`def main():
    return 0

def helper():
    return 1

if __name__ == "__main__":
    main()
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
		assertFunctionByName(t, got, "main"),
		"dead_code_root_kinds",
		[]string{"python.script_main_guard"},
	)
	if helper := assertFunctionByName(t, got, "helper"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathPythonEmitsReversedScriptMainGuardRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "script.py")
	writeTestFile(
		t,
		filePath,
		`def main():
    return 0

def helper():
    return 1

if ("__main__" == __name__):
    main()
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
		assertFunctionByName(t, got, "main"),
		"dead_code_root_kinds",
		[]string{"python.script_main_guard"},
	)
	if helper := assertFunctionByName(t, got, "helper"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathPythonScriptMainGuardSkipsElseAndNestedDefinitions(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "script.py")
	writeTestFile(
		t,
		filePath,
		`def main():
    return 0

def else_helper():
    return 1

def nested_helper():
    return 2

def nested_definition_call():
    return 3

if __name__ == "__main__":
    main()
    def local():
        nested_definition_call()
else:
    else_helper()
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
		assertFunctionByName(t, got, "main"),
		"dead_code_root_kinds",
		[]string{"python.script_main_guard"},
	)
	if helper := assertFunctionByName(t, got, "else_helper"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("else_helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
	if nested := assertFunctionByName(t, got, "nested_definition_call"); nested["dead_code_root_kinds"] != nil {
		t.Fatalf("nested_definition_call dead_code_root_kinds = %#v, want nil", nested["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathPythonDunderAssignmentEvidenceIsEnclosingScopeScoped(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "protocol_patch.py")
	writeTestFile(
		t,
		filePath,
		`def __reduce__():
    return "module"

def patch_missing(missing):
    def __reduce__():
        return "missing"

    type(missing).__reduce__ = __reduce__
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

	reduceFunctions := pythonFunctionsByName(t, got, "__reduce__")
	if len(reduceFunctions) != 2 {
		t.Fatalf("__reduce__ function count = %d, want 2 in %#v", len(reduceFunctions), reduceFunctions)
	}
	for _, function := range reduceFunctions {
		lineNumber, _ := function["line_number"].(int)
		switch lineNumber {
		case 1:
			if function["dead_code_root_kinds"] != nil {
				t.Fatalf("module __reduce__ dead_code_root_kinds = %#v, want nil", function["dead_code_root_kinds"])
			}
		case 5:
			assertParserStringSliceFieldValue(t, function, "dead_code_root_kinds", []string{"python.dunder_method"})
		default:
			t.Fatalf("unexpected __reduce__ line_number = %d in %#v", lineNumber, function)
		}
	}
}

func TestDefaultEngineParsePathPythonEmitsSAMHandlerDeadCodeRootKind(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(
		t,
		filepath.Join(repoRoot, "template.yaml"),
		`AWSTemplateFormatVersion: '2010-09-09'
Transform: 'AWS::Serverless-2016-10-31'
Resources:
  Worker:
    Type: AWS::Serverless::Function
    Properties:
      Handler: app.lambda_handler
      Runtime: python3.11
      CodeUri: ./
`,
	)
	filePath := filepath.Join(repoRoot, "app.py")
	writeTestFile(
		t,
		filePath,
		`def lambda_handler(event, context):
    return "ok"

def helper():
    return "candidate"
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
		assertFunctionByName(t, got, "lambda_handler"),
		"dead_code_root_kinds",
		[]string{"python.aws_lambda_handler"},
	)
	if _, ok := assertFunctionByName(t, got, "helper")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent")
	}
}

func pythonFunctionsByName(t *testing.T, payload map[string]any, name string) []map[string]any {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	var matches []map[string]any
	for _, function := range functions {
		functionName, _ := function["name"].(string)
		if functionName == name {
			matches = append(matches, function)
		}
	}
	return matches
}

func TestDefaultEngineParsePathPythonEmitsPublicAPIRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	packageDir := filepath.Join(repoRoot, "library")
	writeTestFile(
		t,
		filepath.Join(packageDir, "__init__.py"),
		`from .models import PublicService as Service
`,
	)
	filePath := filepath.Join(packageDir, "models.py")
	writeTestFile(
		t,
		filePath,
		`__all__ = ["module_factory"]

class BaseService:
    def inherited(self):
        return "ok"

class PublicService(BaseService):
    def run(self):
        return "ok"

def module_factory():
    return PublicService()

def private_helper():
    return "candidate"
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
		assertBucketItemByName(t, got, "classes", "PublicService"),
		"dead_code_root_kinds",
		[]string{"python.package_init_export"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertBucketItemByName(t, got, "classes", "BaseService"),
		"dead_code_root_kinds",
		[]string{"python.public_api_base"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "run"),
		"dead_code_root_kinds",
		[]string{"python.public_api_member"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "inherited"),
		"dead_code_root_kinds",
		[]string{"python.public_api_member"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "module_factory"),
		"dead_code_root_kinds",
		[]string{"python.module_all_export"},
	)
	if _, ok := assertFunctionByName(t, got, "private_helper")["dead_code_root_kinds"]; ok {
		t.Fatalf("private_helper dead_code_root_kinds present, want absent")
	}
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
