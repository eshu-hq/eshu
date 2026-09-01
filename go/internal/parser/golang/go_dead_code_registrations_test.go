// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathGoEmitsDeadCodeRegistrationRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "registrations.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

import (
	rootcobra "github.com/spf13/cobra"
	handler "net/http"
)

func ServeDirect(w handler.ResponseWriter, r *handler.Request) {}
func ServeMuxed(w handler.ResponseWriter, r *handler.Request) {}
func ServeStatus(w handler.ResponseWriter, r *handler.Request) {}
func runDirect(cmd *rootcobra.Command, args []string) {}
func runAssigned(cmd *rootcobra.Command, args []string) {}

func wire() {
	handler.HandleFunc("/payments", ServeDirect)
	handler.HandleFunc("GET /status", ServeStatus)
	mux := handler.NewServeMux()
	mux.Handle("/health", handler.HandlerFunc(ServeMuxed))
	rootCmd := &rootcobra.Command{Run: runDirect}
	rootCmd.RunE = runAssigned
	_ = rootCmd
}
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

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "ServeDirect"), "dead_code_root_kinds", "go.net_http_handler_registration")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "ServeMuxed"), "dead_code_root_kinds", "go.net_http_handler_registration")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "ServeStatus"), "dead_code_root_kinds", "go.net_http_handler_registration")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "runDirect"), "dead_code_root_kinds", "go.cobra_run_registration")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "runAssigned"), "dead_code_root_kinds", "go.cobra_run_registration")
	parsertest.AssertFrameworksEqual(t, got, "net_http")
	parsertest.AssertNestedStringSliceEqual(t, got, "net_http", "route_methods", []string{"ANY", "GET"})
	parsertest.AssertNestedStringSliceEqual(t, got, "net_http", "route_paths", []string{"/payments", "/status", "/health"})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "net_http", []map[string]string{
		{"method": "ANY", "path": "/payments", "handler": "ServeDirect"},
		{"method": "GET", "path": "/status", "handler": "ServeStatus"},
		{"method": "ANY", "path": "/health", "handler": "ServeMuxed"},
	})
}

func TestDefaultEngineParsePathGoIgnoresUnknownHandleFuncReceivers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "unknown_mux.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

type fakeMux struct{}

func (m *fakeMux) HandleFunc(_ string, _ func()) {}

func maybeHTTP() {}

func wire() {
	mux := &fakeMux{}
	mux.HandleFunc("/payments", maybeHTTP)
}
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

	functionItem := parsertest.AssertBucketItemByName(t, got, "functions", "maybeHTTP")
	if semantics, ok := got["framework_semantics"]; ok {
		t.Fatalf("framework_semantics = %#v, want absent for unknown mux receiver", semantics)
	}
	parsertest.AssertStringSliceContains(t, functionItem, "dead_code_root_kinds", "go.function_value_reference")
	parsertest.AssertStringSliceNotContains(
		t,
		functionItem,
		"dead_code_root_kinds",
		"go.net_http_handler_registration",
	)
}

func TestDefaultEngineParsePathGoEmitsMixedCaseServeMuxRouteEntry(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "mixed_case_mux.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

import handler "net/http"

func ServeReady(w handler.ResponseWriter, r *handler.Request) {}

func wire() {
	apiMux := handler.NewServeMux()
	apiMux.HandleFunc("GET /ready", ServeReady)
}
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

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "ServeReady"), "dead_code_root_kinds", "go.net_http_handler_registration")
	parsertest.AssertNestedRouteEntriesEqual(t, got, "net_http", []map[string]string{
		{"method": "GET", "path": "/ready", "handler": "ServeReady"},
	})
}

