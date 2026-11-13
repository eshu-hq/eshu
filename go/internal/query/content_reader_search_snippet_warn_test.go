// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errSnapshotStore fails snapshot open; the embedded ReadStore stays nil
// because the enrichment path under test never reaches cursor or row reads.
type errSnapshotStore struct {
	db.ReadStore
	err error
}

func (store *errSnapshotStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, store.err
}

func TestEnrichFileSnippetsWarnsOnNonTimeoutError(t *testing.T) {
	t.Parallel()

	const probePattern = "S3cr3t-P4tt3rn-PROBE05a"
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	reader := NewContentReaderWithReadStore(&errSnapshotStore{err: errors.New("snapshot refused")}).
		WithUnscopedSearch(0, logger)

	files := []querycontract.FileContent{{
		RepoID:       "repo-1",
		RelativePath: "a.go",
		ContentHash:  "hash",
	}}
	reader.enrichFileSnippetsWithTimeout(context.Background(), probePattern, files, time.Second)

	if files[0].MatchSnippet != "" || files[0].MatchLine != 0 {
		t.Fatalf("error path must omit snippet fields, got %+v", files[0])
	}
	out := logs.String()
	if !strings.Contains(out, "snippet_rows=1") {
		t.Fatalf("warn must carry the row count, got %q", out)
	}
	if strings.Contains(out, probePattern) {
		t.Fatalf("warn must never carry the pattern, got %q", out)
	}
}
