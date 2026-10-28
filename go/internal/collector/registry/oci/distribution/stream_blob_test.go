// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package distribution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamBlobVerifiesDigestAndBoundsBytes(t *testing.T) {
	t.Parallel()
	body := bytes.Repeat([]byte("layer data"), 200000)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/blobs/"+digest) {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := client.StreamBlob(context.Background(), "example/app", digest, int64(len(body)), int64(len(body)), &output); err != nil {
		t.Fatalf("StreamBlob() error = %v", err)
	}
	if !bytes.Equal(output.Bytes(), body) {
		t.Fatal("streamed blob differs from verified source")
	}
	output.Reset()
	if _, err := client.StreamBlob(context.Background(), "example/app", digest, int64(len(body)), int64(len(body)-1), &output); err == nil {
		t.Fatal("StreamBlob() passed byte cap, want failure")
	}
}

func TestStreamBlobRejectsDigestMismatch(t *testing.T) {
	t.Parallel()
	body := []byte("wrong content")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("expected content")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.StreamBlob(context.Background(), "example/app", digest, int64(len(body)), 1024, &bytes.Buffer{}); err == nil {
		t.Fatal("StreamBlob() accepted wrong digest")
	}
}

func TestStreamBlobRejectsShortRead(t *testing.T) {
	t.Parallel()
	body := []byte("short content")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.StreamBlob(context.Background(), "example/app", digest, int64(len(body)+1), 1024, &bytes.Buffer{}); err == nil {
		t.Fatal("StreamBlob() accepted short response")
	}
}

func TestStreamBlobRejectsInvalidDigestBeforeRequest(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.StreamBlob(context.Background(), "example/app", "sha256:invalid", 4, 1024, &bytes.Buffer{}); err == nil {
		t.Fatal("StreamBlob() accepted invalid digest")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want zero", requests)
	}
}
