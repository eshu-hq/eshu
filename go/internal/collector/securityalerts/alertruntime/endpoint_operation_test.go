// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"errors"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
)

// TestClassifiedProviderFailurePreservesEndpointToken proves issue #8258: the
// runtime wrapper must preserve the inner endpoint token so failure_operation
// carries the endpoint (list_repo_alerts / list_org_alerts), not the
// claim-stage fallback.
func TestClassifiedProviderFailurePreservesEndpointToken(t *testing.T) {
	t.Parallel()

	inner := securityalerts.GitHubDependabotError{
		StatusCode: http.StatusInternalServerError,
		Message:    "fetch github dependabot alerts",
		Operation:  "list_repo_alerts",
	}
	failure := classifiedProviderFailure(inner)
	if got, want := failure.FailureOperation(), "list_repo_alerts"; got != want {
		t.Fatalf("FailureOperation() = %q, want %q", got, want)
	}
	if got, want := failure.FailureClass(), FailureRetryable; got != want {
		t.Fatalf("FailureClass() = %q, want %q (retry contract must not change)", got, want)
	}
	if failure.TerminalFailure() {
		t.Fatal("TerminalFailure() = true, want false for HTTP 500 (retry contract must not change)")
	}
	var ghErr securityalerts.GitHubDependabotError
	if !errors.As(failure, &ghErr) {
		t.Fatalf("failure chain type = %T, want GitHubDependabotError preserved", failure)
	}
}

// TestClassifiedProviderFailureWithoutTokenKeepsStageFallback proves a cause
// with no endpoint token still classifies with a blank operation so claim
// handling falls back to its stage token.
func TestClassifiedProviderFailureWithoutTokenKeepsStageFallback(t *testing.T) {
	t.Parallel()

	failure := classifiedProviderFailure(securityalerts.GitHubDependabotError{
		Message: "github dependabot token is required",
	})
	if got := failure.FailureOperation(); got != "" {
		t.Fatalf("FailureOperation() = %q, want blank for the stage fallback", got)
	}
}
