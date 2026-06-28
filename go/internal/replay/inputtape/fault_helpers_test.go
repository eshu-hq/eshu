// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
)

// containsString reports whether b contains substring s. Delegates to
// bytes.Contains so semantics match the stdlib (containsString(b, "") == true
// for any b, consistent with bytes.Contains). Shared by fault_test.go and any
// future test that needs a bytes-contains helper.
func containsString(b []byte, s string) bool {
	return bytes.Contains(b, []byte(s))
}

// retryOnStatusLoop drives a RoundTripper through a retry-on-5xx loop and
// returns the body of the first 200 response. It creates a fresh replayer from
// tape on each call so the per-key attempt counter resets — proving the tape is
// the unit of determinism. The baseURL and path are used to build each request.
// It fails the test if no 200 is received within maxAttempts.
func retryOnStatusLoop(t *testing.T, tape Tape, baseURL, path string, maxAttempts int) []byte {
	t.Helper()
	replayer, err := NewReplayer(tape, Config{})
	if err != nil {
		t.Fatalf("new replayer: %v", err)
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		resp, err := replayer.RoundTrip(req)
		if err != nil {
			t.Fatalf("attempt %d: transport error: %v", attempt, err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == 200 {
			return b
		}
		if attempt == maxAttempts-1 {
			t.Fatalf("never got 200 after %d attempts", maxAttempts)
		}
	}
	return nil
}

// retryOnTimeoutOrStatus drives a RoundTripper through a retry loop that retries
// on ErrFaultTimeout and 5xx statuses, returning the body of the first 200.
// Creates a fresh replayer each call for determinism proof.
func retryOnTimeoutOrStatus(t *testing.T, tape Tape, baseURL, path string, maxAttempts int) string {
	t.Helper()
	replayer, err := NewReplayer(tape, Config{})
	if err != nil {
		t.Fatalf("new replayer: %v", err)
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		resp, err := replayer.RoundTrip(req)
		if err != nil {
			if errors.Is(err, ErrFaultTimeout) {
				continue
			}
			t.Fatalf("attempt %d unexpected error: %v", attempt, err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return string(b)
	}
	t.Fatalf("never succeeded after %d retries", maxAttempts)
	return ""
}
