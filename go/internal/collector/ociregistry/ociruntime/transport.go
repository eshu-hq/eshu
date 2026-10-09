// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// isSkippableTransientError reports whether a direct-mode scan failure skips
// the target for the cycle instead of exiting the collector: a transient
// transport error, or an HTTP status the registry failure classes mark
// retryable (408, 5xx) or rate-limited (429). Cancellation, auth denial,
// not-found, terminal statuses, and content failures still propagate.
func isSkippableTransientError(ctx context.Context, err error) bool {
	if sdk.IsTransientTransportError(ctx, err) {
		return true
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	_, ok := retryableStatusCode(err)
	return ok
}

// retryableStatusCode returns the HTTP status when err carries an SDK HTTP
// failure whose status the registry classes mark retryable or rate-limited.
// Transport failures carry status 0, which maps to terminal, so they only
// skip through the transport classifier.
func retryableStatusCode(err error) (int, bool) {
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		return 0, false
	}
	switch collector.RegistryFailureClassForHTTPStatus(httpErr.StatusCode) {
	case collector.RegistryFailureRetryable, collector.RegistryFailureRateLimited:
		return httpErr.StatusCode, true
	default:
		return 0, false
	}
}

// scanFailureResult maps a failed scan to its scan-duration metric result
// value: skipped transport failures record retryable_transport, skipped
// retryable HTTP statuses record retryable_status, everything else failed.
func scanFailureResult(ctx context.Context, err error) string {
	switch {
	case sdk.IsTransientTransportError(ctx, err):
		return "retryable_transport"
	case isSkippableTransientError(ctx, err):
		return "retryable_status"
	default:
		return "failed"
	}
}

// skipCauseClass names the skip cause for the warn log: the registry failure
// class for HTTP status skips, the transport cause otherwise.
func skipCauseClass(ctx context.Context, err error) string {
	if status, ok := retryableStatusCode(err); ok && !sdk.IsTransientTransportError(ctx, err) {
		return collector.RegistryFailureClassForHTTPStatus(status)
	}
	return sdk.TransportFailureClass(err)
}

// skipTransientTransport records one transient failure for the target at
// index and reports nil so Next moves on to the next target. A dropped
// connection, a network timeout, a retryable registry status, or rate
// limiting is a property of the registry connection, not of the target's
// configuration: the target is retried next cycle. A run of
// sdk.MaxConsecutiveTransportFailures failed cycles in a row is a persistent
// outage or misconfiguration (wrong host or port, removed registry) and
// returns as a fatal error so it crash-loops instead of idling.
func (s *Source) skipTransientTransport(ctx context.Context, index int, target TargetConfig, err error) error {
	if s.transportFailures == nil {
		s.transportFailures = make(map[int]int)
	}
	s.transportFailures[index]++
	failures := s.transportFailures[index]
	causeClass := skipCauseClass(ctx, err)
	if failures >= sdk.MaxConsecutiveTransportFailures {
		return fmt.Errorf(
			"OCI registry scan failed %d consecutive scan cycles (cause_class=%s): %w",
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
		ctx, "OCI registry scan hit a transient error; retrying next cycle",
		telemetry.PhaseAttr(telemetry.PhaseDiscovery),
		log.Provider(string(target.Provider)),
		slog.String("failure_class", string(sdk.FailureRetryable)),
		slog.String("cause_class", causeClass),
		slog.Int("consecutive_transport_failures", failures),
	)
}
