package distribution

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestClientGetBlobKeepsAuthOnSameHostRedirect proves the Distribution client
// re-attaches its credential when a registry redirects a blob fetch to another
// path on the same registry host. ECR-class registries answer blob GETs with a
// redirect, and the credential must follow same-host hops or the second request
// is rejected with registry_auth_denied even though the credential is valid.
func TestClientGetBlobKeepsAuthOnSameHostRedirect(t *testing.T) {
	t.Parallel()

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("AWS:tok"))
	const blobDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	var redirectAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != wantAuth {
			w.Header().Set("WWW-Authenticate", `Basic realm="ecr"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v2/team/api/blobs/" + blobDigest:
			http.Redirect(w, r, "/internal/blob-store/"+blobDigest, http.StatusTemporaryRedirect)
		case "/internal/blob-store/" + blobDigest:
			redirectAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/vnd.cyclonedx+json")
			_, _ = w.Write([]byte(`{"bomFormat":"CycloneDX"}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:  server.URL,
		Username: "AWS",
		Password: "tok",
		Client:   server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	blob, err := client.GetBlob(context.Background(), "team/api", blobDigest)
	if err != nil {
		t.Fatalf("GetBlob() error = %v (redirectAuth=%q)", err, redirectAuth)
	}
	if string(blob.Body) != `{"bomFormat":"CycloneDX"}` {
		t.Fatalf("GetBlob() body = %q", blob.Body)
	}
	if redirectAuth != wantAuth {
		t.Fatalf("same-host redirect dropped credential: got %q want %q", redirectAuth, wantAuth)
	}
}

// TestClientGetBlobDropsAuthOnCrossHostRedirect proves the Distribution client
// never forwards its registry credential to a different host. ECR redirects blob
// fetches to presigned S3 URLs that reject any extra Authorization header, and
// leaking the registry credential to an unrelated host is a disclosure defect.
func TestClientGetBlobDropsAuthOnCrossHostRedirect(t *testing.T) {
	t.Parallel()

	const (
		blobDigest   = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		registryHost = "registry.ecr.test"
		blobHost     = "blobstore.s3.test"
	)

	var blobStoreAuth string
	var blobStoreCalled bool
	blobStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blobStoreCalled = true
		blobStoreAuth = r.Header.Get("Authorization")
		if blobStoreAuth != "" {
			// Presigned blob stores reject requests carrying an extra credential.
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("PRESIGNED-BLOB"))
	}))
	defer blobStore.Close()

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="ecr"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Redirect to a genuinely different host so the stdlib classifies the
		// hop as cross-host.
		http.Redirect(w, r, "https://"+blobHost+"/presigned/"+blobDigest, http.StatusTemporaryRedirect)
	}))
	defer registry.Close()

	// A rewriting transport maps the synthetic hostnames back to the httptest
	// listeners, so the client sees real cross-host hostnames over loopback.
	transport := &rewritingTransport{
		hostToBackend: map[string]string{
			registryHost: mustHostPort(t, registry.URL),
			blobHost:     mustHostPort(t, blobStore.URL),
		},
	}

	client, err := NewClient(ClientConfig{
		BaseURL:  "http://" + registryHost,
		Username: "AWS",
		Password: "tok",
		Client:   &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	blob, err := client.GetBlob(context.Background(), "team/api", blobDigest)
	if err != nil {
		t.Fatalf("GetBlob() error = %v (blobStoreAuth=%q)", err, blobStoreAuth)
	}
	if !blobStoreCalled {
		t.Fatal("cross-host blob store was never reached")
	}
	if blobStoreAuth != "" {
		t.Fatalf("cross-host redirect leaked credential to blob store: %q", blobStoreAuth)
	}
	if string(blob.Body) != "PRESIGNED-BLOB" {
		t.Fatalf("GetBlob() body = %q", blob.Body)
	}
}

// rewritingTransport routes requests for synthetic test hostnames to real
// httptest listeners while preserving the original request Host. It lets a test
// exercise genuine cross-host redirect behavior over loopback.
type rewritingTransport struct {
	hostToBackend map[string]string
}

func (rt *rewritingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	backend, ok := rt.hostToBackend[req.URL.Hostname()]
	if !ok {
		return nil, &url.Error{Op: "Get", URL: req.URL.String(), Err: errUnknownHost}
	}
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	cloned.URL.Host = backend
	return http.DefaultTransport.RoundTrip(cloned)
}

func mustHostPort(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return parsed.Host
}

var errUnknownHost = &hostError{}

type hostError struct{}

func (*hostError) Error() string { return "unknown synthetic host" }
