// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
)

// seedSinkRecords appends two read records for one backend and returns them,
// so the round-trip test can compare what the sink wrote with what the
// loader reads back.
func seedSinkRecords(t *testing.T, sink *Sink, backend string) []backendconformance.DifferentialRecord {
	t.Helper()
	records := []backendconformance.DifferentialRecord{
		{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Repository) RETURN n", Parameters: "null"},
			Backend:     backend,
			RowCount:    2,
			Digest:      "aaa",
		},
		{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Repository) RETURN n", Parameters: "null"},
			Backend:     backend,
			RowCount:    2,
			Digest:      "aaa",
		},
	}
	for _, record := range records {
		if err := sink.Append(record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	return records
}

// TestSinkRoundTrip is the persistence contract: records appended through a
// session survive the process boundary — the loader reads back exactly what
// the sink wrote, grouped by backend.
func TestSinkRoundTrip(t *testing.T) {
	dir := t.TempDir()
	nornic, err := OpenDir(dir, "nornicdb", "query-test")
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	wantNornic := seedSinkRecords(t, nornic, "nornicdb")
	if err := nornic.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	neo, err := OpenDir(dir, "neo4j", "query-test")
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	wantNeo := seedSinkRecords(t, neo, "neo4j")
	wantNeo[0].Digest = "bbb"
	wantNeo[1].Digest = "bbb"
	if err := neo.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	byBackend, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(byBackend["nornicdb"]) != len(wantNornic) {
		t.Fatalf("LoadDir() nornicdb records = %d, want %d", len(byBackend["nornicdb"]), len(wantNornic))
	}
	for i, got := range byBackend["nornicdb"] {
		if got != wantNornic[i] {
			t.Fatalf("LoadDir() nornicdb record %d = %+v, want %+v", i, got, wantNornic[i])
		}
	}
	if len(byBackend["neo4j"]) != len(wantNeo) {
		t.Fatalf("LoadDir() neo4j records = %d, want %d", len(byBackend["neo4j"]), len(wantNeo))
	}
}

// TestSinkRecordDurableWithoutClose pins the kill-safety contract: B-7
// SIGTERMs the replay binaries, so an appended record must reach the file
// without waiting for Close. A kill mid-run loses at most the in-flight
// statement, never the whole recording.
func TestSinkRecordDurableWithoutClose(t *testing.T) {
	dir := t.TempDir()
	sink, err := OpenDir(dir, "nornicdb", "drain-test")
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	seedSinkRecords(t, sink, "nornicdb")
	byBackend, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(byBackend["nornicdb"]) != 2 {
		t.Fatalf("LoadDir() nornicdb records = %d, want 2 without Close", len(byBackend["nornicdb"]))
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestSinkCloseWithoutAppendWritesNoFile pins the quiet-process contract: a
// binary that executed no graph statements leaves no recording file, so the
// loader never mistakes an empty run for a backend with zero statements.
func TestSinkCloseWithoutAppendWritesNoFile(t *testing.T) {
	dir := t.TempDir()
	sink, err := OpenDir(dir, "nornicdb", "query-test")
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("Glob() files = %v, want none", matches)
	}
}

// TestLoadDirRejectsUnknownBackend keeps a mislabeled recording from
// silently joining the wrong side of the diff: a backend label outside the
// known pair fails the load.
func TestLoadDirRejectsUnknownBackend(t *testing.T) {
	dir := t.TempDir()
	// A mislabeled file can only arrive by hand or by a writer older than
	// the recording-side check, so the test writes it directly.
	raw := `{"phase":"","record":{"Fingerprint":{"Statement":"MATCH (n:R) RETURN n","Parameters":"null"},"Backend":"cassandra","RowCount":1,"Digest":"x","Failed":false}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rogue-1.jsonl"), []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("LoadDir() error = nil, want unknown-backend rejection")
	}
}
