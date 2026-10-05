// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// EventAuthIdentityStoreUnavailable names the structured log record emitted when
// a credential could not be evaluated because the identity store (PostgreSQL)
// was unreachable or timed out (#7586). It carries the bounded failure_class
// (unavailable, timeout, or topology) and never the credential or the driver's
// error text. When the failure came through the bounded pool, the paired
// postgres.store.error record carries the driver detail; a bare deadline or a
// database/sql connection sentinel has no paired record. The request is answered
// with a 503, not a 401.
const EventAuthIdentityStoreUnavailable = "auth.identity_store.unavailable"
