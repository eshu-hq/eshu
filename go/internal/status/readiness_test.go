// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestStatusWrappersForwardReadiness(t *testing.T) {
	t.Parallel()

	want := errors.New("schema unavailable")
	base := &fakeReader{readinessErr: want}
	for name, reader := range map[string]status.Reader{
		"retry policies":    status.WithRetryPolicies(base, status.RetryPolicySummary{Stage: "reducer"}),
		"provider profiles": status.WithSemanticProviderProfiles(base, status.SemanticProviderProfileStatus{ProfileID: "test"}),
	} {
		t.Run(name, func(t *testing.T) {
			checker, ok := reader.(status.ReadinessChecker)
			if !ok {
				t.Fatal("wrapped reader does not implement ReadinessChecker")
			}
			if err := checker.CheckStatusReadiness(context.Background()); !errors.Is(err, want) {
				t.Fatalf("CheckStatusReadiness() error = %v, want %v", err, want)
			}
		})
	}
}

func TestStatusWrapperFailsClosedWithoutReadinessChecker(t *testing.T) {
	t.Parallel()

	base := readerWithoutReadiness{Reader: &fakeReader{}}
	wrapped := status.WithRetryPolicies(base)
	checker := wrapped.(status.ReadinessChecker)
	if err := checker.CheckStatusReadiness(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "does not support readiness") {
		t.Fatalf("CheckStatusReadiness() error = %v, want unsupported-reader error", err)
	}
}

type readerWithoutReadiness struct {
	status.Reader
}

// startupReader is a fake reader that reports a startup configuration error.
type startupReader struct {
	fakeReader
	err error
}

func (r *startupReader) StartupError() error { return r.err }

func TestStatusWrappersForwardStartupError(t *testing.T) {
	t.Parallel()

	want := errors.New("ESHU_STATUS_SUMMARY_STALE_AFTER=\"soon\": invalid")
	base := &startupReader{err: want}
	retry := status.WithRetryPolicies(base, status.RetryPolicySummary{Stage: "reducer"})
	profiles := status.WithSemanticProviderProfiles(base, status.SemanticProviderProfileStatus{ProfileID: "test"})
	for name, reader := range map[string]status.Reader{
		"retry policies":    retry,
		"provider profiles": profiles,
		"stacked":           status.WithSemanticProviderProfiles(retry, status.SemanticProviderProfileStatus{ProfileID: "test"}),
	} {
		if err := status.ReaderStartupError(reader); !errors.Is(err, want) {
			t.Fatalf("%s: ReaderStartupError() = %v, want %v", name, err, want)
		}
	}
	if err := status.ReaderStartupError(&fakeReader{}); err != nil {
		t.Fatalf("a reader with no startup reporter: ReaderStartupError() = %v, want nil", err)
	}
}
