// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package swift holds the Swift call resolver: receiver-typed method
// resolution via the shared repo-scoped receiver-method index (Swift imports
// name modules, not files, so there is no import-to-file binding). It reads
// the shared entity index from code/call/shared and never imports code/call
// or its sibling language leaves.
package swift
