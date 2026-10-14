// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/http2"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// TestIsTransientAcceptsHTTP2StreamErrors pins the predicate for both
// stream-error types: golang.org/x/net/http2's, and the unexported bundled
// net/http type matched by name (see the real-server test below).
func TestIsTransientAcceptsHTTP2StreamErrors(t *testing.T) {
	t.Parallel()

	cancel := http2.StreamError{StreamID: 15, Code: http2.ErrCodeCancel}
	if sdk.IsTransientTransportError(context.Background(), cancel) {
		t.Fatal("sdk accepts an HTTP/2 stream error now; the rest type check is no longer needed")
	}
	if !rest.IsTransient(context.Background(), fmt.Errorf("decode: %w", cancel)) {
		t.Fatal("x/net http2.StreamError CANCEL not accepted as transient")
	}
	if got := rest.TransportClass(cancel); got != "reset" {
		t.Fatalf("class = %q, want reset", got)
	}
	if rest.IsTransient(context.Background(), errors.New("invalid character 'x' looking for beginning of value")) {
		t.Fatal("a JSON syntax error must not be transient")
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if rest.IsTransient(cancelled, cancel) {
		t.Fatal("a stream error during shutdown must not be retried")
	}
}

// TestIsTransientAcceptsTheBundledHTTP2StreamError proves the real net/http
// HTTP/2 body failure (the 2026-10-13 stream reset shape) against a server.
// The bundled type is unexported, so the predicate matches its name; if Go
// renames it this test fails.
func TestIsTransientAcceptsTheBundledHTTP2StreamError(t *testing.T) {
	t.Parallel()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":1,"full_na`))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // RST_STREAM mid-body
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.ProtoMajor != 2 {
		t.Fatalf("protocol = %s, want HTTP/2", response.Proto)
	}
	var payload []map[string]any
	decodeErr := json.NewDecoder(response.Body).Decode(&payload)
	_, _ = io.Copy(io.Discard, response.Body)
	if decodeErr == nil {
		t.Fatal("decode error = nil, want the stream reset")
	}
	if sdk.IsTransientTransportError(context.Background(), decodeErr) {
		t.Fatalf("sdk accepts %T now; the rest type check is no longer needed", decodeErr)
	}
	if !rest.IsTransient(context.Background(), decodeErr) {
		t.Fatalf("IsTransient(%T %v) = false, want true", decodeErr, decodeErr)
	}
	if got := rest.TransportClass(decodeErr); got != "reset" {
		t.Fatalf("class = %q, want reset", got)
	}
}
