// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// contentSearchMirrorStores builds the two satisfiers under comparison
// over one shared canned-row store: the production *ContentReader (the
// want side) and the admission constructor (the got side). Both calls
// consume queued rows in order, so queue one row per call.
func contentSearchMirrorStores(t *testing.T, queued []contentReaderQueryResult) (*ContentReader, ContentSearchReadAdmission) {
	t.Helper()
	store := postgres.NewSQLReadStore(openContentReaderTestDB(t, queued))
	if store == nil {
		t.Fatal("NewSQLReadStore() = nil, want a guarded ReadStore over the canned rows")
	}
	return NewContentReaderWithReadStore(store), NewContentSearchReadAdmissionWithReadStore(store, 0, nil, nil)
}

var contentSearchFileRowColumns = []string{
	"repo_id", "relative_path", "commit_sha", "content",
	"content_hash", "line_count", "language", "artifact_type",
	"match_snippet", "match_line",
}

var contentSearchEntityRowColumns = []string{
	"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
	"start_line", "end_line", "language", "source_cache", "metadata",
}

func contentSearchFileRow(content string) contentReaderQueryResult {
	return contentReaderQueryResult{
		columns: contentSearchFileRowColumns,
		rows: [][]driver.Value{
			{"repo-1", "src/app.go", "abc123", content, "h-1", int64(2), "go", "", "l1", int64(1)},
		},
	}
}

// contentSearchLegacyFileRowColumns is the 8-column shape of the legacy
// unpaged SearchFileContent/SearchFileContentAnyRepo SELECTs, which keep no
// snippet fields by design (#10607: the paged search paths alone ship
// snippets; the legacy fallback stays a handle read).
var contentSearchLegacyFileRowColumns = []string{
	"repo_id", "relative_path", "commit_sha", "content",
	"content_hash", "line_count", "language", "artifact_type",
}

func contentSearchLegacyFileRow(content string) contentReaderQueryResult {
	return contentReaderQueryResult{
		columns: contentSearchLegacyFileRowColumns,
		rows: [][]driver.Value{
			{"repo-1", "src/app.go", "abc123", content, "h-1", int64(2), "go", ""},
		},
	}
}

func contentSearchEntityRow() contentReaderQueryResult {
	return contentReaderQueryResult{
		columns: contentSearchEntityRowColumns,
		rows: [][]driver.Value{
			{
				"entity-1", "repo-1", "src/app.ts", "Function", "renderApp",
				int64(10), int64(24), "typescript", "function renderApp() {}",
				[]byte(`{"docstring":"Renders the app."}`),
			},
		},
	}
}

var contentSearchDockerRowColumns = []string{
	"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
	"start_line", "end_line", "metadata",
}

func contentSearchDockerRow() contentReaderQueryResult {
	return contentReaderQueryResult{
		columns: contentSearchDockerRowColumns,
		rows: [][]driver.Value{
			{
				"entity-9", "repo-1", "Dockerfile", "DockerfileInstall", "curl",
				int64(3), int64(3),
				[]byte(`{"manager":"apt","category":"package","stage":"build"}`),
			},
		},
	}
}

