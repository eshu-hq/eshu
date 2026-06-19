package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	awsecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ecr"
	"github.com/eshu-hq/eshu/go/internal/collector/sbomruntime"
)

// TestNewDocumentProviderWiresECRReferrerFactory proves the SBOM attestation
// collector builds its document provider with the ECR oci_referrer auth path
// wired in, so provider=ecr targets reach the GetAuthorizationToken exchange
// instead of falling back to empty static credentials.
func TestNewDocumentProviderWiresECRReferrerFactory(t *testing.T) {
	t.Parallel()

	provider := newDocumentProvider(nil)
	factory, ok := provider.ClientFactory.(sbomruntime.ECRReferrerClientFactory)
	if !ok {
		t.Fatalf("ClientFactory type = %T, want sbomruntime.ECRReferrerClientFactory", provider.ClientFactory)
	}
	if factory.AuthorizationClient == nil {
		t.Fatal("ECRReferrerClientFactory.AuthorizationClient = nil, want a wired AWS authorization client")
	}
}

// TestNewDocumentProviderECRFactoryFetchesReferrer proves the wired factory
// performs the ECR token exchange and fetches an oci_referrer document end to
// end when a synthetic GetAuthorizationToken client is supplied. It substitutes
// only the AWS authorization seam, exercising the same provider construction the
// collector runtime uses.
func TestNewDocumentProviderECRFactoryFetchesReferrer(t *testing.T) {
	t.Parallel()

	const fakeToken = "fake-ecr-token-blob"
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("AWS:"+fakeToken))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != wantAuth {
			w.Header().Set("WWW-Authenticate", `Basic realm="https://ecr"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.cyclonedx+json")
		_, _ = w.Write([]byte(`{"bomFormat":"CycloneDX","specVersion":"1.5"}`))
	}))
	defer server.Close()

	provider := newDocumentProvider(nil)
	factory := provider.ClientFactory.(sbomruntime.ECRReferrerClientFactory)
	factory.HTTPClient = server.Client()
	factory.AuthorizationClient = func(context.Context, sbomruntime.TargetConfig) (ecr.AuthorizationTokenAPI, error) {
		return stubECRTokenAPI{token: fakeToken, proxy: server.URL}, nil
	}
	provider.ClientFactory = factory

	doc, err := provider.FetchDocument(context.Background(), sbomruntime.TargetConfig{
		ScopeID:        "sbom://oci/team/api",
		SourceType:     sbomruntime.SourceTypeOCIReferrer,
		ArtifactKind:   sbomruntime.ArtifactKindSBOM,
		DocumentFormat: sbomruntime.DocumentFormatCycloneDX,
		Provider:       "ecr",
		Registry:       server.URL,
		Repository:     "team/api",
		ReferrerDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		MaxBytes:       1 << 20,
	})
	if err != nil {
		t.Fatalf("FetchDocument() error = %v", err)
	}
	if len(doc.Body) == 0 {
		t.Fatal("FetchDocument() returned empty document body")
	}
}

// stubECRTokenAPI is a synthetic ECR GetAuthorizationToken endpoint. It returns a
// fake base64 "AWS:<token>" blob and never contacts AWS.
type stubECRTokenAPI struct {
	token string
	proxy string
}

func (s stubECRTokenAPI) GetAuthorizationToken(
	context.Context,
	*awsecr.GetAuthorizationTokenInput,
	...func(*awsecr.Options),
) (*awsecr.GetAuthorizationTokenOutput, error) {
	return &awsecr.GetAuthorizationTokenOutput{
		AuthorizationData: []awsecrtypes.AuthorizationData{{
			AuthorizationToken: awsv2.String(base64.StdEncoding.EncodeToString([]byte("AWS:" + s.token))),
			ProxyEndpoint:      awsv2.String(s.proxy),
		}},
	}, nil
}
