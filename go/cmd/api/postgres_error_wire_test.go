// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type postgresReaderProxy struct {
	listener net.Listener
	target   string
	mu       sync.Mutex
	sockets  []net.Conn
	stopped  bool
}

func newPostgresReaderProxy(t *testing.T, endpoint string) *postgresReaderProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxy := &postgresReaderProxy{listener: listener, target: endpoint}
	t.Cleanup(proxy.stop)
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			backend, err := net.DialTimeout("tcp", endpoint, time.Second)
			if err != nil {
				_ = client.Close()
				continue
			}
			proxy.mu.Lock()
			if proxy.stopped {
				_ = client.Close()
				_ = backend.Close()
				proxy.mu.Unlock()
				continue
			}
			proxy.sockets = append(proxy.sockets, client, backend)
			proxy.mu.Unlock()
			go func() { _, _ = io.Copy(backend, client); _ = client.Close(); _ = backend.Close() }()
			go func() { _, _ = io.Copy(client, backend); _ = client.Close(); _ = backend.Close() }()
		}
	}()
	return proxy
}

func (p *postgresReaderProxy) stop() {
	_ = p.listener.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
	for _, socket := range p.sockets {
		_ = socket.Close()
	}
}

func TestPostgresReaderOutageAPIWireDoesNotExposeEndpoint(t *testing.T) {
	writerDSN := os.Getenv("ESHU_AUTH_QUALIFIED_DSN")
	readerDSN := os.Getenv("ESHU_AUTH_QUALIFIED_READ_DSN")
	if writerDSN == "" || readerDSN == "" {
		t.Skip("owned migrated PostgreSQL writer and reader DSNs required")
	}
	readerURL, err := url.Parse(readerDSN)
	if err != nil {
		t.Fatal(err)
	}
	if readerURL.Host == "" || readerURL.Path == "" || readerURL.User == nil {
		t.Fatal("reader DSN must name a host, database, and user")
	}
	originalHost := readerURL.Host
	proxy := newPostgresReaderProxy(t, readerURL.Host)
	readerURL.Host = proxy.listener.Addr().String()
	const apiKey = "reader-outage-wire-proof"
	values := map[string]string{
		"ESHU_POSTGRES_DSN":                 writerDSN,
		"ESHU_POSTGRES_READ_DSN":            readerURL.String(),
		"ESHU_POSTGRES_MAX_OPEN_CONNS":      "4",
		"ESHU_POSTGRES_READ_MAX_OPEN_CONNS": "1",
		"ESHU_QUERY_PROFILE":                "local_lightweight",
		"ESHU_DISABLE_NEO4J":                "true",
		"ESHU_AUTH_BOOTSTRAP_MODE":          "disabled",
		"ESHU_API_KEY":                      apiKey,
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	handler, cleanup, _, err := wireAPI(ctx, func(key string) string { return values[key] }, nil, nil)
	if err != nil {
		t.Fatalf("wire API: %v", err)
	}
	defer cleanup()
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/status/index", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if got := request(); got.Code != http.StatusOK {
		t.Fatalf("healthy reader status=%d body=%s", got.Code, got.Body.String())
	}
	proxy.stop()
	for attempt := 0; attempt < 2; attempt++ {
		got := request()
		if got.Code != http.StatusInternalServerError {
			t.Fatalf("reader outage attempt %d status=%d body=%s", attempt, got.Code, got.Body.String())
		}
		body := got.Body.String()
		for _, secret := range []string{originalHost, readerURL.Host, readerURL.User.Username(), readerURL.Path[1:], "password", "connection refused", "127.0.0.1"} {
			if secret != "" && strings.Contains(body, secret) {
				t.Errorf("public API exposed backend metadata %q on attempt %d", secret, attempt)
			}
		}
	}
}
