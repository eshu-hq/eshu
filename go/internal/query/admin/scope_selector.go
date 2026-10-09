// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"errors"
	"fmt"
	"strings"
)

// ErrScopeSelectorNotFound means no ingestion scope matches the
// operator's scope selector. Skip and reopen share this resolver
// outcome (#7732); each route maps it to its own response (skip
// returns an empty result, reopen returns 404).
var ErrScopeSelectorNotFound = errors.New("no ingestion scope matches the scope selector")

// ScopeSelectorAmbiguousError means the operator's scope selector matched
// more than one ingestion scope: one scope's id collided with another
// scope's source key. Skip and reopen share this resolver outcome (#7732);
// both routes fail closed with 409 and name the matched scopes so the
// operator can resubmit with the exact scope_id.
type ScopeSelectorAmbiguousError struct {
	// Selector is the raw operator input that matched ambiguously.
	Selector string
	// ScopeIDs are the matched scope ids in ascending order, bounded to
	// the first two: enough to name the collision.
	ScopeIDs []string
}

// Error reports the collision, naming the matched scopes.
func (e ScopeSelectorAmbiguousError) Error() string {
	return fmt.Sprintf(
		"scope selector %q matches multiple scopes: %s; resubmit with the exact scope_id",
		e.Selector, strings.Join(e.ScopeIDs, ", "),
	)
}
