// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// This file pins issue #11470 P1: the production backfill wiring must not
// silently skip the page guards. cmd/ingester/main.go passes
// *InstrumentedDB (wrapping postgres.SQLDB) into the backfill loaders, but
// only SQLDB implements backfill.PageConnProvider, so runGuardedBackfillQuery
// took its fallback leg — no SET LOCAL statement_timeout and no
// path-distinct application_name. *InstrumentedDB must forward the provider
// capability to its Inner handle, and these tests run the paged loaders
// through that production-shaped wrapper asserting both SET LOCAL guards on
// every page transaction.

// guardedBackfillProvider is a hermetic backfill.PageConnProvider whose page
// transactions record every Exec (the SET LOCAL guards) and serve scripted
// fact pages FIFO. Direct QueryContext/ExecContext calls record a fallback
// leg: the loader ran on the pooled handle with no guards.
type guardedBackfillProvider struct {
	mu            sync.Mutex
	pages         [][][]any
	conns         []*guardedBackfillConn
	txns          []*guardedBackfillTx
	directQueries int
	directExecs   int
}

var (
	_ backfill.PageConnProvider = (*guardedBackfillProvider)(nil)
	_ db.ExecQueryer            = (*guardedBackfillProvider)(nil)
)

func (p *guardedBackfillProvider) OpenBackfillPageConn(context.Context) (backfill.PageConn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	conn := &guardedBackfillConn{provider: p}
	p.conns = append(p.conns, conn)
	return conn, nil
}

func (p *guardedBackfillProvider) QueryContext(
	_ context.Context, _ string, _ ...any,
) (db.Rows, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.directQueries++
	return &queueFakeRows{}, nil
}

func (p *guardedBackfillProvider) ExecContext(
	_ context.Context, _ string, _ ...any,
) (sql.Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.directExecs++
	return fakeResult{}, nil
}

type guardedBackfillConn struct {
	provider *guardedBackfillProvider
	closed   bool
}

var _ backfill.PageConn = (*guardedBackfillConn)(nil)

func (c *guardedBackfillConn) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (c *guardedBackfillConn) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return &queueFakeRows{}, nil
}

func (c *guardedBackfillConn) Begin(context.Context) (db.Transaction, error) {
	c.provider.mu.Lock()
	defer c.provider.mu.Unlock()
	tx := &guardedBackfillTx{provider: c.provider}
	c.provider.txns = append(c.provider.txns, tx)
	return tx, nil
}

func (c *guardedBackfillConn) Close() error {
	c.closed = true
	return nil
}

type guardedBackfillTx struct {
	provider  *guardedBackfillProvider
	execs     []string
	committed bool
}

var _ db.Transaction = (*guardedBackfillTx)(nil)

func (tx *guardedBackfillTx) ExecContext(
	_ context.Context, query string, _ ...any,
) (sql.Result, error) {
	tx.execs = append(tx.execs, query)
	return fakeResult{}, nil
}

func (tx *guardedBackfillTx) QueryContext(
	_ context.Context, _ string, _ ...any,
) (db.Rows, error) {
	tx.provider.mu.Lock()
	defer tx.provider.mu.Unlock()
	if len(tx.provider.pages) == 0 {
		return &queueFakeRows{}, nil
	}
	page := tx.provider.pages[0]
	tx.provider.pages = tx.provider.pages[1:]
	return &queueFakeRows{rows: page}, nil
}

func (tx *guardedBackfillTx) Commit() error {
	tx.committed = true
	return nil
}

func (tx *guardedBackfillTx) Rollback() error { return nil }

