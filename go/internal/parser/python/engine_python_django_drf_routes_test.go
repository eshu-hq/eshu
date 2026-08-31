// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPythonDjangoDRFExactRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "urls.py")
	writeTestFile(
		t,
		filePath,
		`from django.urls import path
from django.views import View
from rest_framework.viewsets import ViewSet

def health(request):
    return "ok"

class ReportView(View):
    def get(self, request):
        return "report"

class WidgetViewSet(ViewSet):
    def list(self, request):
        return "list"

urlpatterns = [
    path("health/", health),
    path("reports/", ReportView.as_view()),
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
	parsertest.AssertNestedRouteEntriesEqual(t, got, "django", []map[string]string{
		{"method": "ANY", "path": "/health/", "handler": "health"},
		{"method": "GET", "path": "/reports/", "handler": "ReportView.get"},
	})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "drf", []map[string]string{
		{"method": "GET", "path": "/widgets/", "handler": "WidgetViewSet.list"},
	})
}
