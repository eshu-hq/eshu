package provider

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPostJSONDecodesSuccess verifies that a 200 response is decoded into out,
// that the request carries Content-Type: application/json, and that custom
// headers are forwarded.
func TestPostJSONDecodesSuccess(t *testing.T) {
	t.Parallel()

	type respBody struct {
		OK bool `json:"ok"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type: got %q, want %q", got, "application/json")
		}
		if got := r.Header.Get("X-Custom"); got != "sentinel" {
			t.Errorf("X-Custom: got %q, want %q", got, "sentinel")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(respBody{OK: true})
	}))
	defer srv.Close()

	tr := newTransport(srv.Client())
	var out respBody
	err := tr.postJSON(t.Context(), srv.URL, map[string]string{"X-Custom": "sentinel"}, map[string]any{"ping": 1}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.OK {
		t.Fatal("expected out.OK to be true")
	}
}

// TestPostJSONRetriesOn503ThenSucceeds verifies that postJSON retries on a 503
// and ultimately succeeds on the second attempt, without making more than 2
// total requests.
func TestPostJSONRetriesOn503ThenSucceeds(t *testing.T) {
	t.Parallel()

	type respBody struct {
		Done bool `json:"done"`
	}

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(respBody{Done: true})
	}))
	defer srv.Close()

	tr := newTransport(srv.Client())
	var out respBody
	err := tr.postJSON(t.Context(), srv.URL, nil, struct{}{}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected exactly 2 attempts, got %d", attempts)
	}
	if !out.Done {
		t.Fatal("expected out.Done to be true")
	}
}

// TestPostJSONReturnsProviderErrorOn400WithoutBody verifies that a 400 response
// produces a *ProviderError with StatusCode==400 and that the error message does
// NOT contain the raw response body (i.e. no body leakage).
func TestPostJSONReturnsProviderErrorOn400WithoutBody(t *testing.T) {
	t.Parallel()

	const secretBody = "TOP_SECRET_RESPONSE_XYZ"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(secretBody))
	}))
	defer srv.Close()

	tr := newTransport(srv.Client())
	var out struct{}
	err := tr.postJSON(t.Context(), srv.URL, nil, struct{}{}, &out)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	var provErr *ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if provErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected StatusCode 400, got %d", provErr.StatusCode)
	}
	if msg := provErr.Error(); contains(msg, secretBody) {
		t.Fatalf("error message must not contain secret body; got: %q", msg)
	}
}

// contains is a simple substring check used to keep the test readable.
func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && findSubstr(s, sub)
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
