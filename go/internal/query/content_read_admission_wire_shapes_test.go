// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errContentPointWireShapeProbe is the sentinel the failing guarded
// reader below answers every read with. A non-nil error (never a
// panic, never a confident empty result) is the fail-closed proof the
// satellite-consumable constructor wires the guarded port through.
var errContentPointWireShapeProbe = errors.New("content point wire-shape probe: reader unavailable")

// errContentPointWireShapeRow is a db.Row that always fails its scan.
type errContentPointWireShapeRow struct{ err error }

func (r errContentPointWireShapeRow) Scan(...any) error { return r.err }

// errContentPointWireShapeReadStore is a guarded reader that answers
// every read with the probe error. It implements the full db.ReadStore
// surface (cursor, row, and snapshot reads), so constructing the
// admission satisfier over it proves the satellite path needs nothing
// wider than the request-scoped ReadStore handle.
type errContentPointWireShapeReadStore struct{ err error }

func (s errContentPointWireShapeReadStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, s.err
}

func (s errContentPointWireShapeReadStore) QueryRowContext(_ context.Context, _ string, _ ...any) db.Row {
	return errContentPointWireShapeRow(s)
}

func (s errContentPointWireShapeReadStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, s.err
}

var _ db.ReadStore = errContentPointWireShapeReadStore{}

// marshalContentPointWire projects one wire object through the same
// encoding the handlers use (no HTML escaping), so the mirror compares
// want and got through one projection instead of hand-written
// expectations that could drift from the wire.
func marshalContentPointWire(t *testing.T, v any) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal wire object: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// contentPointMirrorStores builds the two satisfiers under comparison
// over one shared canned-row store: the production *ContentReader (the
// want side) and the admission constructor (the got side). Both calls
// consume queued rows in order, so queue one row per call.
func contentPointMirrorStores(t *testing.T, queued []contentReaderQueryResult) (*ContentReader, ContentPointReadAdmission) {
	t.Helper()
	store := postgres.NewSQLReadStore(openContentReaderTestDB(t, queued))
	if store == nil {
		t.Fatal("NewSQLReadStore() = nil, want a guarded ReadStore over the canned rows")
	}
	return NewContentReaderWithReadStore(store), NewContentPointReadAdmissionWithReadStore(store)
}

var contentPointFileRowColumns = []string{
	"repo_id", "relative_path", "commit_sha", "content",
	"content_hash", "line_count", "language", "artifact_type",
}

var contentPointEntityRowColumns = []string{
	"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
	"start_line", "end_line", "language", "source_cache", "metadata",
}

func contentPointFileRow(content string) contentReaderQueryResult {
	return contentReaderQueryResult{
		columns: contentPointFileRowColumns,
		rows: [][]driver.Value{
			{"repo-1", "src/app.go", "abc123", content, "h-1", int64(5), "go", ""},
		},
	}
}