// TestContentSearchReadAdmissionWireShapesAreSatelliteConsumable proves
// the wire shapes behind the #9669 W4 pin: the production Postgres
// content reader satisfies the admission, and the WithReadStore
// constructor serves all ten re-declared search reads over nothing
// wider than the guarded ReadStore the satellite holds. Every read
// fails closed (never panics, never returns confident emptiness) when
// the guarded reader is unavailable, and every canned-row read answers
// byte-identical wire objects to *ContentReader over the same store —
// the full wire-object byte mirror — with want and got unified through
// one projection.
func TestContentSearchReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	var _ ContentSearchReadAdmission = NewContentReaderWithReadStore(errContentPointWireShapeReadStore{err: errContentPointWireShapeProbe})

	admission := NewContentSearchReadAdmissionWithReadStore(
		errContentPointWireShapeReadStore{err: errContentPointWireShapeProbe}, 0, nil, nil,
	)
	if admission == nil {
		t.Fatal("NewContentSearchReadAdmissionWithReadStore() = nil, want a ContentSearchReadAdmission bound to the guarded reader")
	}

	ctx := context.Background()
	failClosed := []struct {
		name string
		call func(ContentSearchReadAdmission) error
	}{
		{"SearchFiles", func(a ContentSearchReadAdmission) error {
			_, err := a.SearchFiles(ctx, "repo-1", nil, "app", 10, 0)
			return err
		}},
		{"SearchEntities", func(a ContentSearchReadAdmission) error {
			_, err := a.SearchEntities(ctx, "repo-1", nil, "app", 10, 0)
			return err
		}},
		{"SearchFileContent", func(a ContentSearchReadAdmission) error {
			_, err := a.SearchFileContent(ctx, "repo-1", "app", 10)
			return err
		}},
		{"SearchFileContentAnyRepo", func(a ContentSearchReadAdmission) error {
			_, err := a.SearchFileContentAnyRepo(ctx, "app", 10)
			return err
		}},
		{"SearchEntityContent", func(a ContentSearchReadAdmission) error {
			_, err := a.SearchEntityContent(ctx, "repo-1", "app", 10)
			return err
		}},
		{"SearchEntityContentAnyRepo", func(a ContentSearchReadAdmission) error {
			_, err := a.SearchEntityContentAnyRepo(ctx, "app", 10)
			return err
		}},
		{"SearchFilesUnscoped", func(a ContentSearchReadAdmission) error {
			_, err := a.SearchFilesUnscoped(ctx, "app", 10, 0, "", "", 0)
			return err
		}},
		{"SearchEntitiesUnscoped", func(a ContentSearchReadAdmission) error {
			_, err := a.SearchEntitiesUnscoped(ctx, "app", 10, 0, querycontract.SearchCursor{})
			return err
		}},
		{"SearchDockerfileInstallEntities", func(a ContentSearchReadAdmission) error {
			_, err := a.SearchDockerfileInstallEntities(ctx, "curl", nil, "", "", 0, "", 10)
			return err
		}},
		{"SearchPHPConfigEntities", func(a ContentSearchReadAdmission) error {
			_, err := a.SearchPHPConfigEntities(ctx, nil, "php_directive:display_errors", nil, 10)
			return err
		}},
	}
	for _, tc := range failClosed {
		tc := tc
		t.Run("fail-closed/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.call(admission); err == nil {
				t.Errorf("%s over an unavailable reader = nil error, want a fail-closed error", tc.name)
			}
		})
	}

	t.Run("mirror/search-files-paged", func(t *testing.T) {
		t.Parallel()
		row := contentSearchFileRow("l1\nl2")
		wantReader, got := contentSearchMirrorStores(t, []contentReaderQueryResult{row, row})

		want, wantErr := wantReader.SearchFiles(ctx, "repo-1", nil, "l1", 10, 0)
		gotFiles, gotErr := got.SearchFiles(ctx, "repo-1", nil, "l1", 10, 0)
		if wantErr != nil || gotErr != nil {
			t.Fatalf("errors = (%v, %v), want nil on both sides", wantErr, gotErr)
		}
		if len(want) != 1 || want[0].Content != "l1\nl2" {
			t.Fatalf("want side = %+v, want the canned file row", want)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotFiles); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/search-files-empty", func(t *testing.T) {
		t.Parallel()
		empty := contentReaderQueryResult{columns: contentSearchFileRowColumns}
		wantReader, got := contentSearchMirrorStores(t, []contentReaderQueryResult{empty, empty})

		want, wantErr := wantReader.SearchFiles(ctx, "repo-1", nil, "zzz-no-match", 10, 0)
		gotFiles, gotErr := got.SearchFiles(ctx, "repo-1", nil, "zzz-no-match", 10, 0)
		if wantErr != nil || gotErr != nil {
			t.Fatalf("errors = (%v, %v), want nil on both sides", wantErr, gotErr)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotFiles); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/search-entities-paged", func(t *testing.T) {
		t.Parallel()
		row := contentSearchEntityRow()
		wantReader, got := contentSearchMirrorStores(t, []contentReaderQueryResult{row, row})

		want, wantErr := wantReader.SearchEntities(ctx, "repo-1", nil, "render", 10, 0)
		gotEntities, gotErr := got.SearchEntities(ctx, "repo-1", nil, "render", 10, 0)
		if wantErr != nil || gotErr != nil {
			t.Fatalf("errors = (%v, %v), want nil on both sides", wantErr, gotErr)
		}
		if len(want) != 1 || want[0].Metadata["docstring"] != "Renders the app." {
			t.Fatalf("want side metadata = %+v, want the decoded canned metadata", want)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotEntities); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/search-file-content", func(t *testing.T) {
		t.Parallel()
		row := contentSearchLegacyFileRow("package app")
		wantReader, got := contentSearchMirrorStores(t, []contentReaderQueryResult{row, row})

		want, wantErr := wantReader.SearchFileContent(ctx, "repo-1", "package", 10)
		gotFiles, gotErr := got.SearchFileContent(ctx, "repo-1", "package", 10)
		if wantErr != nil || gotErr != nil {
			t.Fatalf("errors = (%v, %v), want nil on both sides", wantErr, gotErr)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotFiles); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/search-file-content-any-repo", func(t *testing.T) {
		t.Parallel()
		row := contentSearchLegacyFileRow("package app")
		wantReader, got := contentSearchMirrorStores(t, []contentReaderQueryResult{row, row})

		want, wantErr := wantReader.SearchFileContentAnyRepo(ctx, "package", 10)
		gotFiles, gotErr := got.SearchFileContentAnyRepo(ctx, "package", 10)
		if wantErr != nil || gotErr != nil {
			t.Fatalf("errors = (%v, %v), want nil on both sides", wantErr, gotErr)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotFiles); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/search-entity-content", func(t *testing.T) {
		t.Parallel()
		row := contentSearchEntityRow()
		wantReader, got := contentSearchMirrorStores(t, []contentReaderQueryResult{row, row})

		want, wantErr := wantReader.SearchEntityContent(ctx, "repo-1", "render", 10)
		gotEntities, gotErr := got.SearchEntityContent(ctx, "repo-1", "render", 10)
		if wantErr != nil || gotErr != nil {
			t.Fatalf("errors = (%v, %v), want nil on both sides", wantErr, gotErr)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotEntities); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/search-entity-content-any-repo", func(t *testing.T) {
		t.Parallel()
		row := contentSearchEntityRow()
		wantReader, got := contentSearchMirrorStores(t, []contentReaderQueryResult{row, row})

		want, wantErr := wantReader.SearchEntityContentAnyRepo(ctx, "render", 10)
		gotEntities, gotErr := got.SearchEntityContentAnyRepo(ctx, "render", 10)
		if wantErr != nil || gotErr != nil {
			t.Fatalf("errors = (%v, %v), want nil on both sides", wantErr, gotErr)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotEntities); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/search-dockerfile-install-entities", func(t *testing.T) {
		t.Parallel()
		row := contentSearchDockerRow()
		wantReader, got := contentSearchMirrorStores(t, []contentReaderQueryResult{row, row})

		want, wantErr := wantReader.SearchDockerfileInstallEntities(ctx, "curl", nil, "", "", 0, "", 10)
		gotEntities, gotErr := got.SearchDockerfileInstallEntities(ctx, "curl", nil, "", "", 0, "", 10)
		if wantErr != nil || gotErr != nil {
			t.Fatalf("errors = (%v, %v), want nil on both sides", wantErr, gotErr)
		}
		if len(want) != 1 || want[0].Metadata["manager"] != "apt" {
			t.Fatalf("want side = %+v, want the canned install row", want)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotEntities); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/search-php-config-entities", func(t *testing.T) {
		t.Parallel()
		row := contentSearchEntityRow()
		wantReader, got := contentSearchMirrorStores(t, []contentReaderQueryResult{row, row})

		want, wantErr := wantReader.SearchPHPConfigEntities(ctx, nil, "php_directive:display_errors", nil, 10)
		gotEntities, gotErr := got.SearchPHPConfigEntities(ctx, nil, "php_directive:display_errors", nil, 10)
		if wantErr != nil || gotErr != nil {
			t.Fatalf("errors = (%v, %v), want nil on both sides", wantErr, gotErr)
		}
		if wantBytes, gotBytes := marshalContentPointWire(t, want), marshalContentPointWire(t, gotEntities); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/unscoped-budget-validation-parity", func(t *testing.T) {
		t.Parallel()
		row := contentSearchFileRow("l1\nl2")
		wantReader, got := contentSearchMirrorStores(t, []contentReaderQueryResult{row, row})

		_, wantErr := wantReader.SearchFilesUnscoped(ctx, "l1", 10, 0, "", "", 50)
		_, gotErr := got.SearchFilesUnscoped(ctx, "l1", 10, 0, "", "", 50)
		if wantErr == nil || gotErr == nil {
			t.Fatalf("errors = (%v, %v), want a budget validation error on both sides", wantErr, gotErr)
		}
		if wantErr.Error() != gotErr.Error() {
			t.Errorf("validation errors differ:\n want %q\n got  %q", wantErr.Error(), gotErr.Error())
		}
	})
}

