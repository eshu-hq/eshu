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
