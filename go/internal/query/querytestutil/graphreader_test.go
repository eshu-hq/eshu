// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil_test

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestFakeGraphReaderRoutesIncomingQueries pins the dispatch rule that makes
// this fake usable at all: a handler issuing both an outgoing and an incoming
// traversal gets two different row sets from one fake, chosen by the query
// text. Collapse the two and a test that meant to assert on incoming edges
// silently asserts on outgoing ones.
func TestFakeGraphReaderRoutesIncomingQueries(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"which": "outgoing"}}, nil
		},
		RunIncomingFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"which": "incoming"}}, nil
		},
	}

	got, err := reader.Run(context.Background(), "MATCH (n) RETURN n", nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(got) != 1 || got[0]["which"] != "outgoing" {
		t.Fatalf("Run() = %v, want the outgoing rows", got)
	}

	got, err = reader.Run(context.Background(), "MATCH (n) RETURN incoming_entity_id", nil)
	if err != nil {
		t.Fatalf("Run(incoming) error = %v", err)
	}
	if len(got) != 1 || got[0]["which"] != "incoming" {
		t.Fatalf("Run(incoming) = %v, want the incoming rows", got)
	}
}

// TestFakeGraphReaderIncomingWithoutHandlerIsEmpty covers what a caller hits
// when it sets only RunFn: an incoming query must not fall through to the
// outgoing handler. Falling through would hand a dead-code test the outgoing
// rows and let it pass while proving nothing about incoming edges.
func TestFakeGraphReaderIncomingWithoutHandlerIsEmpty(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"which": "outgoing"}}, nil
		},
	}

	got, err := reader.Run(context.Background(), "RETURN incoming_entity_id", nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got != nil {
		t.Fatalf("Run() = %v, want nil when no incoming handler is set", got)
	}
}

// TestFakeGraphReaderSuppressesDeadCodeCandidateProbe pins the second dispatch
// rule. The dead-code scanner issues a paged non-function candidate probe
// (Class/Struct/Interface with SKIP and LIMIT) that most tests do not intend to
// answer. The fake returns nothing for it, so a test asserting on a different
// query is not handed those rows by accident.
func TestFakeGraphReaderSuppressesDeadCodeCandidateProbe(t *testing.T) {
	t.Parallel()

	const probe = "MATCH (e:Class) RETURN coalesce(e.uid, e.id) as entity_id SKIP $skip LIMIT $limit"

	reader := querytestutil.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"entity_id": "should-not-surface"}}, nil
		},
	}

	got, err := reader.Run(context.Background(), probe, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got != nil {
		t.Fatalf("Run(candidate probe) = %v, want nil", got)
	}
}

// TestFakeGraphReaderCandidateProbeNeedsEveryMarker guards the suppression from
// growing too wide. A query carrying only some of the probe's markers is an
// ordinary query and must still reach RunFn; broadening the match would
// silently blank out real rows.
func TestFakeGraphReaderCandidateProbeNeedsEveryMarker(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"entity_id": "real"}}, nil
		},
	}

	// The RETURN clause and the label, but no SKIP/LIMIT paging.
	got, err := reader.Run(context.Background(),
		"MATCH (e:Class) RETURN coalesce(e.uid, e.id) as entity_id", nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(got) != 1 || got[0]["entity_id"] != "real" {
		t.Fatalf("Run() = %v, want the real rows", got)
	}
}

// TestFakeGraphReaderZeroValueIsUsable matters because many callers construct
// the fake with no handler at all, purely to satisfy a port. The zero value has
// to answer rather than panic on a nil func.
func TestFakeGraphReaderZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var reader querytestutil.FakeGraphReader

	rows, err := reader.Run(context.Background(), "MATCH (n) RETURN n", nil)
	if err != nil || rows != nil {
		t.Fatalf("Run() = (%v, %v), want (nil, nil)", rows, err)
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (n) RETURN n", nil)
	if err != nil || row != nil {
		t.Fatalf("RunSingle() = (%v, %v), want (nil, nil)", row, err)
	}
}

// TestFakeGraphReaderRunSingleFallsBackToFirstRow pins the convenience that
// lets a caller set only RunFn and still satisfy a single-row read. Returning
// the last row instead would change which record the handler under test sees.
func TestFakeGraphReaderRunSingleFallsBackToFirstRow(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"n": 1}, {"n": 2}}, nil
		},
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (n) RETURN n", nil)
	if err != nil {
		t.Fatalf("RunSingle() error = %v", err)
	}
	if row["n"] != 1 {
		t.Fatalf("RunSingle() = %v, want the first row", row)
	}
}

// TestFakeGraphReaderRunSinglePrefersItsOwnHandler confirms RunSingleFn wins
// over the Run fallback, so a caller can give the two reads different answers.
func TestFakeGraphReaderRunSinglePrefersItsOwnHandler(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"from": "run"}}, nil
		},
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"from": "runSingle"}, nil
		},
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (n) RETURN n", nil)
	if err != nil {
		t.Fatalf("RunSingle() error = %v", err)
	}
	if row["from"] != "runSingle" {
		t.Fatalf("RunSingle() = %v, want the RunSingleFn answer", row)
	}
}

// TestFakeGraphReaderRunSinglePropagatesError pins that a failing Run surfaces
// as an error rather than an empty row. Tests asserting on a graph-read failure
// path depend on this; swallowing it would let them pass against a handler that
// never checks the error.
func TestFakeGraphReaderRunSinglePropagatesError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("graph unavailable")
	reader := querytestutil.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, sentinel
		},
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (n) RETURN n", nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunSingle() error = %v, want %v", err, sentinel)
	}
	if row != nil {
		t.Fatalf("RunSingle() row = %v, want nil on error", row)
	}
}