// TestContentSearchReadAdmissionConstructorReproducesProductionBudget
// proves the constructor half of the W4 transport: the admission holds
// the production *ContentReader wiring (the dynamic type is the
// production reader, so no parallel construction path exists) and the
// unscoped budget, logger, and instruments the satellite passes reach
// the bounded walk unchanged. A constructor that delegates to
// NewContentReaderWithReadStore without applying WithUnscopedSearch
// leaves the zero budget here and fails, and one that skips
// WithInstruments leaves nil instruments here and fails.
func TestContentSearchReadAdmissionConstructorReproducesProductionBudget(t *testing.T) {
	t.Parallel()

	store := postgres.NewSQLReadStore(openContentReaderTestDB(t, nil))
	if store == nil {
		t.Fatal("NewSQLReadStore() = nil, want a guarded ReadStore")
	}
	budget := 750 * time.Millisecond
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	instruments := &telemetry.Instruments{}
	admission := NewContentSearchReadAdmissionWithReadStore(store, budget, logger, instruments)

	cr, ok := admission.(*ContentReader)
	if !ok {
		t.Fatalf("constructor dynamic type = %T, want *ContentReader: the admission must delegate to NewContentReaderWithReadStore", admission)
	}
	if cr.unscoped.budget != budget {
		t.Errorf("unscoped budget = %v, want %v: the constructor must apply WithUnscopedSearch with the production budget", cr.unscoped.budget, budget)
	}
	if cr.unscoped.logger != logger {
		t.Error("unscoped logger was not applied: the constructor must apply WithUnscopedSearch with the production logger")
	}
	if cr.instruments != instruments {
		t.Error("instruments were not applied: the constructor must apply WithInstruments the way cmd/api wiring does, or the satellite path loses operator metrics")
	}
}
