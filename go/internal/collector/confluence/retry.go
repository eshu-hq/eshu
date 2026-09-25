// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package confluence

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const (
	confluenceRetryBaseDelay = time.Second
	confluenceRetryMaxDelay  = time.Minute
)

// ErrRetryable marks a Confluence provider response or transient transport
// failure (connection reset, unexpected EOF, network timeout) that should be
// retried after bounded backoff instead of failing the collector process.
var ErrRetryable = errors.New("confluence retryable provider failure")

// RetryableHTTPError carries bounded Confluence retry metadata without source
// paths, page IDs, titles, URLs, or response bodies. StatusCode is zero when
// the failure was a transient transport error rather than an HTTP status.
type RetryableHTTPError struct {
	StatusCode int
	RetryAfter time.Duration
	Cause      error
}

func (e RetryableHTTPError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("confluence provider retryable status %d", e.StatusCode)
	}
	return "confluence provider retryable failure"
}

// Is lets callers use errors.Is(err, ErrRetryable).
func (e RetryableHTTPError) Is(target error) bool {
	return target == ErrRetryable
}

// Unwrap returns the bounded provider status failure.
func (e RetryableHTTPError) Unwrap() error {
	return e.Cause
}

// RetryAfterDelay returns provider retry guidance for shared retry handling.
func (e RetryableHTTPError) RetryAfterDelay() time.Duration {
	return e.RetryAfter
}

type confluenceRetryState struct {
	attempts int
	// transportFailures counts consecutive transport-class retryable failures
	// (StatusCode 0). A status-class retry or a successful sync resets it, so
	// only an unbroken run of dropped or refused connections escalates.
	transportFailures int
	nextAttemptAt     time.Time
}

func (s *Source) retryBackoffActive(now time.Time) bool {
	return !s.retry.nextAttemptAt.IsZero() && now.Before(s.retry.nextAttemptAt)
}

// recordRetryableFailure schedules a backoff for a retryable provider failure
// and reports whether it did. It returns false, so the caller treats err as a
// fatal source error, for a non-retryable error and for a transport failure
// that has repeated sdk.MaxConsecutiveTransportFailures times in a row: a wrong
// host or port, or a removed service, is persistent misconfiguration that must
// surface as a crash-loop an operator can alert on instead of an idle collector.
func (s *Source) recordRetryableFailure(ctx context.Context, now time.Time, err error) (bool, error) {
	var retryable RetryableHTTPError
	if !errors.As(err, &retryable) {
		return false, err
	}
	s.retry.attempts++
	failureClass := confluenceRetryFailureClass(retryable.StatusCode)
	causeClass := ""
	if retryable.StatusCode == 0 {
		s.retry.transportFailures++
		causeClass = sdk.TransportFailureClass(retryable.Cause)
		if s.retry.transportFailures >= sdk.MaxConsecutiveTransportFailures {
			return false, fmt.Errorf(
				"confluence transport failed %d consecutive attempts (cause_class=%s): %w",
				s.retry.transportFailures, causeClass, retryable.Cause,
			)
		}
	} else {
		s.retry.transportFailures = 0
	}
	s.recordSyncFailure(ctx, failureClass)
	delay := s.retryDelay(retryable)
	s.retry.nextAttemptAt = now.Add(delay)
	if s.Logger != nil {
		attrs := []any{
			"failure_class", failureClass,
			"status_code", retryable.StatusCode,
			"attempt", s.retry.attempts,
			"retry_after_seconds", int64(delay.Seconds()),
		}
		if causeClass != "" {
			attrs = append(attrs,
				"cause_class", causeClass,
				"consecutive_transport_failures", s.retry.transportFailures,
			)
		}
		s.Logger.WarnContext(ctx, "confluence sync retry scheduled", attrs...)
	}
	return true, nil
}

func (s *Source) resetRetryBackoff() {
	s.retry = confluenceRetryState{}
}

func (s *Source) retryDelay(retryable RetryableHTTPError) time.Duration {
	if retryable.RetryAfter > 0 {
		return retryable.RetryAfter
	}
	delay := confluenceRetryBaseDelay
	for attempt := 1; attempt < s.retry.attempts; attempt++ {
		delay *= 2
		if delay >= confluenceRetryMaxDelay {
			delay = confluenceRetryMaxDelay
			break
		}
	}
	return delay + deterministicRetryJitter(s.retryKey(), s.retry.attempts, delay)
}

func (s *Source) retryKey() string {
	if activeSpaceID := s.activeSpaceID(); activeSpaceID != "" {
		return "space:" + activeSpaceID
	}
	if s.Config.RootPageID != "" {
		return "root:" + s.Config.RootPageID
	}
	return strings.Join(s.Config.SpaceIDs, ",")
}

func deterministicRetryJitter(key string, attempt int, delay time.Duration) time.Duration {
	window := delay / 4
	if window <= 0 {
		return 0
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key))
	_, _ = hash.Write([]byte(":"))
	_, _ = hash.Write([]byte(strconv.Itoa(attempt)))
	return time.Duration(hash.Sum64() % uint64(window+1)) // #nosec G115 -- bounded: result is at most window (a duration/4), safe to convert to time.Duration
}

func confluenceRetryFailureClass(statusCode int) string {
	switch statusCode {
	case 0:
		return "transport_error"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "provider_unavailable"
	default:
		return "retryable_status"
	}
}

func confluenceRetryResult(statusCode int) string {
	switch statusCode {
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "retryable_status"
	}
}

func isConfluenceRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode == http.StatusServiceUnavailable
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	return sdk.ParseRetryAfter(value, now)
}
