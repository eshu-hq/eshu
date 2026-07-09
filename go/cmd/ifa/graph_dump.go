// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
)

// graphDumpOptions holds the parsed command-line inputs for one
// "ifa graph-dump" run.
type graphDumpOptions struct {
	out    string
	digest bool
}

func parseGraphDumpFlags(args []string, stderr io.Writer) (graphDumpOptions, error) {
	fs := flag.NewFlagSet("ifa graph-dump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o graphDumpOptions
	fs.StringVar(&o.out, "out", "", "path to write the canonical graph dump (default: stdout)")
	fs.BoolVar(&o.digest, "digest", false, "print the sha256 digest of the canonical graph instead of its full canonical bytes")
	if err := fs.Parse(args); err != nil {
		return graphDumpOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	return o, nil
}

// runGraphDumpCommand implements `ifa graph-dump`: the graph-truth half of
// Ifá's P3 determinism matrix (issue #4396, design doc
// docs/internal/design/4389-ifa-conformance-platform.md, Layer 2). It opens a
// live Bolt connection to the configured graph backend
// (ESHU_GRAPH_BACKEND/NEO4J_URI/NEO4J_USERNAME/NEO4J_PASSWORD/NEO4J_DATABASE,
// the same env contract every other Bolt-backed Eshu binary honours via
// runtime.OpenNeo4jDriver — see graphdump_reader.go), reads every node and
// relationship through graphdump.Reader, and writes
// graphdump.Canonicalize's stable byte form (or, with -digest, its sha256
// hex digest) to -out or stdout.
//
// The flags are parsed before the backend is opened: a bad flag fails fast
// without requiring a database connection, so a hermetic caller can exercise
// that path without Docker or a graph backend running (mirrors
// runDriveCommand's cassette-before-Postgres ordering in drive.go).
//
// This is a read-only diagnostic verb: it applies no schema DDL and performs
// no write. A follow-on slice's determinism matrix calls it at each
// worker-count replay to prove the resulting graph is byte-identical; run
// standalone it is also a way for an operator to inspect what a live graph
// canonicalizes to.
func runGraphDumpCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	o, err := parseGraphDumpFlags(args, stderr)
	if err != nil {
		return err
	}

	reader, closeFn, err := openBoltGraphReader(ctx, os.Getenv)
	if err != nil {
		return fmt.Errorf("ifa graph-dump: open graph backend: %w", err)
	}
	defer closeFn()

	data, err := graphDumpOutput(ctx, reader, o.digest)
	if err != nil {
		return fmt.Errorf("ifa graph-dump: %w", err)
	}
	return writeGraphDump(o.out, stdout, data)
}

// graphDumpOutput returns the bytes runGraphDumpCommand writes out: the
// canonical graph document, or (with digest=true) its sha256 hex digest
// followed by a trailing newline so either form is a well-formed line-based
// CLI output.
func graphDumpOutput(ctx context.Context, reader graphdump.Reader, digest bool) ([]byte, error) {
	if digest {
		d, err := graphdump.Digest(ctx, reader)
		if err != nil {
			return nil, fmt.Errorf("digest graph: %w", err)
		}
		return []byte(d + "\n"), nil
	}
	bs, err := graphdump.Canonicalize(ctx, reader)
	if err != nil {
		return nil, fmt.Errorf("canonicalize graph: %w", err)
	}
	return append(bs, '\n'), nil
}

// writeGraphDump writes data to path, or to stdout when path is empty.
func writeGraphDump(path string, stdout io.Writer, data []byte) error {
	if path == "" {
		if _, err := stdout.Write(data); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(path, data, 0o600); err != nil { // #nosec G703 -- -out is an explicit operator-controlled CLI output path for this local tool.
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
