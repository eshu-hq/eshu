// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package collisions reports accidental corpus nesting from repository
// basenames. It is a leaf: package git imports it, and it must never
// import package git. It depends only on the standard library and
// internal/telemetry, so it stays importable without pulling the
// collector in.
package collisions
