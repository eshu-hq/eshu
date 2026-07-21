// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestCloudResourceOwnerBackfillerSeedsExistingGraphRowsBeforeCompletion(t *testing.T) {
	t.Parallel()

	graph := &scriptedCloudResourceBackfillGraph{pages: [][]map[string]any{
		{{
			"uid":            "uid-a",
			"id":             "uid-a",
			"resource_type":  "aws_iam_role",
			"collector_kind": "aws",
			"source_fact_id": "fact-a",
			"name":           "role-a",
		}},
		{},
	}}
	store := &recordingCloudResourceBackfillStore{}
	now := time.Date(2026, time.July, 21, 14, 30, 0, 0, time.UTC)
	backfiller := CloudResourceOwnerBackfiller{
		Graph: graph,
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := backfiller.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if !store.markedComplete {
		t.Fatal("Run() did not mark the backfill complete")
	}
	if got, want := len(store.seeded), 1; got != want {
		t.Fatalf("seeded entries = %d, want %d", got, want)
	}
	entry := store.seeded[0]
	if got, want := entry.UID, "uid-a"; got != want {
		t.Fatalf("seeded uid = %q, want %q", got, want)
	}
	if got, want := entry.SourceOrderKey, cloudResourceBackfillMinimumOrderKeyPrefix+"fact-a"; got != want {
		t.Fatalf("seeded source order key = %q, want %q", got, want)
	}
	if got := string(entry.WinningRow); got == "" || !containsAllBackfillFragments(got,
		`"uid":"uid-a"`, `"resource_type":"aws_iam_role"`,
		`"collector_kind":"aws"`, `"source_fact_id":"fact-a"`,
		`"name":"role-a"`,
	) {
		t.Fatalf("seeded winning row = %s, want complete existing graph row", got)
	}
	if got, want := store.markedAt, now; !got.Equal(want) {
		t.Fatalf("marked complete at = %s, want %s", got, want)
	}
}

func TestCloudResourceOwnerBackfillerCompleteSkipsGraph(t *testing.T) {
	t.Parallel()

	graph := &scriptedCloudResourceBackfillGraph{err: errors.New("graph must not be read")}
	store := &recordingCloudResourceBackfillStore{complete: true}
	backfiller := CloudResourceOwnerBackfiller{Graph: graph, Store: store}

	if err := backfiller.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if graph.calls != 0 {
		t.Fatalf("graph calls = %d, want 0", graph.calls)
	}
	if len(store.seeded) != 0 || store.markedComplete {
		t.Fatalf("completed backfill mutated store: seeded=%d marked=%t", len(store.seeded), store.markedComplete)
	}
}

func TestCloudResourceOwnerBackfillerPagesWithoutGaps(t *testing.T) {
	t.Parallel()

	graph := &scriptedCloudResourceBackfillGraph{pages: [][]map[string]any{
		{
			{"uid": "uid-a", "source_fact_id": "fact-a", "resource_type": "aws_s3_bucket"},
			{"uid": "uid-b", "source_fact_id": "fact-b", "resource_type": "aws_s3_bucket"},
		},
		{{"uid": "uid-c", "source_fact_id": "fact-c", "resource_type": "aws_s3_bucket"}},
	}}
	store := &recordingCloudResourceBackfillStore{}
	backfiller := CloudResourceOwnerBackfiller{Graph: graph, Store: store, PageSize: 2}

	if err := backfiller.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := graph.afterUIDs, []string{"", "uid-b"}; !equalStrings(got, want) {
		t.Fatalf("after uid sequence = %#v, want %#v", got, want)
	}
	if got, want := seededUIDs(store.seeded), []string{"uid-a", "uid-b", "uid-c"}; !equalStrings(got, want) {
		t.Fatalf("seeded uids = %#v, want %#v", got, want)
	}
}

