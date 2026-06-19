package ecr

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

// TestNewReferrerClientUsesTokenExchangeBasicAuth proves the referrer client
// mints Distribution basic auth from an ECR GetAuthorizationToken exchange and
// reaches an ECR-shaped registry that rejects unauthenticated requests.
func TestNewReferrerClientUsesTokenExchangeBasicAuth(t *testing.T) {
	t.Parallel()

	const fakeToken = "fake-ecr-password-blob"
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("AWS:"+fakeToken))

	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if sawAuth != wantAuth {
			w.Header().Set("WWW-Authenticate", `Basic realm="https://ecr",service="ecr.amazonaws.com"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		_, _ = w.Write([]byte(`{"schemaVersion":2}`))
	}))
	defer server.Close()

	tokenAPI := &fakeAuthorizationTokenAPI{
		t: t,
		output: &ecr.GetAuthorizationTokenOutput{
			AuthorizationData: []types.AuthorizationData{{
				AuthorizationToken: awsv2.String(base64.StdEncoding.EncodeToString([]byte("AWS:" + fakeToken))),
				ProxyEndpoint:      awsv2.String(server.URL),
			}},
		},
	}

	client, err := NewReferrerClient(context.Background(), ReferrerClientOptions{
		AuthorizationClient: tokenAPI,
		RegistryHost:        server.URL,
		HTTPClient:          server.Client(),
	})
	if err != nil {
		t.Fatalf("NewReferrerClient() error = %v", err)
	}

	if _, err := client.GetManifest(context.Background(), "team/api", testReferrerDigest()); err != nil {
		t.Fatalf("GetManifest() error = %v (sawAuth=%q)", err, sawAuth)
	}
	if tokenAPI.calledCount != 1 {
		t.Fatalf("token exchange calledCount = %d, want 1", tokenAPI.calledCount)
	}
}

// TestNewReferrerClientUsesProxyEndpointWhenHostBlank proves the proxy endpoint
// from the token exchange seeds the registry host when none is configured.
func TestNewReferrerClientUsesProxyEndpointWhenHostBlank(t *testing.T) {
	t.Parallel()

	const fakeToken = "fake-ecr-password-blob"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		_, _ = w.Write([]byte(`{"schemaVersion":2}`))
	}))
	defer server.Close()

	tokenAPI := &fakeAuthorizationTokenAPI{
		t: t,
		output: &ecr.GetAuthorizationTokenOutput{
			AuthorizationData: []types.AuthorizationData{{
				AuthorizationToken: awsv2.String(base64.StdEncoding.EncodeToString([]byte("AWS:" + fakeToken))),
				ProxyEndpoint:      awsv2.String(server.URL),
			}},
		},
	}

	client, err := NewReferrerClient(context.Background(), ReferrerClientOptions{
		AuthorizationClient: tokenAPI,
		HTTPClient:          server.Client(),
	})
	if err != nil {
		t.Fatalf("NewReferrerClient() error = %v", err)
	}
	if _, err := client.GetManifest(context.Background(), "team/api", testReferrerDigest()); err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}
}

// TestNewReferrerClientRequiresAuthorizationClient guards the required seam.
func TestNewReferrerClientRequiresAuthorizationClient(t *testing.T) {
	t.Parallel()

	if _, err := NewReferrerClient(context.Background(), ReferrerClientOptions{
		RegistryHost: "000000000000.dkr.ecr.us-east-1.amazonaws.com",
	}); err == nil {
		t.Fatal("NewReferrerClient() error = nil, want missing authorization client")
	}
}

func testReferrerDigest() string {
	return "sha256:1111111111111111111111111111111111111111111111111111111111111111"
}
