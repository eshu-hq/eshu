// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcantargets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// crossScopeTargetQueryerStub answers only the scope-state probe and records
// its bound arguments.
type crossScopeTargetQueryerStub struct {
	rows [][]any
	args []any
	err  error
}

func (q *crossScopeTargetQueryerStub) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	if query != candidateScopesQuery {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	q.args = args
	if q.err != nil {
		return nil, q.err
	}
	return &fakeRows{rows: q.rows}, nil
}

type recordedFactListCall struct {
	scopeID, generationID, factKind, payloadKey string
	values                                      []string
}

// crossScopeTargetFactsStub returns one aws_resource envelope per requested
// arn, recording every pinned (scope, generation) read.
type crossScopeTargetFactsStub struct {
	calls []recordedFactListCall
}

func (f *crossScopeTargetFactsStub) ListFactsByKindAndPayloadValue(
	_ context.Context, scopeID, generationID, factKind, payloadKey string, values []string,
) ([]facts.Envelope, error) {
	f.calls = append(f.calls, recordedFactListCall{scopeID, generationID, factKind, payloadKey, slices.Clone(values)})
	out := make([]facts.Envelope, 0, len(values))
	for _, value := range values {
		out = append(out, facts.Envelope{FactKind: facts.AWSResourceFactKind, ScopeID: scopeID, Payload: map[string]any{"arn": value}})
	}
	return out, nil
}

// TestStoreLoadsPinnedActiveGenerations proves the
// store binds the account, service/region pairs, and excluded scope; reports
// every candidate scope's state; and reads facts only from scopes with an
// active generation, pinned to that generation id and filtered to the ARNs of
// the scope's own service.
func TestStoreLoadsPinnedActiveGenerations(t *testing.T) {
	t.Parallel()
	queryer := &crossScopeTargetQueryerStub{rows: [][]any{
		{"aws:123456789012:us-east-1:s3", "s3-gen-2", true, true, false},
		{"aws:123456789012:us-west-2:kms", "", false, false, true},
	}}
	factsStub := &crossScopeTargetFactsStub{}
	store := Store{DB: queryer, Facts: factsStub}
	snapshot, err := store.LoadCrossScopeTargets(context.Background(), iamcan.CrossScopeTargetRequest{
		AccountID:      "123456789012",
		ExcludeScopeID: "aws:123456789012:us-east-1:iam",
		Targets: []iamcan.CrossScopeTarget{
			{ServiceKind: "kms", Region: "us-west-2", ARN: "arn:aws:kms:us-west-2:123456789012:key/abc"},
			{ServiceKind: "s3", ARN: "arn:aws:s3:::receipts"},
		},
	})
	if err != nil {
		t.Fatalf("LoadCrossScopeTargets() error = %v", err)
	}

	if got := queryer.args[0]; got != "123456789012" {
		t.Fatalf("account arg = %v", got)
	}
	if got, want := []string(queryer.args[1].(pgarray.StringArray)), []string{"kms", "s3"}; !slices.Equal(got, want) {
		t.Fatalf("service kinds = %v, want %v", got, want)
	}
	if got, want := []string(queryer.args[2].(pgarray.StringArray)), []string{"us-west-2", ""}; !slices.Equal(got, want) {
		t.Fatalf("regions = %v, want %v", got, want)
	}
	if got := queryer.args[3]; got != "aws:123456789012:us-east-1:iam" {
		t.Fatalf("excluded scope arg = %v", got)
	}

	wantScopes := []iamcan.CrossScopeTargetScope{
		{ScopeID: "aws:123456789012:us-east-1:s3", ActiveGenerationID: "s3-gen-2", GenerationActive: true, NodesCommitted: true},
		{ScopeID: "aws:123456789012:us-west-2:kms", GenerationPending: true},
	}
	if !slices.Equal(snapshot.Scopes, wantScopes) {
		t.Fatalf("scopes = %+v, want %+v", snapshot.Scopes, wantScopes)
	}
	wantCalls := []recordedFactListCall{{
		scopeID: "aws:123456789012:us-east-1:s3", generationID: "s3-gen-2",
		factKind: facts.AWSResourceFactKind, payloadKey: "arn", values: []string{"arn:aws:s3:::receipts"},
	}}
	if len(factsStub.calls) != 1 || factsStub.calls[0].scopeID != wantCalls[0].scopeID ||
		factsStub.calls[0].generationID != wantCalls[0].generationID ||
		factsStub.calls[0].factKind != wantCalls[0].factKind || factsStub.calls[0].payloadKey != "arn" ||
		!slices.Equal(factsStub.calls[0].values, wantCalls[0].values) {
		t.Fatalf("fact reads = %+v, want %+v", factsStub.calls, wantCalls)
	}
	if got := len(snapshot.Resources["aws:123456789012:us-east-1:s3"]); got != 1 {
		t.Fatalf("s3 resources = %d, want 1", got)
	}
}

// TestStoreSurfacesErrors proves a store that
// cannot answer returns an error, never an empty snapshot the handler would
// read as "settled, unresolved".
func TestStoreSurfacesErrors(t *testing.T) {
	t.Parallel()
	store := Store{
		DB:    &crossScopeTargetQueryerStub{err: errors.New("connection reset")},
		Facts: &crossScopeTargetFactsStub{},
	}
	_, err := store.LoadCrossScopeTargets(context.Background(), iamcan.CrossScopeTargetRequest{
		AccountID: "123456789012",
		Targets:   []iamcan.CrossScopeTarget{{ServiceKind: "s3", ARN: "arn:aws:s3:::receipts"}},
	})
	if err == nil {
		t.Fatal("LoadCrossScopeTargets() error = nil, want the query error")
	}
}

// TestStoreEmptyRequestIsFree proves an intent
// with no exact cross-scope target costs no query.
func TestStoreEmptyRequestIsFree(t *testing.T) {
	t.Parallel()
	queryer := &crossScopeTargetQueryerStub{err: errors.New("must not be called")}
	store := Store{DB: queryer, Facts: &crossScopeTargetFactsStub{}}
	if _, err := store.LoadCrossScopeTargets(context.Background(), iamcan.CrossScopeTargetRequest{AccountID: "123456789012"}); err != nil {
		t.Fatalf("LoadCrossScopeTargets() error = %v", err)
	}
}

// fakeRows replays fixed rows of string and bool columns.
type fakeRows struct {
	rows  [][]any
	index int
}

func (r *fakeRows) Next() bool { return r.index < len(r.rows) }

func (r *fakeRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	r.index++
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			*target = row[i].(string)
		case *bool:
			*target = row[i].(bool)
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	return nil
}

func (r *fakeRows) Err() error   { return nil }
func (r *fakeRows) Close() error { return nil }
