// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathGoTagsFunctionsWithSingleFileFramework(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "routes.go")
	parsertest.WriteFile(
		t,
		sourcePath,
		`package test

import gin "github.com/gin-gonic/gin"

func wire() {
	router := gin.New()
	router.GET("/health", Health)
}

func Health() {}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	parsertest.AssertFrameworksEqual(t, got, "gin")
	parsertest.AssertStringFieldValue(t, parsertest.AssertBucketItemByName(t, got, "functions", "Health"), "framework", "gin")
	parsertest.AssertStringFieldValue(t, parsertest.AssertBucketItemByName(t, got, "functions", "wire"), "framework", "gin")
}

func TestDefaultEngineParsePathGoSkipsFrameworkTagOnMultipleFrameworks(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "routes.go")
	parsertest.WriteFile(
		t,
		sourcePath,
		`package test

import (
	gin "github.com/gin-gonic/gin"
	echo "github.com/labstack/echo/v4"
)

func wire() {
	ginRouter := gin.New()
	ginRouter.GET("/gin", GinHealth)
	echoRouter := echo.New()
	echoRouter.GET("/echo", EchoHealth)
}

func GinHealth() {}

func EchoHealth() {}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	// Two frameworks claim the file: ambiguous stays silent.
	for _, name := range []string{"wire", "GinHealth", "EchoHealth"} {
		if item := parsertest.AssertBucketItemByName(t, got, "functions", name); item["framework"] != nil {
			t.Fatalf("%s framework = %#v, want nil", name, item["framework"])
		}
	}
}

func TestDefaultEngineParsePathGoLeavesFrameworkUnsetWithoutRoutes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "plain.go")
	parsertest.WriteFile(
		t,
		sourcePath,
		`package test

func Greet(name string) string {
	return "hello " + name
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	if item := parsertest.AssertBucketItemByName(t, got, "functions", "Greet"); item["framework"] != nil {
		t.Fatalf("Greet framework = %#v, want nil", item["framework"])
	}
}
