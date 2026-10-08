// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package javascript holds the JavaScript/JSX call resolver: receiver-typed
// method resolution, dynamic (alias-obscured) call resolution, the
// file-root and top-level-reference caller identity helpers the code/call
// dispatcher uses for module-body and route-configuration calls, and the
// repo-fallback barrier that keeps an unresolved package key to an external
// package off same-named in-repository declarations. It reads the shared
// entity index and candidate-name helpers from code/call/shared and never
// imports code/call or its sibling language leaves.
package javascript
