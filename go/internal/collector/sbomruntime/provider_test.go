package sbomruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
