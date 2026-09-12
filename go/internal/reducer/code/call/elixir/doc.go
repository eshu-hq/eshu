// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package elixir holds the Elixir call resolver: alias-import-bound callee
// resolution for qualified `Module.function` calls, and the repo-fallback-
// blocking check for an explicit alias binding. It reads the shared entity
// index and path helpers from code/call/shared and never imports code/call
// or its sibling language leaves.
package elixir
