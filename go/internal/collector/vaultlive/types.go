// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultlive

import "time"

// VaultTarget is one bounded Vault scan scope: a single Vault cluster and
// namespace under a coordinator-assigned claim scope and generation. Object
// versions (mount accessors, policy hashes, KV metadata versions) are carried
// as source evidence by the per-fact observations so staleness is detectable.
type VaultTarget struct {
	VaultClusterID string
	Namespace      string
	ScopeID        string
	GenerationID   string
	FencingToken   int64
	ObservedAt     time.Time
	SourceURI      string
}
