// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathRustEmitsExactFrameworkRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "src", "lib.rs")
	writeTestFile(
		t,
		sourcePath,
		`use actix_web::{get as actix_get, post};
use axum::{routing::{get, post as axum_post}, Router};
use rocket::get as rocket_get;

#[rocket_get("/rocket/<id>")]
fn rocket_show() -> &'static str {
    "rocket"
}

#[actix_get("/actix/{id}")]
async fn actix_show() -> &'static str {
    "actix"
}

#[actix_web::post("/actix")]
async fn actix_create() -> &'static str {
    "actix"
}

fn app() -> Router {
    Router::new()
        .route("/axum/:id", get(axum_show))
        .route("/axum", axum_post(axum_create))
}

async fn axum_show() -> &'static str {
    "axum"
}

async fn axum_create() -> &'static str {
    "axum"
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

	assertNestedRouteEntriesEqual(t, got, "rocket", []map[string]string{
		{"method": "GET", "path": "/rocket/<id>", "handler": "rocket_show"},
	})
	assertNestedRouteEntriesEqual(t, got, "actix_web", []map[string]string{
		{"method": "GET", "path": "/actix/{id}", "handler": "actix_show"},
		{"method": "POST", "path": "/actix", "handler": "actix_create"},
	})
	assertNestedRouteEntriesEqual(t, got, "axum", []map[string]string{
		{"method": "GET", "path": "/axum/:id", "handler": "axum_show"},
		{"method": "POST", "path": "/axum", "handler": "axum_create"},
	})
}

func TestDefaultEngineParsePathRustSkipsNonExactFrameworkRoutes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "src", "lib.rs")
	writeTestFile(
		t,
		sourcePath,
		`use axum::{routing::get, Router};

#[get("/custom")]
async fn unresolved_bare_attribute() -> &'static str {
    "custom"
}

#[actix_web::web::get("/nested")]
async fn nested_non_route_attribute() -> &'static str {
    "nested"
}

#[cfg(feature = "admin")]
#[rocket::get("/admin")]
fn cfg_gated_rocket() -> &'static str {
    "admin"
}

fn dynamic_router(path: &str) -> Router {
    Router::new()
        .route(path, get(dynamic_handler))
        .route("/closure", get(|| async { "closure" }))
}

struct CustomRouter;

impl CustomRouter {
    fn new() -> Self {
        CustomRouter
    }

    fn route(self, _path: &str, _handler: impl Send) -> Self {
        self
    }
}

fn custom_router() -> CustomRouter {
    CustomRouter::new().route("/custom-router", get(dynamic_handler))
}

async fn dynamic_handler() -> &'static str {
    "dynamic"
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

	semantics, ok := got["framework_semantics"].(map[string]any)
	if !ok {
		return
	}
	for _, framework := range []string{"actix_web", "axum", "rocket"} {
		nested, _ := semantics[framework].(map[string]any)
		if nested == nil {
			continue
		}
		if _, ok := nested["route_entries"]; ok {
			t.Fatalf("framework_semantics.%s.route_entries = %#v, want absent for non-exact Rust routes", framework, nested["route_entries"])
		}
	}
}
