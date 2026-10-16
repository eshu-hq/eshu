// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestProviderFailurePreservesStructuredClassification(t *testing.T) {
	t.Parallel()

	cause := HTTPError{Provider: "jira", StatusCode: http.StatusBadRequest, Message: "Bad Request"}
	failure := ClassifyProviderFailure("jira", cause, StatusPolicy{}, FailureRetryable)

	if got, want := failure.FailureClass(), string(FailureTerminal); got != want {
		t.Fatalf("FailureClass() = %q, want %q", got, want)
	}
	if !failure.TerminalFailure() {
		t.Fatal("TerminalFailure() = false, want true for HTTP 400")
	}
	if got, want := failure.FailureStatusCode(), http.StatusBadRequest; got != want {
		t.Fatalf("FailureStatusCode() = %d, want %d", got, want)
	}
}

func TestProviderFailureStatusCodeUnknownWithoutHTTPStatus(t *testing.T) {
	t.Parallel()

	failure := NewProviderFailure("jira", FailureRetryable, false, errors.New("dial tcp: connection refused"))
	if got := failure.FailureStatusCode(); got != 0 {
		t.Fatalf("FailureStatusCode() = %d, want 0 for non-HTTP failures", got)
	}
	if got := failure.FailureOperation(); got != "" {
		t.Fatalf("FailureOperation() = %q, want empty when unset", got)
	}
}

func TestProviderFailureWithOperationKeepsBoundedToken(t *testing.T) {
	t.Parallel()

	failure := NewProviderFailure("jira", FailureTerminal, true, errors.New("bad request")).WithOperation("search_issues")
	if got, want := failure.FailureOperation(), "search_issues"; got != want {
		t.Fatalf("FailureOperation() = %q, want %q", got, want)
	}
}

func TestProviderFailureOperationSanitizesUnsafeInput(t *testing.T) {
	t.Parallel()

	failure := NewProviderFailure("jira", FailureTerminal, true, errors.New("bad request")).
		WithOperation("https://jira.example.test/rest/api/3/search?token=secret")
	operation := failure.FailureOperation()
	for _, leaked := range []string{"://", "token=secret", "jira.example.test"} {
		if strings.Contains(operation, leaked) {
			t.Fatalf("FailureOperation() = %q, must not contain %q", operation, leaked)
		}
	}
	if operation == "" {
		t.Fatal("FailureOperation() is empty, want a bounded fallback token")
	}
	if got := failure.FailureStatusCode(); got != 0 {
		t.Fatalf("FailureStatusCode() = %d, want 0 when no HTTP status is known", got)
	}
}

func TestProviderFailureErrorStaysBoundedWithoutOperation(t *testing.T) {
	t.Parallel()

	cause := HTTPError{Provider: "jira", StatusCode: http.StatusBadRequest, Message: "Bad Request"}
	failure := ClassifyProviderFailure("jira", cause, StatusPolicy{}, FailureRetryable)
	if got, want := failure.Error(), "jira provider failure: terminal"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
