// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopestore

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// SourceKey returns the value persisted in ingestion_scopes.source_key for
// scopeValue: the trimmed Metadata["source_key"] when it is set, otherwise the
// scope ID. The ingestion commit writes it, and the deferred-maintenance lock
// falls back to it for a scope with no partition key, so both call this one
// derivation.
func SourceKey(scopeValue scope.IngestionScope) string {
	if scopeValue.Metadata != nil {
		if sourceKey := strings.TrimSpace(scopeValue.Metadata["source_key"]); sourceKey != "" {
			return sourceKey
		}
	}

	return scopeValue.ScopeID
}
