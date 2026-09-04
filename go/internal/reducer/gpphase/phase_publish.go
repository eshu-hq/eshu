// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import (
	"context"
	"fmt"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// PublishIntentGraphPhase publishes the readiness milestone for one durable
// reducer intent (issue #6061; moved here from the reducer root's
// publishIntentGraphPhase). A nil publisher and an intent that cannot name a
// bounded slice (see [StateForIntentValue]) are both no-ops, so a handler
// wired without readiness publication still runs.
//
// This is the one exception to the rest of this package's plain-data, pure-
// builder contract: it performs the publish I/O through the [PhasePublisher]
// interface. It moved here, rather than staying at the root or following
// platformfam's per-family local-wrapper pattern (see the package doc for
// why that pattern existed and why this supersedes it for the
// reducer-root-owned publish path), because the same four hoisted symbols
// (this, [StateForIntentValue], [EndpointPresenceWriter],
// [EndpointPresenceRow]) are the last root-owned pieces blocking the ec2,
// s3, iam, and security_group families from splitting out of the reducer
// root without importing it.
func PublishIntentGraphPhase(
	ctx context.Context,
	publisher PhasePublisher,
	intent reducercontract.Intent,
	keyspace Keyspace,
	phase Phase,
	observedAt time.Time,
) error {
	if publisher == nil {
		return nil
	}
	state, ok := StateForIntentValue(intent, keyspace, phase, observedAt)
	if !ok {
		return nil
	}
	if err := publisher.PublishGraphProjectionPhases(ctx, []PhaseState{state}); err != nil {
		return fmt.Errorf("publish %s phase: %w", phase, err)
	}
	return nil
}