func TestDefaultEngineParsePathGoEmitsThirdPartyRouteEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		imports   string
		wire      string
		framework string
		want      []map[string]string
	}{
		{
			name:    "gin",
			imports: `gin "github.com/gin-gonic/gin"`,
			wire: `
func wire() {
	router := gin.New()
	router.GET("/health", Health)
	api := router.Group("/api")
	api.POST("/widgets", CreateWidget)
}
`,
			framework: "gin",
			want: []map[string]string{
				{"method": "GET", "path": "/health", "handler": "Health"},
				{"method": "POST", "path": "/api/widgets", "handler": "CreateWidget"},
			},
		},
		{
			name:    "gin camelcase receiver",
			imports: `gin "github.com/gin-gonic/gin"`,
			wire: `
func wire() {
	apiRouter := gin.Default()
	apiRouter.GET("/health", Health)
}
`,
			framework: "gin",
			want: []map[string]string{
				{"method": "GET", "path": "/health", "handler": "Health"},
			},
		},
		{
			name:    "gin package receiver",
			imports: `gin "github.com/gin-gonic/gin"`,
			wire: `
var apiRouter = gin.New()

func wire() {
	apiRouter.GET("/health", Health)
}
`,
			framework: "gin",
			want: []map[string]string{
				{"method": "GET", "path": "/health", "handler": "Health"},
			},
		},
		{
			name:    "echo",
			imports: `echo "github.com/labstack/echo/v4"`,
			wire: `
func wire() {
	router := echo.New()
	router.GET("/health", Health)
	api := router.Group("/api")
	api.POST("/widgets", CreateWidget)
}
`,
			framework: "echo",
			want: []map[string]string{
				{"method": "GET", "path": "/health", "handler": "Health"},
				{"method": "POST", "path": "/api/widgets", "handler": "CreateWidget"},
			},
		},
		{
			name:    "chi",
			imports: `chi "github.com/go-chi/chi/v5"`,
			wire: `
func wire() {
	router := chi.NewRouter()
	router.Get("/health", Health)
	router.MethodFunc("PATCH", "/widgets/{id}", UpdateWidget)
}
`,
			framework: "chi",
			want: []map[string]string{
				{"method": "GET", "path": "/health", "handler": "Health"},
				{"method": "PATCH", "path": "/widgets/{id}", "handler": "UpdateWidget"},
			},
		},
		{
			name:    "fiber",
			imports: `fiber "github.com/gofiber/fiber/v2"`,
			wire: `
func wire() {
	app := fiber.New()
	app.Get("/health", Health)
	api := app.Group("/api")
	api.Post("/widgets", CreateWidget)
}
`,
			framework: "fiber",
			want: []map[string]string{
				{"method": "GET", "path": "/health", "handler": "Health"},
				{"method": "POST", "path": "/api/widgets", "handler": "CreateWidget"},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			filePath := filepath.Join(repoRoot, "routes.go")
			parsertest.WriteFile(
				t,
				filePath,
				`package roots

import (
	`+tc.imports+`
)

func Health() {}
func CreateWidget() {}
func UpdateWidget() {}
`+tc.wire,
			)

			engine, err := parser.DefaultEngine()
			if err != nil {
				t.Fatalf("DefaultEngine() error = %v, want nil", err)
			}

			got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
			if err != nil {
				t.Fatalf("ParsePath() error = %v, want nil", err)
			}

			parsertest.AssertFrameworksEqual(t, got, tc.framework)
			parsertest.AssertNestedRouteEntriesEqual(t, got, tc.framework, tc.want)
		})
	}
}

func TestDefaultEngineParsePathGoSkipsAmbiguousThirdPartyRoutes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "ambiguous_routes.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

import gin "github.com/gin-gonic/gin"

func Health() {}
func Wrapped() {}

func wire(dynamicPath string) {
	router := gin.New()
	router.GET(dynamicPath, Health)
	router.GET("/closure", func() {})
	router.GET("/method-value", controller.Health)
	router.GET("/middleware", middleware, Health)
	router.GET("/wrapped", gin.WrapF(Wrapped))
	unknown.GET("/unknown", Health)
}
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

	if semantics, ok := got["framework_semantics"]; ok {
		t.Fatalf("framework_semantics = %#v, want absent for ambiguous third-party routes", semantics)
	}
}

func TestDefaultEngineParsePathGoScopesThirdPartyRouteReceivers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "scoped_routes.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

import gin "github.com/gin-gonic/gin"

type customRouter struct{}

func Health() {}
func Shadow() {}

func wire() {
	router := gin.New()
	router.GET("/health", Health)
}

func other(router customRouter) {
	router.GET("/shadowed", Shadow)
}
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

	parsertest.AssertFrameworksEqual(t, got, "gin")
	parsertest.AssertNestedRouteEntriesEqual(t, got, "gin", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "Health"},
	})
}
