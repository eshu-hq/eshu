// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// fingerprintedMetadataJSON is an entity metadata JSONB blob as the store
// holds it for a fingerprinted function: one real key plus every parser
// fingerprint key, so a key added to fingerprint.MetadataKeys() is covered
// without editing this fixture.
func fingerprintedMetadataJSON(t *testing.T) []byte {
	t.Helper()

	metadata := map[string]any{"docstring": "Handles the request."}
	for _, key := range fingerprint.MetadataKeys() {
		metadata[key] = "store-internal-" + key
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal fixture metadata: %v", err)
	}
	return raw
}

func TestDecodeEntityMetadataStripsFingerprintKeys(t *testing.T) {
	t.Parallel()

	got, err := decodeEntityMetadata(fingerprintedMetadataJSON(t))
	if err != nil {
		t.Fatalf("decodeEntityMetadata() error = %v, want nil", err)
	}
	if len(got) != 1 || got["docstring"] != "Handles the request." {
		t.Fatalf("decodeEntityMetadata() = %#v, want only docstring", got)
	}
	for _, key := range fingerprint.MetadataKeys() {
		if _, present := got[key]; present {
			t.Errorf("decodeEntityMetadata() kept store-internal key %q", key)
		}
	}
}

func TestDecodeEntityMetadataReturnsNilWhenOnlyFingerprintKeysRemain(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{}
	for _, key := range fingerprint.MetadataKeys() {
		metadata[key] = "x"
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := decodeEntityMetadata(raw)
	if err != nil {
		t.Fatalf("decodeEntityMetadata() error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("decodeEntityMetadata() = %#v, want nil so metadata,omitempty drops the field", got)
	}
}

func TestDecodeEntityMetadataKeepsNonFingerprintKeys(t *testing.T) {
	t.Parallel()

	got, err := decodeEntityMetadata([]byte(`{"async":true,"decorators":["route"],"body_hash":"kept"}`))
	if err != nil {
		t.Fatalf("decodeEntityMetadata() error = %v, want nil", err)
	}
	if len(got) != 3 {
		t.Fatalf("decodeEntityMetadata() = %#v, want all three non-fingerprint keys", got)
	}
}

// TestContentReaderEntityReadsOmitFingerprintKeys drives the reader methods
// that hand EntityContent to the eight budget-relevant row builders (search,
// language/type, exact-name), so every decode call site is exercised on
// production SQL scanning, not just the helper.
func TestContentReaderEntityReadsOmitFingerprintKeys(t *testing.T) {
	t.Parallel()

	columns := []string{
		"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
		"start_line", "end_line", "language", "source_cache", "metadata",
	}
	row := func(t *testing.T) []driver.Value {
		return []driver.Value{
			"entity-1", "repo-1", "src/app.go", "Function", "handler",
			int64(1), int64(80), "go", "func handler() {}", fingerprintedMetadataJSON(t),
		}
	}

	for _, tc := range []struct {
		name string
		read func(*ContentReader) ([]EntityContent, error)
	}{
		{"SearchEntitiesByLanguageAndType", func(cr *ContentReader) ([]EntityContent, error) {
			return cr.SearchEntitiesByLanguageAndType(context.Background(), "repo-1", "go", "Function", "handler", 5)
		}},
		{"SearchEntitiesByExactName", func(cr *ContentReader) ([]EntityContent, error) {
			return cr.SearchEntitiesByExactName(context.Background(), "repo-1", "Function", "handler", 5)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openContentReaderTestDB(t, []contentReaderQueryResult{
				{columns: columns, rows: [][]driver.Value{row(t)}},
			})
			got, err := tc.read(NewContentReader(db))
			if err != nil {
				t.Fatalf("%s() error = %v, want nil", tc.name, err)
			}
			if len(got) != 1 {
				t.Fatalf("%s() returned %d rows, want 1", tc.name, len(got))
			}
			if got[0].Metadata["docstring"] != "Handles the request." {
				t.Fatalf("%s() metadata = %#v, want docstring kept", tc.name, got[0].Metadata)
			}
			for _, key := range fingerprint.MetadataKeys() {
				if _, present := got[0].Metadata[key]; present {
					t.Errorf("%s() metadata kept store-internal key %q", tc.name, key)
				}
			}
		})
	}
}

// TestCodeHandlerSearchEntityContentResponseOmitsFingerprintKeys is the wire
// proof: the JSON a caller (and the MCP dispatcher, which forwards these
// bytes) receives has no fingerprint key anywhere in the row metadata.
func TestCodeHandlerSearchEntityContentResponseOmitsFingerprintKeys(t *testing.T) {
	t.Parallel()

	columns := []string{
		"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
		"start_line", "end_line", "language", "source_cache", "metadata",
	}
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{columns: columns, rows: [][]driver.Value{}},
		{columns: columns, rows: [][]driver.Value{{
			"entity-1", "repo-1", "src/app.go", "Function", "handler",
			int64(1), int64(80), "go", "func handler() {}", fingerprintedMetadataJSON(t),
		}}},
	})

	handler := &CodeHandler{Content: NewContentReader(db), Neo4j: graph.FakeGraphReader{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/search",
		bytes.NewBufferString(`{"query":"handler","repo_id":"repo-1","limit":10}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	for _, key := range fingerprint.MetadataKeys() {
		if bytes.Contains(rec.Body.Bytes(), []byte(key)) {
			t.Errorf("response body contains store-internal key %q: %s", key, rec.Body.String())
		}
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("Handles the request.")) {
		t.Fatalf("response body lost the non-fingerprint metadata: %s", rec.Body.String())
	}
}
