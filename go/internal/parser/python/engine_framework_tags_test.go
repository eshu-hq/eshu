// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPythonTagsFunctionsWithSingleFileFramework(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "flask_app.py")
	writeTestFile(
		t,
		filePath,
		`from flask import Flask

app = Flask(__name__)

@app.route("/health")
def health():
    return "ok"

def helper():
    return "helper"
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
	parsertest.AssertStringFieldValue(t, parsertest.AssertBucketItemByName(t, got, "functions", "health"), "framework", "flask")
	parsertest.AssertStringFieldValue(t, parsertest.AssertBucketItemByName(t, got, "functions", "helper"), "framework", "flask")
}

func TestDefaultEngineParsePathPythonSkipsFrameworkTagOnMultipleFrameworks(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "urls.py")
	writeTestFile(
		t,
		filePath,
		`from django.urls import path
from rest_framework.viewsets import ViewSet

def health(request):
    return "ok"

class WidgetViewSet(ViewSet):
    def list(self, request):
        return "list"

urlpatterns = [
    path("health/", health),
    path("widgets/", WidgetViewSet.as_view({"get": "list"})),
]
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

	parsertest.AssertFrameworksEqual(t, got, "django", "drf")
	// Two frameworks claim the file: ambiguous stays silent.
	if item := parsertest.AssertBucketItemByName(t, got, "functions", "health"); item["framework"] != nil {
		t.Fatalf("health framework = %#v, want nil", item["framework"])
	}
}

func TestDefaultEngineParsePathPythonLeavesFrameworkUnsetWithoutRoutes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "plain.py")
	writeTestFile(
		t,
		filePath,
		`def greet(name):
    return "hello " + name
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

	if item := parsertest.AssertBucketItemByName(t, got, "functions", "greet"); item["framework"] != nil {
		t.Fatalf("greet framework = %#v, want nil", item["framework"])
	}
}
