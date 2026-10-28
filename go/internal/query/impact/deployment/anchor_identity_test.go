// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// stubIdentityRows scripts a db.Rows for scanLiveIdentityMatchRows: each
// row carries cluster, object, group-version-resource, and an optional
// ready-replica count, mirroring the SELECT column order.
type stubIdentityRows struct {
	rows [][]any
	pos  int
	err  error
}

func (s *stubIdentityRows) Next() bool {
	s.pos++
	return s.pos <= len(s.rows)
}

func (s *stubIdentityRows) Scan(dests ...any) error {
	if s.err != nil {
		return s.err
	}
	values := s.rows[s.pos-1]
	for i, dest := range dests {
		switch target := dest.(type) {
		case *sql.NullString:
			if values[i] == nil {
				*target = sql.NullString{}
			} else {
				*target = sql.NullString{String: values[i].(string), Valid: true}
			}
		case *sql.NullInt64:
			if values[i] == nil {
				*target = sql.NullInt64{}
			} else {
				*target = sql.NullInt64{Int64: values[i].(int64), Valid: true}
			}
		default:
			return errors.New("stubIdentityRows: unexpected scan destination")
		}
	}
	return nil
}

func (s *stubIdentityRows) Err() error { return nil }

func (s *stubIdentityRows) Close() error { return nil }

// stubDBQueryer scripts a db.Queryer for the read-store constructor
// proof: every query fails with the scripted error.
type stubDBQueryer struct {
	db.Queryer
	err error
}

func (s stubDBQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, s.err
}

func TestResolveImpactAnchorNode(t *testing.T) {
	t.Parallel()

	t.Run("ShapesAnchor", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{
				"id": "n1", "name": "api", "label": "Service",
				"labels": []any{"Service"}, "uid": "uid-1", "repo_id": "repo-a",
			}, nil
		}}
		got, err := ResolveImpactAnchorNode(t.Context(), reader, "id", "n1")
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got == nil || got.ID != "n1" || got.Label != "Service" || got.RepoID != "repo-a" {
			t.Fatalf("anchor = %+v, want the shaped node", got)
		}
	})
	t.Run("NilRowAndBlankLabelResolveNothing", func(t *testing.T) {
		t.Parallel()
		empty := graph.FakeGraphReader{RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, nil
		}}
		if got, err := ResolveImpactAnchorNode(t.Context(), empty, "id", "missing"); got != nil || err != nil {
			t.Fatalf("missing = (%v, %v), want (nil, nil)", got, err)
		}
		blank := graph.FakeGraphReader{RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"id": "n1", "label": ""}, nil
		}}
		if got, err := ResolveImpactAnchorNode(t.Context(), blank, "id", "n1"); got != nil || err != nil {
			t.Fatalf("blank label = (%v, %v), want (nil, nil)", got, err)
		}
	})
	t.Run("ReaderErrorPropagates", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, errors.New("backend down")
		}}
		if _, err := ResolveImpactAnchorNode(t.Context(), reader, "id", "n1"); err == nil {
			t.Fatal("error = nil, want the reader failure propagated")
		}
	})
}

func TestImpactHopShapers(t *testing.T) {
	t.Parallel()

	t.Run("TraceHops", func(t *testing.T) {
		t.Parallel()
		got := ImpactTraceHops([]ImpactRelProvenance{
			{RelType: "CALLS", Confidence: 0.9, HasConf: true, Reason: "static_call"},
			{RelType: "REFERENCES"},
		})
		if len(got) != 2 {
			t.Fatalf("hops = %v, want two", got)
		}
		if got[0]["type"] != "CALLS" || got[0]["confidence"] != 0.9 || got[0]["reason"] != "static_call" {
			t.Fatalf("first hop = %v, want type, confidence, and reason", got[0])
		}
		if _, hasConf := got[1]["confidence"]; hasConf {
			t.Fatalf("second hop = %v, want no confidence without HasConf", got[1])
		}
		if got := ImpactTraceHops(nil); len(got) != 0 {
			t.Fatalf("nil hops = %v, want empty", got)
		}
	})
	t.Run("DependencyHops", func(t *testing.T) {
		t.Parallel()
		nodes := []ImpactNodeIdentity{{ID: "a", Name: "api"}, {ID: "b", Name: "db"}}
		rels := []ImpactRelProvenance{{RelType: "DEPENDS_ON", Confidence: 0.7, HasConf: true, Reason: "config"}}
		got := ImpactDependencyHops(nodes, rels)
		if len(got) != 1 {
			t.Fatalf("hops = %v, want one zipped hop", got)
		}
		hop := got[0]
		if hop["from_id"] != "a" || hop["from_name"] != "api" || hop["to_id"] != "b" || hop["to_name"] != "db" {
			t.Fatalf("hop endpoints = %v, want source toward target order", hop)
		}
		if hop["type"] != "DEPENDS_ON" || hop["confidence"] != 0.7 || hop["reason"] != "config" {
			t.Fatalf("hop provenance = %v, want type, confidence, and reason", hop)
		}
	})
}

