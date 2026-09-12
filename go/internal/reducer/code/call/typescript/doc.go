// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package typescript holds the TypeScript/TSX call resolver: interface-typed
// method resolution against the shared entity index's declared-interface
// method table. It reads code/call/shared and never imports code/call, its
// sibling javascript leaf, or any other language leaf.
package typescript
