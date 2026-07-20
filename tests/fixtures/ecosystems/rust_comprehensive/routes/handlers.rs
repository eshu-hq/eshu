// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// #5361 route query-proof matrix: cribs the Axum router shape proven by
// internal/parser/rust_route_entries_test.go
// (TestDefaultEngineParsePathRustEmitsExactFrameworkRouteEntries).
use axum::{routing::get, Router};

fn app() -> Router {
    Router::new().route("/axum/:id", get(axum_show))
}

async fn axum_show() -> &'static str {
    "axum"
}
