// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomruntime

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	awsecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ecr"
)

// stubAuthorizationTokenAPI is a synthetic ECR GetAuthorizationToken endpoint.
// It returns a fake base64 "AWS:<token>" blob and never contacts AWS.
type stubAuthorizationTokenAPI struct {
	token       string
	proxy       string
	calledCount int
}

func (s *stubAuthorizationTokenAPI) GetAuthorizationToken(
	context.Context,
	*awsecr.GetAuthorizationTokenInput,
	...func(*awsecr.Options),
) (*awsecr.GetAuthorizationTokenOutput, error) {
	s.calledCount++
	return &awsecr.GetAuthorizationTokenOutput{
		AuthorizationData: []awsecrtypes.AuthorizationData{{
			AuthorizationToken: awsv2.String(base64.StdEncoding.EncodeToString([]byte("AWS:" + s.token))),
			ProxyEndpoint:      awsv2.String(s.proxy),
		}},
	}, nil
}

// TestHTTPProviderFetchesECROCIReferrerViaTokenExchange proves an oci_referrer
// target with provider=ecr and no static credentials fetches the referrer SBOM
// using AWS-default-chain (token-exchange) credentials.
func TestHTTPProviderFetchesECROCIReferrerViaTokenExchange(t *testing.T) {
	t.Parallel()

	const fakeToken = "fake-ecr-token-blob"
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("AWS:"+fakeToken))
	raw := readFixture(t, "../sbomdocument/testdata/cyclonedx_image_subject.json")

	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if sawAuth != wantAuth {
			w.Header().Set("WWW-Authenticate", `Basic realm="https://ecr",service="ecr.amazonaws.com"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v2/team/api/manifests/"+testReferrerDigest {
			w.Header().Set("Content-Type", "application/vnd.cyclonedx+json")
			_, _ = w.Write(raw)
			return
		}
		t.Fatalf("unexpected path %q", r.URL.Path)
	}))
	defer server.Close()

	tokenAPI := &stubAuthorizationTokenAPI{token: fakeToken, proxy: server.URL}
	provider := HTTPProvider{
		HTTPClient: server.Client(),
		Now:        func() time.Time { return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC) },
		ClientFactory: ECRReferrerClientFactory{
			HTTPClient: server.Client(),
			AuthorizationClient: func(context.Context, TargetConfig) (ecr.AuthorizationTokenAPI, error) {
				return tokenAPI, nil
			},
		},
	}

	doc, err := provider.FetchDocument(context.Background(), TargetConfig{
		ScopeID:        "sbom://oci/team/api",
		SourceType:     SourceTypeOCIReferrer,
		ArtifactKind:   ArtifactKindSBOM,
		DocumentFormat: DocumentFormatCycloneDX,
		Provider:       "ecr",
		Registry:       server.URL,
		Repository:     "team/api",
		SubjectDigest:  testSubjectDigest,
		ReferrerDigest: testReferrerDigest,
		MaxBytes:       defaultMaxDocumentBytes,
	})
	if err != nil {
		t.Fatalf("FetchDocument() error = %v (sawAuth=%q want=%q)", err, sawAuth, wantAuth)
	}
	if string(doc.Body) != string(raw) {
		t.Fatal("FetchDocument() returned unexpected document body")
	}
	if tokenAPI.calledCount != 1 {
		t.Fatalf("token exchange calledCount = %d, want 1", tokenAPI.calledCount)
	}
	if got := doc.SourceRecordID; got != testReferrerDigest {
		t.Fatalf("SourceRecordID = %q, want %q", got, testReferrerDigest)
	}
	if strings.Contains(doc.SourceURI, fakeToken) {
		t.Fatalf("SourceURI leaked token: %q", doc.SourceURI)
	}
}

// TestHTTPProviderECRFactoryIgnoredForNonECRProvider proves the factory only
// engages the token-exchange path for provider=ecr targets and leaves other
// providers on the static-credential path.
func TestHTTPProviderECRFactoryIgnoredForNonECRProvider(t *testing.T) {
	t.Parallel()

	tokenAPI := &stubAuthorizationTokenAPI{token: "unused", proxy: "https://ecr.invalid"}
	raw := readFixture(t, "../sbomdocument/testdata/cyclonedx_image_subject.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/team/api/manifests/"+testReferrerDigest {
			w.Header().Set("Content-Type", "application/vnd.cyclonedx+json")
			_, _ = w.Write(raw)
			return
		}
		t.Fatalf("unexpected path %q", r.URL.Path)
	}))
	defer server.Close()

	provider := HTTPProvider{
		HTTPClient: server.Client(),
		ClientFactory: ECRReferrerClientFactory{
			HTTPClient: server.Client(),
			AuthorizationClient: func(context.Context, TargetConfig) (ecr.AuthorizationTokenAPI, error) {
				return tokenAPI, nil
			},
		},
	}

	if _, err := provider.FetchDocument(context.Background(), TargetConfig{
		ScopeID:        "sbom://oci/team/api",
		SourceType:     SourceTypeOCIReferrer,
		ArtifactKind:   ArtifactKindSBOM,
		DocumentFormat: DocumentFormatCycloneDX,
		Provider:       "harbor",
		Registry:       server.URL,
		Repository:     "team/api",
		SubjectDigest:  testSubjectDigest,
		ReferrerDigest: testReferrerDigest,
		MaxBytes:       defaultMaxDocumentBytes,
	}); err != nil {
		t.Fatalf("FetchDocument() error = %v", err)
	}
	if tokenAPI.calledCount != 0 {
		t.Fatalf("token exchange calledCount = %d, want 0 for non-ecr provider", tokenAPI.calledCount)
	}
}
