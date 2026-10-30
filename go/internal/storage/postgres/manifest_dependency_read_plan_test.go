// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// manifestDependencyFakeRow returns one scanned manifest dependency row whose
// fact_id and observed_at sort by i.
func manifestDependencyFakeRow(i int) []any {
	return []any{
		fmt.Sprintf("fact-dep-%04d", i),
		"repository:repo-1",
		"generation-1",
		"content_entity",
		fmt.Sprintf("content_entity:dep-%04d", i),
		"1.0.0",
		"git",
		int64(0),
		"unknown",
		"git",
		fmt.Sprintf("content_entity:dep-%04d", i),
		"file:///repo/package.json",
		fmt.Sprintf("dep-%04d", i),
		time.Date(2026, time.May, 14, 10, 0, i, 0, time.UTC),
		false,
		[]byte(`{"entity_type":"Variable","entity_name":"left-pad","entity_metadata":{"config_kind":"dependency","package_manager":"npm"}}`),
	}
}

// TestListActivePackageManifestDependencyFactsPlansEveryPageCustom is the
// regression guard for #9586. Under pgx's default QueryExecModeCacheStatement
// each SQL text becomes a named prepared statement, and PostgreSQL switches it
// to the generic plan from the sixth execution on a connection. The generic
// plan compares entity_name against the $2 Param array linearly on every
// scanned row (a hashed ScalarArrayOp needs a Const array). Every page of both
// query texts must therefore run on the unnamed statement, which PostgreSQL
// plans with the bound values each time.
func TestListActivePackageManifestDependencyFactsPlansEveryPageCustom(t *testing.T) {
	t.Parallel()

	fullPage := make([][]any, 0, listFactsByKindPageSize)
	for i := range listFactsByKindPageSize {
		fullPage = append(fullPage, manifestDependencyFakeRow(i))
	}
	tests := []struct {
		name       string
		ecosystems []string
		responses  []queueFakeRows
		wantCalls  int
		wantPMs    []string
		wantUnion  bool
	}{
		{
			name:       "plain query, two pages",
			ecosystems: []string{"npm"},
			responses:  []queueFakeRows{{rows: fullPage}, {rows: [][]any{manifestDependencyFakeRow(listFactsByKindPageSize)}}},
			wantCalls:  2,
			wantPMs:    []string{"npm"},
		},
		{
			name:       "maven family query",
			ecosystems: []string{"maven"},
			responses:  []queueFakeRows{{}},
			wantCalls:  1,
			wantPMs:    []string{"gradle", "maven", "sbt"},
			wantUnion:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := &fakeExecQueryer{queryResponses: tt.responses}
			names := []string{"left-pad", "org.typelevel:cats-core_2.13"}
			loaded, err := NewFactStore(db).ListActivePackageManifestDependencyFacts(
				context.Background(), tt.ecosystems, names,
			)
			if err != nil {
				t.Fatalf("ListActivePackageManifestDependencyFacts() error = %v", err)
			}
			if got, want := len(db.queries), tt.wantCalls; got != want {
				t.Fatalf("query calls = %d, want %d", got, want)
			}
			if tt.wantCalls == 2 && len(loaded) != listFactsByKindPageSize+1 {
				t.Fatalf("loaded = %d rows, want %d", len(loaded), listFactsByKindPageSize+1)
			}
			for i, call := range db.queries {
				if !call.hasExecMode || call.execMode != pgx.QueryExecModeCacheDescribe {
					t.Fatalf("page %d exec mode = %v (set %v), want QueryExecModeCacheDescribe", i+1, call.execMode, call.hasExecMode)
				}
				if got := strings.Contains(call.query, "UNION ALL"); got != tt.wantUnion {
					t.Fatalf("page %d UNION ALL = %v, want %v", i+1, got, tt.wantUnion)
				}
				if got, want := len(call.args), 5; got != want {
					t.Fatalf("page %d bound args = %d, want %d: %#v", i+1, got, want, call.args)
				}
				if !reflect.DeepEqual(call.args[0], tt.wantPMs) || !reflect.DeepEqual(call.args[1], names) {
					t.Fatalf("page %d $1/$2 = %v / %v, want %v / %v", i+1, call.args[0], call.args[1], tt.wantPMs, names)
				}
				if got, want := call.args[4], listFactsByKindPageSize; got != want {
					t.Fatalf("page %d $5 = %v, want %v", i+1, got, want)
				}
			}
			if tt.wantCalls == 2 {
				if db.queries[0].args[2] != nil {
					t.Fatalf("page 1 cursor = %v, want nil", db.queries[0].args[2])
				}
				if got, want := db.queries[1].args[3], fmt.Sprintf("fact-dep-%04d", listFactsByKindPageSize-1); got != want {
					t.Fatalf("page 2 cursor fact_id = %v, want %v", got, want)
				}
			}
		})
	}
}

// TestFactStoreOtherActiveReadsKeepDefaultExecMode pins the scope of the
// #9586 fix: only the manifest dependency read opts out of the statement
// cache. A sibling active-fact read still runs under the pool's default mode.
func TestFactStoreOtherActiveReadsKeepDefaultExecMode(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	if _, err := NewFactStore(db).ListActiveRepositoryFacts(context.Background()); err != nil {
		t.Fatalf("ListActiveRepositoryFacts() error = %v", err)
	}
	if len(db.queries) == 0 {
		t.Fatal("ListActiveRepositoryFacts() issued no query")
	}
	for i, call := range db.queries {
		if call.hasExecMode {
			t.Fatalf("query %d pinned exec mode %v, want the pool default", i+1, call.execMode)
		}
	}
}
