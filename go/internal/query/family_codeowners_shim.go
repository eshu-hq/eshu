// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/codeowners"
)

// This file preserves the root package query surface cmd/api,
// cmd/mcp-server, staying stores, and staying tests still use for the
// codeowners-ownership family. The implementation moved to
// internal/query/codeowners (#6060 lane A L2); this alias forwards
// unchanged so those call sites compile without touching other lanes.
//
// The file is deliberately NOT named codeowners_shim.go: a
// codeowners_-prefixed staying file would match a later phase's family glob
// and be swept into a move it must survive (family_code_shim.go
// precedent).

// CodeownersOwnershipHandler exposes a bounded, graph-backed read of one
// repository's CODEOWNERS ownership plus the manifest-vs-codeowners
// effective_owner. See codeowners.Handler.
type CodeownersOwnershipHandler = codeowners.Handler
