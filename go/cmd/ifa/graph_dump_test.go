// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
)

// TestParseGraphDumpFlagsDefaults proves the zero-flag case: -out empty
// (stdout) and -digest false (full canonical bytes).
func TestParseGraphDumpFlagsDefaults(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	o, err := parseGraphDumpFlags(nil, &stderr)
	if err != nil {
		t.Fatalf("parseGraphDumpFlags(nil): %v", err)
	}
	if o.out != "" {
		t.Errorf("out = %q, want empty (stdout)", o.out)
	}
	if o.digest {
		t.Error("digest = true, want false by default")
	}
}

// TestParseGraphDumpFlagsPlumbsOutAndDigest proves -out and -digest reach the
// returned options unchanged.
func TestParseGraphDumpFlagsPlumbsOutAndDigest(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	o, err := parseGraphDumpFlags([]string{"-out", "testdata/example-dump.json", "-digest"}, &stderr)
	if err != nil {
		t.Fatalf("parseGraphDumpFlags: %v", err)
	}
	if o.out != "testdata/example-dump.json" {
		t.Errorf("out = %q, want the flag value plumbed through", o.out)
	}
	if !o.digest {
		t.Error("digest = false, want true")
	}
}

// TestParseGraphDumpFlagsRejectsUnknownFlag proves an unrecognized flag fails
// clearly, before any backend connection is attempted.
func TestParseGraphDumpFlagsRejectsUnknownFlag(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	_, err := parseGraphDumpFlags([]string{"-bogus-flag"}, &stderr)
	if err == nil {
		t.Fatal("parseGraphDumpFlags(-bogus-flag) = nil error, want a flag-parse error")
	}
}

// TestRunGraphDumpCommandRejectsUnknownFlagWithoutBackend proves
// runGraphDumpCommand fails fast on a flag-parse error without ever
// attempting to open the graph backend — this case is hermetically testable
// in CI with no NornicDB/Neo4j running, mirroring
// TestRunDriveCommandMissingCassetteErrorsCleanlyWithoutPostgres in
// drive_test.go.
func TestRunGraphDumpCommandRejectsUnknownFlagWithoutBackend(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := runGraphDumpCommand(context.Background(), []string{"-bogus-flag"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("runGraphDumpCommand(-bogus-flag) = nil error, want a flag-parse error")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want no output written on a flag error", stdout.String())
	}
}

// TestRunDispatchesGraphDumpSubcommand proves the top-level run dispatcher
// wires "graph-dump" through to runGraphDumpCommand (rather than falling
// through to the -version flag path), using the same hermetic
// flag-parse-error signal as TestRunGraphDumpCommandRejectsUnknownFlagWithoutBackend.
func TestRunDispatchesGraphDumpSubcommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"graph-dump", "-bogus-flag"}, &stdout, &stderr)
	if err == nil {
		t.Fatal(`run([]string{"graph-dump", "-bogus-flag"}) = nil error, want the graph-dump subcommand's flag-parse error`)
	}
	if !strings.Contains(stderr.String(), "graph-dump") {
		t.Errorf("stderr = %q, want it to name the graph-dump flag set in its usage output", stderr.String())
	}
}

// TestWriteGraphDumpToStdout proves writeGraphDump writes to stdout when
// path is empty.
func TestWriteGraphDumpToStdout(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	if err := writeGraphDump("", &stdout, []byte("canonical-bytes\n")); err != nil {
		t.Fatalf("writeGraphDump: %v", err)
	}
	if got := stdout.String(); got != "canonical-bytes\n" {
		t.Errorf("stdout = %q, want %q", got, "canonical-bytes\n")
	}
}

// TestGraphDumpOutputPreservesCanonicalBytesAndDigest pins the non-digest CLI
// output to graphdump's canonical byte contract. Canonicalize already supplies
// its one terminal line feed; the command wrapper must not append another.
func TestGraphDumpOutputPreservesCanonicalBytesAndDigest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		sqlEdge("DEPENDS_ON", "repo-1", "package-1"),
	}}
	canonical, err := graphdump.Canonicalize(ctx, reader)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	output, err := graphDumpOutput(ctx, reader, false)
	if err != nil {
		t.Fatalf("graphDumpOutput: %v", err)
	}
	if !bytes.Equal(output, canonical) {
		t.Fatalf("graphDumpOutput bytes differ from Canonicalize:\noutput=%q\ncanonical=%q", output, canonical)
	}
	if trailing := len(output) - len(bytes.TrimRight(output, "\n")); trailing != 1 {
		t.Fatalf("graphDumpOutput has %d terminal line feeds, want exactly 1", trailing)
	}

	wantDigest, err := graphdump.Digest(ctx, reader)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	sum := sha256.Sum256(output)
	if gotDigest := hex.EncodeToString(sum[:]); gotDigest != wantDigest {
		t.Fatalf("sha256(graphDumpOutput) = %s, want graphdump.Digest %s", gotDigest, wantDigest)
	}
}