// assertBackfillPageGuards requires the provider leg: one dedicated,
// now-closed connection per page, every page transaction stamped with exactly
// the two SET LOCAL guards (statement_timeout then application_name) and
// committed, and zero direct fallback queries on the pooled handle.
func assertBackfillPageGuards(t *testing.T, p *guardedBackfillProvider, wantTimeout, wantApp string) {
	t.Helper()
	if p.directQueries != 0 || p.directExecs != 0 {
		t.Fatalf("fallback leg taken: directQueries=%d directExecs=%d, want 0 (page guards skipped)",
			p.directQueries, p.directExecs)
	}
	if len(p.conns) == 0 {
		t.Fatal("no dedicated page connections opened: provider leg was skipped, page guards unset")
	}
	for i, conn := range p.conns {
		if !conn.closed {
			t.Fatalf("page connection %d was not closed after its page", i)
		}
	}
	if len(p.txns) != len(p.conns) {
		t.Fatalf("page transactions = %d, want one per dedicated connection (%d)", len(p.txns), len(p.conns))
	}
	for i, tx := range p.txns {
		if len(tx.execs) != 2 {
			t.Fatalf("page %d execs = %v, want 2 (SET LOCAL statement_timeout + application_name)", i, tx.execs)
		}
		if want, got := "SET LOCAL statement_timeout = '"+wantTimeout+"'", tx.execs[0]; got != want {
			t.Fatalf("page %d guard[0] = %q, want %q", i, got, want)
		}
		if want, got := "SET LOCAL application_name = '"+wantApp+"'", tx.execs[1]; got != want {
			t.Fatalf("page %d guard[1] = %q, want %q", i, got, want)
		}
		if !tx.committed {
			t.Fatalf("page %d transaction was not committed", i)
		}
	}
}

// TestInstrumentedDBForwardsBackfillPageGuards runs the anchor-scoped paged
// loader (scoped_facts.go) through the production-shaped handle —
// *InstrumentedDB wrapping a provider, exactly as cmd/ingester/main.go wires
// it — and requires every page to carry both SET LOCAL guards. RED while
// *InstrumentedDB hides the provider capability (fallback leg, zero page
// transactions); GREEN once it forwards OpenBackfillPageConn to Inner.
func TestInstrumentedDBForwardsBackfillPageGuards(t *testing.T) {
	t.Parallel()

	payload := `{"content_path":"main.tf","content_body":"module \"x\" { source = \"./payments-service\" }"}`
	provider := &guardedBackfillProvider{
		pages: [][][]any{
			{
				contentFactRow("fact-1", "scope-1", "gen-1", "content", payload),
				contentFactRow("fact-2", "scope-1", "gen-1", "content", payload),
			},
		},
	}
	// Production shape: ingester wraps postgres.SQLDB in *InstrumentedDB.
	wrapped := &InstrumentedDB{Inner: provider, StoreName: "ingester"}

	catalog := []relationships.CatalogEntry{{RepoID: "payments-service", Aliases: []string{"payments-service"}}}
	loaded, _, err := backfill.LoadAnchorScopedRelationshipFactsBudget(
		context.Background(), wrapped, catalog, nil, backfill.RelationshipFactLoadBudget{},
	)
	if err != nil {
		t.Fatalf("LoadAnchorScopedRelationshipFactsBudget() error = %v", err)
	}
	assertBackfillPageGuards(t, provider, "300s", backfill.BackfillApplicationNamePerCommit)
	if len(loaded) != 2 {
		t.Fatalf("loaded %d facts, want 2", len(loaded))
	}
}

// TestInstrumentedDBForwardsArgoCDPageGuards is the same forwarding contract
// for the ArgoCD config-repo paged loader (argocd_config.go): through the
// production-shaped wrapper every page must carry both SET LOCAL guards.
func TestInstrumentedDBForwardsArgoCDPageGuards(t *testing.T) {
	t.Parallel()

	payload := `{"repo_id":"cfg-repo","content_path":"app.yaml","content_body":"kind: Application"}`
	provider := &guardedBackfillProvider{
		pages: [][][]any{
			{contentFactRow("cfg-1", "scope-cfg", "gen-cfg", "content", payload)},
		},
	}
	wrapped := &InstrumentedDB{Inner: provider, StoreName: "ingester"}

	loaded, err := backfill.LoadArgoCDGeneratorConfigFacts(context.Background(), wrapped, []string{"cfg-repo"})
	if err != nil {
		t.Fatalf("LoadArgoCDGeneratorConfigFacts() error = %v", err)
	}
	assertBackfillPageGuards(t, provider, "300s", "eshu-backfill")
	if len(loaded) != 1 {
		t.Fatalf("loaded %d facts, want 1", len(loaded))
	}
}