func TestScanLiveIdentityMatchRows(t *testing.T) {
	t.Parallel()

	t.Run("DecodesRows", func(t *testing.T) {
		t.Parallel()
		rows := &stubIdentityRows{rows: [][]any{
			{"prod", "obj-1", "apps/v1/deployments", int64(3)},
			{"prod", "obj-2", nil, nil},
		}}
		got, err := scanLiveIdentityMatchRows(rows)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(got) != 2 {
			t.Fatalf("matches = %+v, want two", got)
		}
		if got[0].ClusterID != "prod" || got[0].ObjectID != "obj-1" || got[0].GroupVersionResource != "apps/v1/deployments" {
			t.Fatalf("first match = %+v, want identity columns decoded", got[0])
		}
		if got[1].GroupVersionResource != "" {
			t.Fatalf("second match = %+v, want NULL columns as empty", got[1])
		}
	})
	t.Run("ScanErrorPropagates", func(t *testing.T) {
		t.Parallel()
		rows := &stubIdentityRows{rows: [][]any{{"prod", "obj-1", "gvr", int64(1)}}, err: errors.New("row lost")}
		if _, err := scanLiveIdentityMatchRows(rows); err == nil {
			t.Fatal("error = nil, want the scan failure propagated")
		}
	})
}

func TestLegacyKubernetesPodTemplateReader(t *testing.T) {
	t.Parallel()

	// The inner queryer speaks *sql.Rows, which no fake can mint, so a
	// lazily-opened handle against a closed localhost port stands in:
	// sql.Open dials nothing, and the query fails closed with a
	// connection error the delegate returns untouched.
	handle, err := sql.Open("pgx", "postgres://eshu:eshu@127.0.0.1:1/eshu?sslmode=disable")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil (open dials nothing)", err)
	}
	defer func() { _ = handle.Close() }()
	reader := legacyKubernetesPodTemplateReader{inner: handle}
	if _, err := reader.QueryContext(t.Context(), "SELECT 1"); err == nil {
		t.Fatal("error = nil, want the refused connection propagated")
	}
}

func TestNewPostgresKubernetesPodTemplateStores(t *testing.T) {
	t.Parallel()

	if store := NewPostgresKubernetesPodTemplateStore(nil); store.DB != nil {
		t.Fatalf("store = %+v, want the nil queryer carried", store)
	}
	inner, err := sql.Open("pgx", "postgres://eshu:eshu@127.0.0.1:1/eshu?sslmode=disable")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil (open dials nothing)", err)
	}
	defer func() { _ = inner.Close() }()
	if store := NewPostgresKubernetesPodTemplateStore(inner); store.DB == nil {
		t.Fatalf("store = %+v, want the queryer carried", store)
	}
	// The read-store constructor prefers the guarded query-only
	// connection: queryer() must return it instead of the database
	// handle, so live reads never fork onto a second connection.
	guarded := stubDBQueryer{err: errors.New("guarded")}
	guardedStore := NewPostgresKubernetesPodTemplateStoreWithReadStore(guarded)
	if _, err := guardedStore.queryer().QueryContext(t.Context(), "SELECT 1"); err == nil {
		t.Fatal("error = nil, want reads routed through the guarded store")
	}
}
