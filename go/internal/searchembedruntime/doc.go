// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package searchembedruntime selects the semantic-search embedder used by
// Eshu runtimes.
//
// The package keeps API, MCP, and reducer wiring on one profile-selection
// contract. It preserves explicit local hash overrides, supports Compose
// auto-local fallback, enables a single governed search_documents provider
// profile by default after policy/egress admission, exposes per-document policy
// admission for reducer dispatch, and fails closed when multiple provider
// profiles need an explicit selector.
package searchembedruntime
