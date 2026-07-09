// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseDriveFlagsRequiresCassette proves a missing -cassette fails
// clearly during flag parsing, before any Postgres connection is attempted.
func TestParseDriveFlagsRequiresCassette(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	_, err := parseDriveFlags(nil, &stderr)
	if err == nil {
		t.Fatal("parseDriveFlags(nil) = nil error, want an error naming -cassette as required")
	}
	if !strings.Contains(err.Error(), "-cassette") {
		t.Errorf("error = %v, want it to name -cassette", err)
	}
}

// TestParseDriveFlagsDefaultsWorkersToOne proves the -workers default
// matches the #4395 acceptance clause's N=1 mode without requiring the
// caller to pass it.
func TestParseDriveFlagsDefaultsWorkersToOne(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	o, err := parseDriveFlags([]string{"-cassette", "testdata/does-not-matter.json"}, &stderr)
	if err != nil {
		t.Fatalf("parseDriveFlags: %v", err)
	}
	if o.workers != driveDefaultWorkers {
		t.Errorf("workers = %d, want default %d", o.workers, driveDefaultWorkers)
	}
	if o.cassette != "testdata/does-not-matter.json" {
		t.Errorf("cassette = %q, want the flag value plumbed through", o.cassette)
	}
}

// TestParseDriveFlagsPlumbsWorkersAndDSN proves -workers and -postgres-dsn
// reach the returned options unchanged.
func TestParseDriveFlagsPlumbsWorkersAndDSN(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	o, err := parseDriveFlags([]string{
		"-cassette", "testdata/example.json",
		"-workers", "8",
		"-postgres-dsn", "postgresql://example/dsn",
	}, &stderr)
	if err != nil {
		t.Fatalf("parseDriveFlags: %v", err)
	}
	if o.workers != 8 {
		t.Errorf("workers = %d, want 8", o.workers)
	}
	if o.postgresDSN != "postgresql://example/dsn" {
		t.Errorf("postgresDSN = %q, want the flag value plumbed through", o.postgresDSN)
	}
}

// TestRunDriveCommandMissingCassetteErrorsCleanlyWithoutPostgres proves
// runDriveCommand fails fast on a missing -cassette flag without ever
// attempting to open Postgres — this case is hermetically testable in CI
// with no database running.
func TestRunDriveCommandMissingCassetteErrorsCleanlyWithoutPostgres(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := runDriveCommand(context.Background(), nil, &stdout, &stderr)
	if err == nil {
		t.Fatal("runDriveCommand(nil) = nil error, want an error naming -cassette as required")
	}
	if !strings.Contains(err.Error(), "-cassette") {
		t.Errorf("error = %v, want it to name -cassette", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want no report printed on a flag error", stdout.String())
	}
}

// TestRunDriveCommandNonexistentCassetteErrorsCleanlyWithoutPostgres proves
// runDriveCommand fails fast when the cassette path does not exist, before
// opening Postgres: the cassette load happens first, so this path is also
// hermetic.
func TestRunDriveCommandNonexistentCassetteErrorsCleanlyWithoutPostgres(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	var stdout, stderr bytes.Buffer
	err := runDriveCommand(context.Background(), []string{"-cassette", missing}, &stdout, &stderr)
	if err == nil {
		t.Fatal("runDriveCommand(missing cassette) = nil error, want a load-cassette error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %v, want it to name the missing cassette path %q", err, missing)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want no report printed on a load error", stdout.String())
	}
}

// TestRunDispatchesDriveSubcommand proves the top-level run dispatcher wires
// "drive" through to runDriveCommand (rather than falling through to the
// -version flag path), using the same missing-cassette hermetic error as
// the signal.
func TestRunDispatchesDriveSubcommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"drive"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run([]string{\"drive\"}) = nil error, want the drive subcommand's -cassette-required error")
	}
	if !strings.Contains(err.Error(), "-cassette") {
		t.Errorf("error = %v, want the drive subcommand's -cassette-required error", err)
	}
}
