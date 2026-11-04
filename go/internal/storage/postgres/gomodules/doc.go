// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gomodules reads the repositories a Go reachability scanner should
// analyze: one rotation page of git repository scopes with an active generation,
// each with its observed commit and remote, keeping those whose stored content
// holds a go.mod. The workflow coordinator is its only caller.
package gomodules
