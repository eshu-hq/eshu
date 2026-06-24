// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopedtoken

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// FileEnvVar points at an operator-managed, secret-mounted scoped-token
// registry file. When unset, scoped-token resolution is disabled and the
// API/MCP surface keeps shared-token (or local dev-mode) behavior.
const FileEnvVar = "ESHU_SCOPED_TOKENS_FILE"

// ResolverFromEnv loads the scoped-token registry referenced by
// ESHU_SCOPED_TOKENS_FILE, if set, and returns it as a query.ScopedTokenResolver.
//
// It returns a nil resolver (not a typed nil) when the variable is unset so the
// caller's dev-mode and shared-token paths stay intact. A configured-but-
// unreadable or malformed registry is a hard startup error: a hosted deployment
// must fail closed rather than silently run without tenant isolation.
func ResolverFromEnv(getenv func(string) string) (query.ScopedTokenResolver, error) {
	path := strings.TrimSpace(getenv(FileEnvVar))
	if path == "" {
		return nil, nil
	}
	registry, err := LoadRegistryFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load scoped token registry: %w", err)
	}
	return registry, nil
}
