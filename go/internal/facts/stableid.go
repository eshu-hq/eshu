// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "github.com/eshu-hq/eshu/go/internal/facts/encode"

// StableID returns a deterministic identifier for one fact-adjacent payload.
//
// The implementation lives in [encode.StableID] so the nested fact families
// can derive their own stable ids without importing this package, which
// imports them. This forwarder keeps the facts.StableID call site every
// collector and reducer already uses, and produces byte-identical ids.
func StableID(factType string, identity map[string]any) string {
	return encode.StableID(factType, identity)
}
