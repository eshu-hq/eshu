// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package dart holds the Dart call resolver: import-bound callee resolution
// (package: and relative imports) and the repo-fallback-blocking check for
// an explicit but unresolved import. It reads the shared entity index and
// path/import helpers from code/call/shared and never imports code/call or
// its sibling language leaves.
package dart
