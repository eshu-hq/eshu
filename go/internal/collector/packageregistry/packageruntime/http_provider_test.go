package packageruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
)

func TestHTTPMetadataProviderFetchesMetadataWithBearerAuth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer token-123"; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"name":"team-api","version":"1.2.3"}`))
	}))
	defer server.Close()

	document, err := HTTPMetadataProvider{Client: server.Client()}.FetchMetadata(context.Background(), TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:  "jfrog",
			Ecosystem: packageregistry.EcosystemGeneric,
			Registry:  "https://artifactory.example.com",
			ScopeID:   "package-registry://jfrog/generic/team-api",
		},
		MetadataURL: server.URL,
		BearerToken: "token-123",
	})
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v, want nil", err)
	}
	if got, want := string(document.Body), `{"name":"team-api","version":"1.2.3"}`; got != want {
		t.Fatalf("Body = %q, want %q", got, want)
	}
}
