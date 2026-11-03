// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
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
	facts   map[string]string
	missing map[string]bool
	// payloads overrides the raw payload bytes for a fact ID (default {"id":..}).
	payloads map[string][]byte
	// misorderOrdered makes the ordered payload page return its rows reversed.
	misorderOrdered bool
	identity        []string
	calls           []supplyChainImpactRoutedCall
	commits         int
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
			rows = append(rows, r.factRow(id))
		}
		return &queueFakeRows{rows: rows}, nil
	case factimpact.PayloadPageOrderedSQL:
		r.calls = append(r.calls, supplyChainImpactRoutedCall{Label: "payload_page_ordered", Args: args})
		ids := slices.Clone(args[0].([]string))
		slices.SortFunc(ids, func(a, b string) int { return bytes.Compare([]byte(a), []byte(b)) })
		if r.misorderOrdered {
			slices.Reverse(ids)
		}
		var rows [][]any
		for _, id := range ids {
			if r.missing[id] {
				continue
			}
			rows = append(rows, r.factRow(id))
		}
		return &queueFakeRows{rows: rows}, nil
	case listFactsByKindAndPayloadValueQuery, protectedStaticFactsByPayloadQuery,
		listFactsByKindAndPayloadValueCursorQuery, protectedStaticFactsByPayloadCursorQuery:
		r.calls = append(r.calls, supplyChainImpactRoutedCall{Label: "python_fold", Args: args})
		return &queueFakeRows{rows: r.pythonFoldPayloadRows(args)}, nil
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

// pythonFoldPayloadRows answers a payload-value page: facts of the requested
// kind whose payload key is allowlisted, in stable fact_id order past the
// cursor, capped at the page limit. Every fake row shares one observed_at, so
// the cursor reduces to the fact_id bound. It renders the caller's scope and
// generation onto each row.
func (r *supplyChainImpactRoutingDB) pythonFoldPayloadRows(args []any) [][]any {
	scopeID := args[0].(string)
	generationID := args[1].(string)
	kind := args[2].(string)
	key := args[3].(string)
	values := args[4].([]string)
	var cursorFactID string
	var limit int
	if len(args) == 6 {
		limit = args[5].(int)
	} else {
		cursorFactID = args[6].(string)
		limit = args[7].(int)
	}
	allow := map[string]bool{}
	for _, value := range values {
		allow[value] = true
	}
	var ids []string
	for id, factKind := range r.facts {
		if factKind != kind {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal(r.payloads[id], &decoded); err != nil {
			continue
		}
		value, _ := decoded[key].(string)
		if !allow[value] {
			continue
		}
		if cursorFactID != "" && id <= cursorFactID {
			continue
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var rows [][]any
	for _, id := range ids {
		if len(rows) >= limit {
			break
		}
		row := r.factRow(id)
		row[1] = scopeID
		row[2] = generationID
		rows = append(rows, row)
	}
	return rows
}

// factRow renders one payload-page row for a configured fact ID.
func (r *supplyChainImpactRoutingDB) factRow(id string) []any {
	row := supplyChainImpactRoutedFactRow(id, r.facts[id])
	if payload, ok := r.payloads[id]; ok {
		row[15] = payload
	}
	return row
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
