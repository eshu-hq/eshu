// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rust holds the Rust call resolver: trait-bound receiver-typed
// callee resolution against the shared entity index's trait-method table. It
// reads the shared entity index from code/call/shared and never imports
// code/call or its sibling language leaves.
package rust
