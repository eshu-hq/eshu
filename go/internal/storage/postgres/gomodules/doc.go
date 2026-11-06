// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gomodules reads the repositories a Go reachability scanner should
// analyze: one rotation page of git repository scopes with an active generation,
// each with its observed commit and remote, keeping those whose stored content
// holds a go.mod and those whose go-reachability scope still holds Go evidence to
// retract. It also retracts a go-reachability generation the commit-skew rule
// names and reopens the scopes holding findings for that repository, in one
// transaction, which is the package's only write. The workflow coordinator is its
// only caller.
package gomodules
