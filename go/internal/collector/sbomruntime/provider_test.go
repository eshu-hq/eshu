// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

func TestHTTPProviderFetchesOCIReferrerDocumentBlob(t *testing.T) {
	t.Parallel()

	const blobDigest = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	raw := readFixture(t, "../sbomdocument/testdata/cyclonedx_image_subject.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/team/api/manifests/" + testReferrerDigest:
			if got := r.Header.Get("Accept"); !strings.Contains(got, "application/vnd.oci.artifact.manifest.v1+json") {
				t.Fatalf("manifest Accept = %q, want artifact manifest support", got)
			}
			w.Header().Set("Content-Type", "application/vnd.oci.artifact.manifest.v1+json")
			_, _ = w.Write([]byte(`{"schemaVersion":2,"blobs":[{"mediaType":"application/vnd.cyclonedx+json","digest":"` + blobDigest + `","size":123}]}`))
		case "/v2/team/api/blobs/" + blobDigest:
			w.Header().Set("Content-Type", "application/vnd.cyclonedx+json")
			_, _ = w.Write(raw)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := HTTPProvider{
		HTTPClient: server.Client(),
		Now:        func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) },
	}
	doc, err := provider.FetchDocument(context.Background(), TargetConfig{
		ScopeID:        "sbom://oci/team/api",
		SourceType:     SourceTypeOCIReferrer,
		ArtifactKind:   ArtifactKindSBOM,
		DocumentFormat: DocumentFormatCycloneDX,
		Provider:       "oci",
		Registry:       server.URL,
		Repository:     "team/api",
		SubjectDigest:  testSubjectDigest,
		ReferrerDigest: testReferrerDigest,
		MaxBytes:       defaultMaxDocumentBytes,
	})
	if err != nil {
		t.Fatalf("FetchDocument() error = %v, want nil", err)
	}
	if string(doc.Body) != string(raw) {
		t.Fatal("FetchDocument() returned unexpected document body")
	}
	if got, want := doc.SourceRecordID, testReferrerDigest; got != want {
		t.Fatalf("SourceRecordID = %q, want %q", got, want)
	}
	if !strings.Contains(doc.SourceURI, "@"+blobDigest) {
		t.Fatalf("SourceURI = %q, want blob digest", doc.SourceURI)
	}
}

func TestHTTPProviderConfiguredSourceStatusFailureWrapsSDKHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "provider body mentions token-secret and private/sbom.json", http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := (HTTPProvider{HTTPClient: server.Client()}).FetchDocument(context.Background(), TargetConfig{
		ScopeID:        "sbom://configured/private",
		SourceType:     SourceTypeConfigured,
		ArtifactKind:   ArtifactKindSBOM,
		DocumentFormat: DocumentFormatCycloneDX,
		DocumentURL:    server.URL + "/private/sbom.json?token=secret",
		BearerToken:    "token-secret",
		MaxBytes:       defaultMaxDocumentBytes,
	})
	if err == nil {
		t.Fatal("FetchDocument() error = nil, want rate-limit failure")
	}
	var failure collector.RegistryFailure
	if !errors.As(err, &failure) {
		t.Fatalf("FetchDocument() error = %T, want collector.RegistryFailure", err)
	}
	if got, want := failure.FailureClass(), collector.RegistryFailureRateLimited; got != want {
		t.Fatalf("FailureClass() = %q, want %q", got, want)
	}
	if got, want := failure.FailureDetails(), "provider=sbom_attestation operation=fetch_document status_code=429"; got != want {
		t.Fatalf("FailureDetails() = %q, want %q", got, want)
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("FetchDocument() error = %T, want sdk.HTTPError", err)
	}
	if got, want := httpErr.Provider, "sbom_attestation"; got != want {
		t.Fatalf("HTTPError.Provider = %q, want %q", got, want)
	}
	if got, want := httpErr.StatusCode, http.StatusTooManyRequests; got != want {
		t.Fatalf("HTTPError.StatusCode = %d, want %d", got, want)
	}
	if got, want := httpErr.RetryAfter, 7*time.Second; got != want {
		t.Fatalf("HTTPError.RetryAfter = %v, want %v", got, want)
	}
	assertErrorRedactsConfiguredSource(t, err, "token-secret", "token=secret", "private/sbom.json")
}

func TestHTTPProviderConfiguredSourceTransportFailureWrapsSDKHTTPError(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("dial failed for private/sbom.json with token-secret")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, transportErr
	})}

	_, err := (HTTPProvider{HTTPClient: client}).FetchDocument(context.Background(), TargetConfig{
		ScopeID:        "sbom://configured/private",
		SourceType:     SourceTypeConfigured,
		ArtifactKind:   ArtifactKindSBOM,
		DocumentFormat: DocumentFormatCycloneDX,
		DocumentURL:    "https://user:secret@sbom.example.com/private/sbom.json?token=secret",
		BearerToken:    "token-secret",
		MaxBytes:       defaultMaxDocumentBytes,
	})
	if err == nil {
		t.Fatal("FetchDocument() error = nil, want transport failure")
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("FetchDocument() error = %v, want wrapped transport cause", err)
	}
	var failure collector.RegistryFailure
	if !errors.As(err, &failure) {
		t.Fatalf("FetchDocument() error = %T, want collector.RegistryFailure", err)
	}
	if got, want := failure.FailureClass(), collector.RegistryFailureRetryable; got != want {
		t.Fatalf("FailureClass() = %q, want %q", got, want)
	}
	if got, want := failure.FailureDetails(), "provider=sbom_attestation operation=fetch_document"; got != want {
		t.Fatalf("FailureDetails() = %q, want %q", got, want)
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("FetchDocument() error = %T, want sdk.HTTPError", err)
	}
	if got, want := httpErr.Provider, "sbom_attestation"; got != want {
		t.Fatalf("HTTPError.Provider = %q, want %q", got, want)
	}
	if httpErr.StatusCode != 0 {
		t.Fatalf("HTTPError.StatusCode = %d, want 0 for transport failure", httpErr.StatusCode)
	}
	assertErrorRedactsConfiguredSource(t, err, "user:secret", "token-secret", "token=secret", "private/sbom.json")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func assertErrorRedactsConfiguredSource(t *testing.T, err error, disallowed ...string) {
	t.Helper()

	rendered := err.Error()
	var failure collector.RegistryFailure
	if errors.As(err, &failure) {
		rendered += " " + failure.FailureDetails()
	}
	var httpErr sdk.HTTPError
	if errors.As(err, &httpErr) {
		rendered += " " + httpErr.Error()
	}
	for _, value := range disallowed {
		if strings.Contains(rendered, value) {
			t.Fatalf("error leaked %q: %q", value, rendered)
		}
	}
}
