// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package dead holds the OpenAPI path fragments for the dead-code routes:
// a single-repository investigation (Investigation), a per-repository scan
// (Scan), and a cross-repository classification (CrossRepo). Each is its
// own file, following the one-constant-per-file shape the rest of
// openapi/paths/ uses. openapi.Spec concatenates all three.
//
// This package MUST NOT import openapi or any other openapi/paths/<leaf>
// package.
package dead
