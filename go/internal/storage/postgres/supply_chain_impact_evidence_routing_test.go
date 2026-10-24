// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

// supplyChainImpactRoutedCall is one statement the hermetic routing fake saw.
type supplyChainImpactRoutedCall struct {
	// Label is the catalog branch name, or scope_resolution, payload_page,
	// identity.
	Label string
	Args  []any
}

// supplyChainImpactRoutingDB answers the active-evidence loader's statements
// by SQL text: catalog branches from branchRows, scope resolution from
// scopes, payload pages from facts, and identity pages from identity. It
// records every call so a test can assert which statements ran and what they
// bound. It is the hermetic stand-in for the live differential.
type supplyChainImpactRoutingDB struct {
	branchRows map[string][][2]string
	// scopes maps a repository list value to its (scope_id, generation_id).
	scopes map[string][2]string
	// facts maps a fact ID to its kind for payload pages; missing drops IDs.
	facts    map[string]string
	missing  map[string]bool
	identity []string
	calls    []supplyChainImpactRoutedCall
	commits  int
}

func (r *supplyChainImpactRoutingDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, fmt.Errorf("routing fake: query outside the snapshot transaction")
}

func (r *supplyChainImpactRoutingDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("routing fake: unexpected exec")
}

// BeginReadOnlyRepeatableRead returns the routing transaction.
func (r *supplyChainImpactRoutingDB) BeginReadOnlyRepeatableRead(context.Context) (db.Transaction, error) {
	return &supplyChainImpactRoutingTx{db: r}, nil
}

type supplyChainImpactRoutingTx struct{ db *supplyChainImpactRoutingDB }

func (tx *supplyChainImpactRoutingTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("routing fake: unexpected exec")
}

func (tx *supplyChainImpactRoutingTx) Commit() error   { tx.db.commits++; return nil }
func (tx *supplyChainImpactRoutingTx) Rollback() error { return nil }

func (tx *supplyChainImpactRoutingTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	r := tx.db
	switch query {
	case factimpact.ScopeResolutionSQL:
		r.calls = append(r.calls, supplyChainImpactRoutedCall{Label: "scope_resolution", Args: args})
		var rows [][]any
		for _, value := range args[0].([]string) {
			if pair, ok := r.scopes[value]; ok {
				rows = append(rows, []any{pair[0], pair[1]})
			}
		}
		return &queueFakeRows{rows: rows}, nil
	case factimpact.PayloadPageSQL:
		r.calls = append(r.calls, supplyChainImpactRoutedCall{Label: "payload_page", Args: args})
		var rows [][]any
		for _, id := range args[0].([]string) {
			if r.missing[id] {
				continue
			}
			rows = append(rows, supplyChainImpactRoutedFactRow(id, r.facts[id]))
		}
		return &queueFakeRows{rows: rows}, nil
	case listCurrentContainerImageIdentitySupportFactsQuery:
		r.calls = append(r.calls, supplyChainImpactRoutedCall{Label: "identity", Args: args})
		cursor, limit := args[5].(string), args[6].(int)
		var rows [][]any
		for _, id := range r.identity {
			if (cursor == "" || id > cursor) && len(rows) < limit {
				rows = append(rows, supplyChainImpactRoutedFactRow(id, supplyChainImpactContainerImageIdentityFactKind))
			}
		}
		return &queueFakeRows{rows: rows}, nil
	}
	for _, branch := range factimpact.Branches() {
		if branch.SQL != query {
			continue
		}
		if len(args) != len(branch.Params) {
			return nil, fmt.Errorf("routing fake: branch %s bound %d args for %d params", branch.Name, len(args), len(branch.Params))
		}
		r.calls = append(r.calls, supplyChainImpactRoutedCall{Label: branch.Name, Args: args})
		var rows [][]any
		for _, row := range r.branchRows[branch.Name] {
			rows = append(rows, []any{row[0], row[1]})
		}
		return &queueFakeRows{rows: rows}, nil
	}
	return nil, fmt.Errorf("routing fake: unrouted statement:\n%s", query)
}

// supplyChainImpactRoutedFactRow renders the sixteen loader columns.
func supplyChainImpactRoutedFactRow(id, kind string) []any {
	return []any{
		id, "scope:" + id, "generation:" + id, kind, "stable:" + id, "1.0.0", "synthetic",
		int64(1), "reported", "synthetic", "source:" + id, "", "",
		time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC), false, []byte(`{"id":"` + id + `"}`),
	}
}

// supplyChainImpactSupportID builds a parseable identity support fact ID.
func supplyChainImpactSupportID(n int) string {
	return fmt.Sprintf("%s%x:%x:%064x", containerImageIdentitySupportFactIDPrefix, "scope-a", fmt.Sprintf("%06d", n), n)
}

// labels returns the routed call labels in order.
func (r *supplyChainImpactRoutingDB) labels() []string {
	out := make([]string, 0, len(r.calls))
	for _, call := range r.calls {
		out = append(out, call.Label)
	}
	return out
}

// callsFor returns the calls with one label.
func (r *supplyChainImpactRoutingDB) callsFor(label string) []supplyChainImpactRoutedCall {
	var out []supplyChainImpactRoutedCall
	for _, call := range r.calls {
		if call.Label == label {
			out = append(out, call)
		}
	}
	return out
}

// boundList returns the list a routed branch call bound for one Param.
func boundList(t *testing.T, call supplyChainImpactRoutedCall, param factimpact.Param) []string {
	t.Helper()
	for _, branch := range factimpact.Branches() {
		if branch.Name != call.Label {
			continue
		}
		for i, bound := range branch.Params {
			if bound == param {
				list, _ := call.Args[i].([]string)
				return list
			}
		}
		t.Fatalf("branch %s binds no param %d", branch.Name, param)
	}
	t.Fatalf("no catalog branch named %s", call.Label)
	return nil
}
