// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// skipTransientTransport records one transient transport failure for the
// target at index and reports nil so Next moves on to the next target. A
// dropped connection or network timeout is a property of the registry
// connection, not of the target's configuration: the target is retried next
// cycle. A run of sdk.MaxConsecutiveTransportFailures failed cycles in a row is
// persistent misconfiguration (wrong host or port, removed registry) and
// returns as a fatal error so it crash-loops instead of idling.
func (s *Source) skipTransientTransport(ctx context.Context, index int, target TargetConfig, err error) error {
	if s.transportFailures == nil {
		s.transportFailures = make(map[int]int)
	}
	s.transportFailures[index]++
	failures := s.transportFailures[index]
	causeClass := sdk.TransportFailureClass(err)
	if failures >= sdk.MaxConsecutiveTransportFailures {
		return fmt.Errorf(
			"OCI registry transport failed %d consecutive scan cycles (cause_class=%s): %w",
			failures, causeClass, err,
		)
	}
	s.logTransientTransport(ctx, target, failures, causeClass)
	return nil
}

func (s *Source) logTransientTransport(ctx context.Context, target TargetConfig, failures int, causeClass string) {
	if s.Logger == nil {
		return
	}
	s.Logger.WarnContext(
		ctx, "OCI registry scan hit a transient transport error; retrying next cycle",
		telemetry.PhaseAttr(telemetry.PhaseDiscovery),
		log.Provider(string(target.Provider)),
		slog.String("failure_class", string(sdk.FailureRetryable)),
		slog.String("cause_class", causeClass),
		slog.Int("consecutive_transport_failures", failures),
	)
}
