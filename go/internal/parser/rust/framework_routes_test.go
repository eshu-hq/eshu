// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// These tests characterize framework-route detection (issue #4840) so the
// single-walk consolidation of the axum call scan into the main payload walk
// cannot silently change which routes are captured or their order.

func TestParseCapturesAxumRouterRoutesInChainOrder(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `fn build_router() -> axum::Router {
    axum::Router::new()
        .route("/users", axum::routing::get(list_users))
        .route("/users", axum::routing::post(create_user))
}

async fn list_users() {}
async fn create_user() {}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("payload[framework_semantics] = %T, want map[string]any", payload["framework_semantics"])
	}
	axum, ok := semantics["axum"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics[axum] = %T, want map[string]any", semantics["axum"])
	}
	entries, ok := axum["route_entries"].([]map[string]string)
	if !ok {
		t.Fatalf("framework_semantics[axum][route_entries] = %T, want []map[string]string", axum["route_entries"])
	}
	if len(entries) != 2 {
		t.Fatalf("route_entries = %#v, want 2 entries", entries)
	}
	if entries[0]["method"] != "GET" || entries[0]["path"] != "/users" || entries[0]["handler"] != "list_users" {
		t.Fatalf("route_entries[0] = %#v, want GET /users list_users", entries[0])
	}
	if entries[1]["method"] != "POST" || entries[1]["path"] != "/users" || entries[1]["handler"] != "create_user" {
		t.Fatalf("route_entries[1] = %#v, want POST /users create_user", entries[1])
	}
}

func TestParseCapturesActixWebAndRocketAttributeRoutes(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `#[actix_web::get("/health")]
async fn health() {}

#[rocket::post("/submit")]
fn submit() {}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("payload[framework_semantics] = %T, want map[string]any", payload["framework_semantics"])
	}

	actix, ok := semantics["actix_web"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics[actix_web] = %T, want map[string]any", semantics["actix_web"])
	}
	actixEntries, ok := actix["route_entries"].([]map[string]string)
	if !ok || len(actixEntries) != 1 {
		t.Fatalf("actix_web route_entries = %#v, want 1 entry", actix["route_entries"])
	}
	if actixEntries[0]["method"] != "GET" || actixEntries[0]["path"] != "/health" || actixEntries[0]["handler"] != "health" {
		t.Fatalf("actix_web route_entries[0] = %#v, want GET /health health", actixEntries[0])
	}

	rocket, ok := semantics["rocket"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics[rocket] = %T, want map[string]any", semantics["rocket"])
	}
	rocketEntries, ok := rocket["route_entries"].([]map[string]string)
	if !ok || len(rocketEntries) != 1 {
		t.Fatalf("rocket route_entries = %#v, want 1 entry", rocket["route_entries"])
	}
	if rocketEntries[0]["method"] != "POST" || rocketEntries[0]["path"] != "/submit" || rocketEntries[0]["handler"] != "submit" {
		t.Fatalf("rocket route_entries[0] = %#v, want POST /submit submit", rocketEntries[0])
	}
}

func TestParseSkipsCfgGatedAxumRoutes(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `#[cfg(feature = "extra")]
fn build_router() -> axum::Router {
    axum::Router::new().route("/hidden", axum::routing::get(hidden))
}

async fn hidden() {}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("payload[framework_semantics] = %T, want map[string]any", payload["framework_semantics"])
	}
	if _, present := semantics["axum"]; present {
		t.Fatalf("framework_semantics[axum] = %#v, want absent for cfg-gated route", semantics["axum"])
	}
}
