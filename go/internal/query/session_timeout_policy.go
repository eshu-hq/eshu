// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// resolveSessionTimeouts forwards to queryauth.ResolveSessionTimeouts. The
// implementation moved there for #6642 so a handler-family subpackage can
// resolve session timeouts without importing this package; every existing
// caller keeps its exact behavior through this wrapper.
func resolveSessionTimeouts(
	ctx context.Context,
	signInPolicy SignInPolicyReadStore,
	tenantID string,
	defaultIdle time.Duration,
	defaultAbsolute time.Duration,
) (idle time.Duration, absolute time.Duration) {
	return queryauth.ResolveSessionTimeouts(ctx, signInPolicy, tenantID, defaultIdle, defaultAbsolute)
}
