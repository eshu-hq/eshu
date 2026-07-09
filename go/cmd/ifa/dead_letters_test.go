// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
)

// fakeDeadLetterQuerier is a hermetic deadLetterQuerier stand-in so
// renderDeadLetters can be tested without a live Postgres connection,
// mirroring go/cmd/golden-corpus-gate/drains.go's fake drainQuerier pattern.
type fakeDeadLetterQuerier struct {
	records []ifa.DeadLetterRecord
	err     error
}

func (f fakeDeadLetterQuerier) DeadLetters(context.Context) ([]ifa.DeadLetterRecord, error) {
	return f.records, f.err
}

func TestRenderDeadLettersSortsDeterministically(t *testing.T) {
	t.Parallel()

	q := fakeDeadLetterQuerier{records: []ifa.DeadLetterRecord{
		{WorkItemID: "wi-2", Stage: "reducer", Domain: "gcp_resource_materialization", FailureClass: "input_invalid"},
		{WorkItemID: "wi-1", Stage: "reducer", Domain: "gcp_resource_materialization", FailureClass: "input_invalid"},
	}}

	var stdout bytes.Buffer
	if err := renderDeadLetters(context.Background(), q, deadLettersOptions{}, &stdout); err != nil {
		t.Fatalf("renderDeadLetters() error = %v", err)
	}

	var got []ifa.DeadLetterRecord
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal output: %v\noutput: %s", err, stdout.String())
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].WorkItemID != "wi-1" || got[1].WorkItemID != "wi-2" {
		t.Fatalf("got = %+v, want sorted by work_item_id ascending", got)
	}
}

func TestRenderDeadLettersEmptySetRendersEmptyArrayNotNull(t *testing.T) {
	t.Parallel()

	q := fakeDeadLetterQuerier{records: nil}
	var stdout bytes.Buffer
	if err := renderDeadLetters(context.Background(), q, deadLettersOptions{}, &stdout); err != nil {
		t.Fatalf("renderDeadLetters() error = %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "[]" {
		t.Fatalf("output = %q, want the empty-set JSON array %q", got, "[]")
	}
}

func TestRenderDeadLettersPropagatesQueryError(t *testing.T) {
	t.Parallel()

	q := fakeDeadLetterQuerier{err: errors.New("connection refused")}
	var stdout bytes.Buffer
	err := renderDeadLetters(context.Background(), q, deadLettersOptions{}, &stdout)
	if err == nil {
		t.Fatal("renderDeadLetters() = nil error, want the querier's error propagated")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %v, want it to wrap the querier's error", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want no output written on a query error", stdout.String())
	}
}

// TestRunDispatchesDeadLettersSubcommand proves the top-level run dispatcher
// wires "dead-letters" through to runDeadLettersCommand rather than falling
// through to the -version flag path or an "unknown subcommand" error. It
// asserts this at the flag-parse level (an unrecognized flag) so the test
// never attempts to open Postgres — pingWithRetry
// (go/internal/runtime/data_stores.go) always retries for the full
// PingTimeout (10s default) regardless of error kind, which would make a
// real-connection-attempt version of this test slow and not hermetic.
func TestRunDispatchesDeadLettersSubcommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"dead-letters", "-not-a-real-flag"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run([]string{\"dead-letters\", \"-not-a-real-flag\"}) = nil error, want a flag-parse error")
	}
	if strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("error = %v, want the dead-letters subcommand reached (a flag-parse error), not an unknown-subcommand fallthrough", err)
	}
}