func TestCloudResourceOwnerBackfillerRejectsUnattributableGraphRow(t *testing.T) {
	t.Parallel()

	graph := &scriptedCloudResourceBackfillGraph{pages: [][]map[string]any{{{
		"uid":           "uid-a",
		"resource_type": "aws_iam_role",
	}}}}
	store := &recordingCloudResourceBackfillStore{}
	backfiller := CloudResourceOwnerBackfiller{Graph: graph, Store: store}

	if err := backfiller.Run(context.Background()); err == nil {
		t.Fatal("Run() error = nil, want missing source_fact_id failure")
	}
	if store.markedComplete {
		t.Fatal("failed backfill marked complete")
	}
}

func TestCloudResourceOwnerBackfillerSeedFailureDoesNotMarkComplete(t *testing.T) {
	t.Parallel()

	seedErr := errors.New("postgres unavailable")
	graph := &scriptedCloudResourceBackfillGraph{pages: [][]map[string]any{{{
		"uid":            "uid-a",
		"resource_type":  "aws_iam_role",
		"source_fact_id": "fact-a",
	}}}}
	store := &recordingCloudResourceBackfillStore{seedErr: seedErr}
	backfiller := CloudResourceOwnerBackfiller{Graph: graph, Store: store}

	if err := backfiller.Run(context.Background()); !errors.Is(err, seedErr) {
		t.Fatalf("Run() error = %v, want wrapping %v", err, seedErr)
	}
	if store.markedComplete {
		t.Fatal("failed backfill marked complete")
	}
}

func TestCloudResourceOwnerBackfillQueriesAreUIDKeysetBounded(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"first": cloudResourceOwnerBackfillFirstPageQuery,
		"next":  cloudResourceOwnerBackfillNextPageQuery,
	} {
		t.Run(name, func(t *testing.T) {
			for _, want := range []string{
				"MATCH (n:CloudResource)",
				"n.uid AS uid",
				"n.resource_type AS resource_type",
				"n.source_fact_id AS source_fact_id",
				"ORDER BY n.uid",
				"LIMIT $limit",
			} {
				if !strings.Contains(query, want) {
					t.Errorf("%s query missing %q:\n%s", name, want, query)
				}
			}
		})
	}
	if strings.Contains(cloudResourceOwnerBackfillFirstPageQuery, "WHERE n.uid >") {
		t.Fatalf("first page unexpectedly has a cursor predicate:\n%s", cloudResourceOwnerBackfillFirstPageQuery)
	}
	if !strings.Contains(cloudResourceOwnerBackfillNextPageQuery, "WHERE n.uid > $after_uid") {
		t.Fatalf("next page lacks indexed uid cursor predicate:\n%s", cloudResourceOwnerBackfillNextPageQuery)
	}
}

type scriptedCloudResourceBackfillGraph struct {
	pages     [][]map[string]any
	err       error
	calls     int
	afterUIDs []string
}

func (g *scriptedCloudResourceBackfillGraph) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	g.calls++
	g.afterUIDs = append(g.afterUIDs, StringVal(params, "after_uid"))
	if g.err != nil {
		return nil, g.err
	}
	if len(g.pages) == 0 {
		return nil, nil
	}
	page := g.pages[0]
	g.pages = g.pages[1:]
	return page, nil
}

func (g *scriptedCloudResourceBackfillGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, errors.New("RunSingle must not be called")
}

type recordingCloudResourceBackfillStore struct {
	complete       bool
	seeded         []postgres.GraphNodeOwnerEntry
	seedErr        error
	markedComplete bool
	markedAt       time.Time
}

func (s *recordingCloudResourceBackfillStore) IsCloudResourceBackfillComplete(context.Context) (bool, error) {
	return s.complete, nil
}

func (s *recordingCloudResourceBackfillStore) SeedExistingGraphNodeOwners(
	_ context.Context,
	entries []postgres.GraphNodeOwnerEntry,
	_ time.Time,
) error {
	s.seeded = append(s.seeded, entries...)
	return s.seedErr
}

func (s *recordingCloudResourceBackfillStore) MarkCloudResourceBackfillComplete(_ context.Context, at time.Time) error {
	s.markedComplete = true
	s.markedAt = at
	return nil
}

func seededUIDs(entries []postgres.GraphNodeOwnerEntry) []string {
	uids := make([]string, 0, len(entries))
	for _, entry := range entries {
		uids = append(uids, entry.UID)
	}
	return uids
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func containsAllBackfillFragments(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}
