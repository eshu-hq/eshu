// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerAuthIdentityStoreUnavailable registers the counter for credentials
// the API could not evaluate because the identity store (PostgreSQL) was
// unreachable or timed out (#7586). It is labeled by failure_class only
// (unavailable, timeout, topology): the first two are the bounded Postgres error
// kinds, and topology is a writer refused for a role, system, or history
// mismatch, which is permanent until restart. It never carries a credential, a
// subject, or the driver's error text.
func registerAuthIdentityStoreUnavailable(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.AuthIdentityStoreUnavailable, err = meter.Int64Counter(
		"eshu_dp_auth_identity_store_unavailable_total",
		metric.WithDescription("Credentials the API could not evaluate because the identity store was unreachable or timed out, by failure_class (unavailable, timeout, topology); each is answered with a retryable 503 instead of a 401 (#7586)"),
	); err != nil {
		return fmt.Errorf("register AuthIdentityStoreUnavailable counter: %w", err)
	}
	return nil
}