// TestContentPointReadAdmissionWireShapesAreSatelliteConsumable proves
// the wire shapes behind the #9669 D13 Wave-2 pin: the production
// Postgres content reader satisfies the admission, and the
// WithReadStore constructor serves all three re-declared reads over
// nothing wider than the guarded ReadStore the satellite holds. Every
// read fails closed (never panics, never returns confident emptiness)
// when the guarded reader is unavailable, and every read answers
// byte-identical wire objects to *ContentReader over the same store —
// the full wire-object mirror, including GetFileLines slicing edge
// semantics, with want and got unified through one projection.
func TestContentPointReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	var _ ContentPointReadAdmission = NewContentReaderWithReadStore(errContentPointWireShapeReadStore{err: errContentPointWireShapeProbe})

	admission := NewContentPointReadAdmissionWithReadStore(errContentPointWireShapeReadStore{err: errContentPointWireShapeProbe})
	if admission == nil {
		t.Fatal("NewContentPointReadAdmissionWithReadStore() = nil, want a ContentPointReadAdmission bound to the guarded reader")
	}

	ctx := context.Background()
	if _, err := admission.GetFileContent(ctx, "repo-1", "src/app.go"); err == nil {
		t.Error("GetFileContent over an unavailable reader = nil error, want a fail-closed error")
	}
	if _, err := admission.GetFileLines(ctx, "repo-1", "src/app.go", 1, 2); err == nil {
		t.Error("GetFileLines over an unavailable reader = nil error, want a fail-closed error")
	}
	if _, err := admission.GetEntityContent(ctx, "entity-1"); err == nil {
		t.Error("GetEntityContent over an unavailable reader = nil error, want a fail-closed error")
	}

	t.Run("mirror/file-read", func(t *testing.T) {
		t.Parallel()
		row := contentPointFileRow("l1\nl2\nl3\nl4\nl5")
		wantReader, got := contentPointMirrorStores(t, []contentReaderQueryResult{row, row})

		want, wantErr := wantReader.GetFileContent(ctx, "repo-1", "src/app.go")
		gotFile, gotErr := got.GetFileContent(ctx, "repo-1", "src/app.go")
		if (wantErr == nil) != (gotErr == nil) {
			t.Fatalf("error mismatch: want %v, got %v", wantErr, gotErr)
		}
		if wantErr != nil {
			t.Fatalf("GetFileContent() error = %v, want nil over canned rows", wantErr)
		}
		if want.Content != "l1\nl2\nl3\nl4\nl5" || want.LineCount != 5 || want.CommitSHA != "abc123" {
			t.Fatalf("want side = %+v, want the full canned file row", want)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotFile); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/file-missing", func(t *testing.T) {
		t.Parallel()
		missing := contentReaderQueryResult{columns: contentPointFileRowColumns}
		wantReader, got := contentPointMirrorStores(t, []contentReaderQueryResult{missing, missing})

		want, wantErr := wantReader.GetFileContent(ctx, "repo-1", "src/missing.go")
		gotFile, gotErr := got.GetFileContent(ctx, "repo-1", "src/missing.go")
		if wantErr != nil || gotErr != nil || want != nil || gotFile != nil {
			t.Fatalf("missing file = (%v, %v) vs (%v, %v), want (nil, nil) on both sides", want, wantErr, gotFile, gotErr)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotFile); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	for _, tc := range []struct {
		name               string
		startLine, endLine int
		wantContent        string
		wantLineCount      int
	}{
		{"full-range", 1, 5, "l1\nl2\nl3\nl4\nl5", 5},
		{"sub-range", 2, 4, "l2\nl3\nl4", 3},
		{"start-clamps-to-one", 0, 2, "l1\nl2", 2},
		{"end-zero-reads-to-end", 2, 0, "l2\nl3\nl4\nl5", 4},
		{"end-clamps-to-length", 4, 99, "l4\nl5", 2},
		{"start-past-end-reads-empty", 9, 99, "", 0},
	} {
		tc := tc
		t.Run("mirror/file-lines/"+tc.name, func(t *testing.T) {
			t.Parallel()
			row := contentPointFileRow("l1\nl2\nl3\nl4\nl5")
			wantReader, got := contentPointMirrorStores(t, []contentReaderQueryResult{row, row})

			want, wantErr := wantReader.GetFileLines(ctx, "repo-1", "src/app.go", tc.startLine, tc.endLine)
			gotFile, gotErr := got.GetFileLines(ctx, "repo-1", "src/app.go", tc.startLine, tc.endLine)
			if wantErr != nil || gotErr != nil {
				t.Fatalf("errors = (%v, %v), want nil on both sides", wantErr, gotErr)
			}
			if want.Content != tc.wantContent || want.LineCount != tc.wantLineCount {
				t.Fatalf("want side = %+v, want content %q line count %d", want, tc.wantContent, tc.wantLineCount)
			}
			if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotFile); wantBytes != gotBytes {
				t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
			}
		})
	}

	t.Run("mirror/entity-read", func(t *testing.T) {
		t.Parallel()
		row := contentReaderQueryResult{
			columns: contentPointEntityRowColumns,
			rows: [][]driver.Value{
				{
					"entity-1", "repo-1", "src/app.ts", "Function", "renderApp",
					int64(10), int64(24), "typescript", "function renderApp() {}",
					[]byte(`{"docstring":"Renders the app.","async":true}`),
				},
			},
		}
		wantReader, got := contentPointMirrorStores(t, []contentReaderQueryResult{row, row})

		want, wantErr := wantReader.GetEntityContent(ctx, "entity-1")
		gotEntity, gotErr := got.GetEntityContent(ctx, "entity-1")
		if wantErr != nil || gotErr != nil {
			t.Fatalf("errors = (%v, %v), want nil on both sides", wantErr, gotErr)
		}
		if want.Metadata["docstring"] != "Renders the app." || want.Metadata["async"] != true {
			t.Fatalf("want side metadata = %#v, want the decoded canned metadata", want.Metadata)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotEntity); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/entity-missing", func(t *testing.T) {
		t.Parallel()
		missing := contentReaderQueryResult{columns: contentPointEntityRowColumns}
		wantReader, got := contentPointMirrorStores(t, []contentReaderQueryResult{missing, missing})

		want, wantErr := wantReader.GetEntityContent(ctx, "entity-missing")
		gotEntity, gotErr := got.GetEntityContent(ctx, "entity-missing")
		if wantErr != nil || gotErr != nil || want != nil || gotEntity != nil {
			t.Fatalf("missing entity = (%v, %v) vs (%v, %v), want (nil, nil) on both sides", want, wantErr, gotEntity, gotErr)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotEntity); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})
}
