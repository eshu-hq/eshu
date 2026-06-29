// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathScalaEmitsExactPlayRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "conf", "routes")
	writeTestFile(
		t,
		sourcePath,
		`# Play route file
GET     /reports/:id      controllers.ReportsController.show(id: Long)
POST    /reports          controllers.ReportsController.create
PATCH   /reports/:id      dynamic.ReportsController.update(id: Long)
DELETE  /reports/:id      controllers.admin.ReportsController.delete(id: Long)
GET     /assets/*file     controllers.Assets.versioned(path="/public", file: Asset)
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "play")
	assertNestedRouteEntriesEqual(t, got, "play", []map[string]string{
		{"method": "GET", "path": "/reports/:id", "handler": "ReportsController.show"},
		{"method": "POST", "path": "/reports", "handler": "ReportsController.create"},
	})
}

func TestDefaultEngineParsePathScalaEmitsExactHttp4sRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "src", "main", "scala", "example", "Routes.scala")
	writeTestFile(
		t,
		sourcePath,
		`package example

import org.http4s.HttpRoutes
import org.http4s.dsl.io._

object ReportRoutes {
  def health = Ok("ok")
  def createReport = Ok("created")
  def dynamic = Ok("dynamic")

  val routes = HttpRoutes.of[IO] {
    case GET -> Root / "health" => health
    case POST -> Root / "reports" => createReport
    case GET -> Root / "users" / IntVar(id) => showUser
    case GET -> dynamicRoot / "bad" => dynamic
  }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "http4s")
	assertNestedRouteEntriesEqual(t, got, "http4s", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "ReportRoutes.health"},
		{"method": "POST", "path": "/reports", "handler": "ReportRoutes.createReport"},
	})
}
