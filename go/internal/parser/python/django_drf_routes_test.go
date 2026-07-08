// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//nolint:filelength // Django/DRF framework-route test suite — the positive,
// negative, and legacy-vs-modern cases share a common set of assertion helpers
// (assertStringSlice, assertRouteEntries) and splitting them into two files
// would either duplicate those helpers or introduce a third test-helper file.

package python

import (
	"reflect"
	"testing"
)

func TestBuildPythonFrameworkSemanticsDjangoExactURLPatterns(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from django.urls import include, path
from django.views import View
from . import views

def health(request):
    return "ok"

def home(request):
    return "home"

class ReportView(View):
    def get(self, request):
        return "report"

    def post(self, request):
        return "created"

urlpatterns = [
    path("", home, name="home"),
    path("health/", health, name="health"),
    path("slashless", health, name="slashless"),
    path("reports/", ReportView.as_view(), name="reports"),
    path("dynamic/<slug:key>/", views.dynamic_detail, name="dynamic"),
    path("imported-class/", views.ReportView.as_view(), name="imported_class"),
    path("nested/", include("nested.urls")),
    path(DYNAMIC_ROUTE, health),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if !reflect.DeepEqual(frameworks, []string{"django"}) {
		t.Fatalf("frameworks = %#v, want [django]", frameworks)
	}
	django, _ := got["django"].(map[string]any)
	if django == nil {
		t.Fatalf("django semantics missing: %#v", got)
	}
	assertStringSlice(t, django, "route_methods", []string{"ANY", "GET", "POST"})
	assertStringSlice(t, django, "route_paths", []string{"/", "/health/", "/slashless", "/reports/", "/dynamic/<slug:key>/", "/imported-class/"})
	assertRouteEntries(t, django, []map[string]string{
		{"method": "ANY", "path": "/", "handler": "home"},
		{"method": "ANY", "path": "/health/", "handler": "health"},
		{"method": "ANY", "path": "/slashless", "handler": "health"},
		{"method": "GET", "path": "/reports/", "handler": "ReportView.get"},
		{"method": "POST", "path": "/reports/", "handler": "ReportView.post"},
		{"method": "ANY", "path": "/dynamic/<slug:key>/"},
		{"method": "ANY", "path": "/imported-class/"},
	})
}

func TestBuildPythonFrameworkSemanticsDRFExactViewSetRoutes(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from django.urls import include, path
from rest_framework.decorators import action
from rest_framework.routers import DefaultRouter
from rest_framework.viewsets import ViewSet

class WidgetViewSet(ViewSet):
    def list(self, request):
        return "list"

    def create(self, request):
        return "create"

    def retrieve(self, request, pk=None):
        return "one"

    @action(detail=True, methods=["post"], url_path="activate")
    def activate(self, request, pk=None):
        return "active"

    @action(detail=False)
    def export(self, request):
        return "export"

class RootViewSet(ViewSet):
    def list(self, request):
        return "root"

router = DefaultRouter()
router.register("widgets", WidgetViewSet, basename="widget")
router.register("", RootViewSet, basename="root")

urlpatterns = [
    path("manual-widgets/", WidgetViewSet.as_view({"get": "list", "post": "create"})),
    path("", include(router.urls)),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if !reflect.DeepEqual(frameworks, []string{"drf"}) {
		t.Fatalf("frameworks = %#v, want [drf]", frameworks)
	}
	drf, _ := got["drf"].(map[string]any)
	if drf == nil {
		t.Fatalf("drf semantics missing: %#v", got)
	}
	assertStringSlice(t, drf, "route_methods", []string{"GET", "POST"})
	assertStringSlice(t, drf, "route_paths", []string{
		"/manual-widgets/",
		"/widgets/",
		"/widgets/{lookup}/",
		"/widgets/{lookup}/activate/",
		"/widgets/export/",
		"/",
	})
	assertRouteEntries(t, drf, []map[string]string{
		{"method": "GET", "path": "/manual-widgets/", "handler": "WidgetViewSet.list"},
		{"method": "POST", "path": "/manual-widgets/", "handler": "WidgetViewSet.create"},
		{"method": "GET", "path": "/widgets/", "handler": "WidgetViewSet.list"},
		{"method": "POST", "path": "/widgets/", "handler": "WidgetViewSet.create"},
		{"method": "GET", "path": "/widgets/{lookup}/", "handler": "WidgetViewSet.retrieve"},
		{"method": "POST", "path": "/widgets/{lookup}/activate/", "handler": "WidgetViewSet.activate"},
		{"method": "GET", "path": "/widgets/export/", "handler": "WidgetViewSet.export"},
		{"method": "GET", "path": "/", "handler": "RootViewSet.list"},
	})
}

func TestBuildPythonFrameworkSemanticsDRFRouterURLConfPrefix(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from django.urls import include, path
from rest_framework.decorators import action
from rest_framework.routers import DefaultRouter
from rest_framework.viewsets import ViewSet

class WidgetViewSet(ViewSet):
    def list(self, request):
        return "list"

    def retrieve(self, request, pk=None):
        return "one"

    @action(detail=True, methods=["post"], url_path="activate")
    def activate(self, request, pk=None):
        return "active"

    @action(detail=False)
    def export(self, request):
        return "export"

class RootViewSet(ViewSet):
    def list(self, request):
        return "root"

router = DefaultRouter()
router.register("widgets", WidgetViewSet, basename="widget")
router.register("", RootViewSet, basename="root")

urlpatterns = [
    path("api/", include(router.urls)),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	drf, _ := got["drf"].(map[string]any)
	if drf == nil {
		t.Fatalf("drf semantics missing: %#v", got)
	}
	assertStringSlice(t, drf, "route_paths", []string{
		"/api/widgets/",
		"/api/widgets/{lookup}/",
		"/api/widgets/{lookup}/activate/",
		"/api/widgets/export/",
		"/api/",
	})
	assertRouteEntries(t, drf, []map[string]string{
		{"method": "GET", "path": "/api/widgets/", "handler": "WidgetViewSet.list"},
		{"method": "GET", "path": "/api/widgets/{lookup}/", "handler": "WidgetViewSet.retrieve"},
		{"method": "POST", "path": "/api/widgets/{lookup}/activate/", "handler": "WidgetViewSet.activate"},
		{"method": "GET", "path": "/api/widgets/export/", "handler": "WidgetViewSet.export"},
		{"method": "GET", "path": "/api/", "handler": "RootViewSet.list"},
	})
}

func TestBuildPythonFrameworkSemanticsDjangoImportedFunctionKeepsRouteOnly(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from django.urls import path
from .views import health

urlpatterns = [
    path("health/", health),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	django, _ := got["django"].(map[string]any)
	if django == nil {
		t.Fatalf("django semantics missing: %#v", got)
	}
	assertRouteEntries(t, django, []map[string]string{
		{"method": "ANY", "path": "/health/"},
	})
}

func TestBuildPythonFrameworkSemanticsDRFRouterRequiresMountEvidence(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from rest_framework.routers import DefaultRouter
from rest_framework.viewsets import ViewSet

class WidgetViewSet(ViewSet):
    def list(self, request):
        return "list"

router = DefaultRouter()
router.register("widgets", WidgetViewSet)
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for unmounted DRF router", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsDjangoDRFDynamicRoutesStayUnclaimed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from django.urls import include, path
from rest_framework.routers import DefaultRouter
from rest_framework.viewsets import ViewSet

class WidgetViewSet(ViewSet):
    def list(self, request):
        return "list"

def health(request):
    return "ok"

route_prefix = "widgets"
method_map = {"get": "list"}
ACTION = "create"
router = DefaultRouter()
router.register(route_prefix, WidgetViewSet)

urlpatterns = [
    path("nested/", include("nested.urls")),
    path(route_prefix + "/health/", health),
    path("manual/", WidgetViewSet.as_view(method_map)),
    path("mixed/", WidgetViewSet.as_view({"get": "list", "post": ACTION})),
    path("invalid-method/", WidgetViewSet.as_view({"foo": "list"})),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for dynamic/include-only route evidence", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsDRFDynamicRouterMountStaysUnclaimed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from django.urls import include, path
from rest_framework.routers import DefaultRouter
from rest_framework.viewsets import ViewSet

API_PREFIX = "api/"

class WidgetViewSet(ViewSet):
    def list(self, request):
        return "list"

router = DefaultRouter()
router.register("widgets", WidgetViewSet)

urlpatterns = [
    path(API_PREFIX, include(router.urls)),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for dynamically mounted DRF router", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsDRFDynamicActionMetadataStayUnclaimed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from rest_framework.decorators import action
from rest_framework.routers import DefaultRouter
from rest_framework.viewsets import ViewSet

METHODS = ["post"]
DETAIL = True
ACTION_PATH = "activate"

class WidgetViewSet(ViewSet):
    @action(detail=DETAIL, methods=METHODS, url_path=ACTION_PATH)
    def activate(self, request, pk=None):
        return "active"

router = DefaultRouter()
router.register("widgets", WidgetViewSet)
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for dynamic DRF action evidence", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsDjangoRequiresURLConfEvidence(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `def health(request):
    return "ok"

def path(route, handler):
    return (route, handler)

routes = [
    path("shadowed/", health),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for non-Django shadowed path helper", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsDjangoRequiresURLPatternsContext(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from django.urls import path

def health(request):
    return "ok"

custom_routes = [
    path("health/", health),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty outside urlpatterns", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsDjangoLegacyURLPatterns(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from django.conf.urls import include, url
from django.views import View

class UserView(View):
    def get(self, request):
        return "user"
    def post(self, request):
        return "created"

class ArticleView(View):
    def get(self, request):
        return "article"
    def put(self, request):
        return "updated"

def health(request):
    return "ok"

urlpatterns = [
    url(r'^user/?$', UserView.as_view()),
    url(r'^users/login/?$', health),
    url(r'^articles/?$', ArticleView.as_view()),
    url(r'^nested/', include("nested.urls")),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if !reflect.DeepEqual(frameworks, []string{"django"}) {
		t.Fatalf("frameworks = %#v, want [django]", frameworks)
	}
	django, _ := got["django"].(map[string]any)
	if django == nil {
		t.Fatalf("django semantics missing: %#v", got)
	}
	assertStringSlice(t, django, "route_methods", []string{"GET", "POST", "ANY", "PUT"})
	assertStringSlice(t, django, "route_paths", []string{"/user/", "/users/login/", "/articles/"})
	assertRouteEntries(t, django, []map[string]string{
		{"method": "GET", "path": "/user/", "handler": "UserView.get"},
		{"method": "POST", "path": "/user/", "handler": "UserView.post"},
		{"method": "ANY", "path": "/users/login/", "handler": "health"},
		{"method": "GET", "path": "/articles/", "handler": "ArticleView.get"},
		{"method": "PUT", "path": "/articles/", "handler": "ArticleView.put"},
	})
}

func TestBuildPythonFrameworkSemanticsDjangoRePath(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from django.urls import re_path
from django.views import View

class UserView(View):
    def get(self, request):
        return "user"

urlpatterns = [
    re_path(r'^user/?$', UserView.as_view()),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if !reflect.DeepEqual(frameworks, []string{"django"}) {
		t.Fatalf("frameworks = %#v, want [django]", frameworks)
	}
	django, _ := got["django"].(map[string]any)
	if django == nil {
		t.Fatalf("django semantics missing: %#v", got)
	}
	assertStringSlice(t, django, "route_methods", []string{"GET"})
	assertStringSlice(t, django, "route_paths", []string{"/user/"})
	assertRouteEntries(t, django, []map[string]string{
		{"method": "GET", "path": "/user/", "handler": "UserView.get"},
	})
}

func TestBuildPythonFrameworkSemanticsDjangoURLAndPathMixed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from django.conf.urls import url
from django.urls import path
from django.views import View

class UserView(View):
    def get(self, request):
        return "user"

class ReportView(View):
    def post(self, request):
        return "created"

urlpatterns = [
    url(r'^user/?$', UserView.as_view()),
    path("reports/", ReportView.as_view()),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if !reflect.DeepEqual(frameworks, []string{"django"}) {
		t.Fatalf("frameworks = %#v, want [django]", frameworks)
	}
	django, _ := got["django"].(map[string]any)
	if django == nil {
		t.Fatalf("django semantics missing: %#v", got)
	}
	assertStringSlice(t, django, "route_paths", []string{"/user/", "/reports/"})
}

func TestBuildPythonFrameworkSemanticsDjangoURLConfImportOnly(t *testing.T) {
	// Negative: url() outside urlpatterns should not produce routes.
	root, source, closer := parsePythonForTest(t, `from django.conf.urls import url
from django.views import View

class UserView(View):
    def get(self, request):
        return "user"

not_urlpatterns = [
    url(r'^user/?$', UserView.as_view()),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for url() outside urlpatterns", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsNonDjangoModule(t *testing.T) {
	// Negative: non-Django module with url() calls should not trigger Django detection.
	root, source, closer := parsePythonForTest(t, `from django.views import View

class UserView(View):
    def get(self, request):
        return "user"

def url(path, view):
    return (path, view)

urlpatterns = [
    url("/user/", UserView.as_view()),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for non-Django module with shadowed url helper", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsDjangoRequiresURLPatternsContextURL(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from django.conf.urls import url
from django.views import View

class UserView(View):
    def get(self, request):
        return "user"

custom_routes = [
    url(r'^user/?$', UserView.as_view()),
]
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty outside urlpatterns", frameworks)
	}
}
