package googleworkspace

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type fakeClient struct {
	files        []File
	exports      map[string]Export
	exportErrors map[string]error
	permissions  map[string]permissionResult
	listRequests []Allowlist
	exportMIMEs  []string
	listErr      error
}

type permissionResult struct {
	summary PermissionSummary
	err     error
}

func newFakeClient(files []File) *fakeClient {
	return &fakeClient{
		files:        files,
		exports:      map[string]Export{},
		exportErrors: map[string]error{},
		permissions:  map[string]permissionResult{},
	}
}

func (f *fakeClient) ListFiles(_ context.Context, allowlist Allowlist) ([]File, error) {
	f.listRequests = append(f.listRequests, allowlist)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]File(nil), f.files...), nil
}

func (f *fakeClient) PermissionSummary(_ context.Context, fileID string) (PermissionSummary, error) {
	if result, ok := f.permissions[fileID]; ok {
		return result.summary, result.err
	}
	return PermissionSummary{Visibility: "restricted"}, nil
}

func (f *fakeClient) Export(_ context.Context, fileID string, mimeType string) (Export, error) {
	f.exportMIMEs = append(f.exportMIMEs, mimeType)
	if err := f.exportErrors[fileID]; err != nil {
		return Export{}, err
	}
	if export, ok := f.exports[fileID]; ok {
		return export, nil
	}
	return Export{Bytes: []byte("export-bytes")}, nil
}

func testFile(id string, kind FileKind) File {
	return File{
		ID:         id,
		Kind:       kind,
		Name:       "Synthetic Workspace File",
		RevisionID: "rev-" + id,
		WebURL:     "https://workspace.example.invalid/file/" + id,
		ModifiedAt: time.Date(2026, time.June, 9, 13, 0, 0, 0, time.UTC),
	}
}

func assertFactKindCount(t *testing.T, envelopes []facts.Envelope, kind string, want int) {
	t.Helper()

	if got := countKind(envelopes, kind); got != want {
		t.Fatalf("fact kind %q count = %d, want %d", kind, got, want)
	}
}

func countKind(envelopes []facts.Envelope, kind string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			count++
		}
	}
	return count
}

func countSectionsForDocument(envelopes []facts.Envelope, documentID string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.DocumentationSectionFactKind && envelope.Payload["document_id"] == documentID {
			count++
		}
	}
	return count
}

func payloadByKind(t *testing.T, envelopes []facts.Envelope, kind string) map[string]any {
	t.Helper()

	var matches []map[string]any
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			matches = append(matches, envelope.Payload)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("fact kind %q count = %d, want 1", kind, len(matches))
	}
	return matches[0]
}

func payloadByKindAndID(t *testing.T, envelopes []facts.Envelope, kind string, key string, value string) map[string]any {
	t.Helper()

	for _, envelope := range envelopes {
		if envelope.FactKind == kind && envelope.Payload[key] == value {
			return envelope.Payload
		}
	}
	t.Fatalf("missing fact kind %q with %s=%q in %#v", kind, key, value, envelopes)
	return nil
}

func stringMapValue(t *testing.T, row map[string]any, key string) map[string]string {
	t.Helper()

	raw := mapValue(t, row, key)
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		value, ok := v.(string)
		if !ok {
			t.Fatalf("row[%q][%q] type = %T, want string", key, k, v)
		}
		out[k] = value
	}
	return out
}

func mapValue(t *testing.T, row map[string]any, key string) map[string]any {
	t.Helper()

	raw, ok := row[key]
	if !ok {
		t.Fatalf("row missing %q: %#v", key, row)
	}
	values, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("row[%q] type = %T, want map[string]any", key, raw)
	}
	return values
}

func stringSliceValue(t *testing.T, row map[string]any, key string) []string {
	t.Helper()

	raw, ok := row[key]
	if !ok {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				t.Fatalf("row[%q] element type = %T, want string", key, value)
			}
			out = append(out, text)
		}
		return out
	default:
		t.Fatalf("row[%q] type = %T, want []string", key, raw)
		return nil
	}
}

func equalStrings(got []string, want []string) bool {
	if got == nil {
		got = []string{}
	}
	if want == nil {
		want = []string{}
	}
	return reflect.DeepEqual(got, want)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
