// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package distribution

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetVerifiedManifestChecksDigestAndCap(t *testing.T) {
	t.Parallel()
	body := []byte(`{"schemaVersion":2,"layers":[]}`)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.GetVerifiedManifest(context.Background(), "example/app", digest, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Body) != string(body) {
		t.Fatalf("body = %q", got.Body)
	}
	if _, err := client.GetVerifiedManifest(context.Background(), "example/app", digest, int64(len(body)-1)); err == nil {
		t.Fatal("oversized manifest accepted")
	}
	badDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("wrong")))
	if _, err := client.GetVerifiedManifest(context.Background(), "example/app", badDigest, 1024); err == nil {
		t.Fatal("digest mismatch accepted")
	}
}
