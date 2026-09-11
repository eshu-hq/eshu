// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package haskell holds the Haskell call resolver: qualified-import-bound
// callee resolution for `Module.function` calls. It reads the shared entity
// index from code/call/shared and never imports code/call or its sibling
// language leaves.
package haskell
