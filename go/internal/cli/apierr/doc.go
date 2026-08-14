// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package apierr carries the one contract the Eshu CLI's transport errors
// have to cross a package boundary: the HTTP status an API call failed with.
//
// The CLI's concrete API error type is unexported and lives in go/cmd/eshu,
// which is package main. Nothing can import it, so every command family whose
// logic moves into an internal/cli package loses the ability to classify a
// failure by status the moment it moves. This package is the seam: go/cmd/eshu
// keeps the concrete type and gives it an HTTPStatusCode method, and the
// internal/cli packages depend on HTTPStatusError and StatusCode here.
//
// The package holds an interface and one function that reads it. It has no
// dependency outside the standard library, deliberately -- anything that grows
// here becomes a dependency of every CLI package that classifies an error.
// Error-code vocabularies, retry policy, and remediation text belong with the
// command family that owns them, not here.
package apierr
