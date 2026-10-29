// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cli holds the OpenAPI 3.0 path fragments for Eshu's CLI-support
// endpoints: packet rendering (render.go), offline report composition
// (compose.go), bundle capture and validation (capture.go, validate.go),
// and answer-quality scoring (score.go).
//
// Each file exports one JSON string constant that openapi/spec.go
// concatenates, in order, into the published OpenAPI document. This
// package MUST NOT import the parent openapi package: spec.go imports this
// package, so the reverse direction is an import cycle.
package cli
