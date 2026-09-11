// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package search holds the OpenAPI 3.0 path fragments for Eshu's
// read/retrieval surface: content search and file reads (content.go),
// natural-language ask (ask.go), entity resolution and entity/service/
// workload context and story reads (entities.go), graph entity listing
// (graph_entities.go), semantic search (semantic.go), and semantic
// code-hint/documentation-observation evidence (semantic_evidence.go).
//
// Each file exports one JSON string constant that openapi/spec.go
// concatenates, in order, into the published OpenAPI document.
// entities.go additionally imports openapi/schema for a shared inline
// schema fragment. This package MUST NOT import the parent openapi
// package: spec.go imports this package, so the reverse direction is an
// import cycle.
package search
