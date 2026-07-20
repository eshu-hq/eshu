// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package routes

import "net/http"

// RegisterRoutes wires the catalog handlers onto the default net/http mux
// (#5361 route query-proof matrix: cribs the net/http shape proven by
// internal/parser/golang/framework_semantics_gate_test.go).
func RegisterRoutes() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /catalog/items", ListItems)
	mux.HandleFunc("POST /catalog/items", CreateItem)
}

// ListItems returns the catalog item collection.
func ListItems(w http.ResponseWriter, r *http.Request) {}

// CreateItem creates a catalog item.
func CreateItem(w http.ResponseWriter, r *http.Request) {}
