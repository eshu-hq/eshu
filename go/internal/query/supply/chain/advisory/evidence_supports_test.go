// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// supportsEvidenceQueryer answers the supports readiness probe with ready (or an
// error) and every other statement with no rows, recording each statement.
type supportsEvidenceQueryer struct {
	queries  []string
	ready    bool
	probeErr error
}

func (q *supportsEvidenceQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.queries = append(q.queries, query)
	if strings.Contains(query, "supply_chain_impact_finding_supports_backfill") {
		if q.probeErr != nil {
			return nil, q.probeErr
		}
		return &readyRows{ready: q.ready}, nil
	}
	return &cov7EvidenceRows{}, nil
}

type readyRows struct {
	ready bool
	done  bool
}

func (r *readyRows) Next() bool { return !r.done }
func (r *readyRows) Scan(dest ...any) error {
	*dest[0].(*bool) = r.ready
	r.done = true
	return nil
}
func (*readyRows) Err() error   { return nil }
func (*readyRows) Close() error { return nil }

func TestEvidenceSupportsRouting(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		gate     bool
		filter   EvidenceFilter
		wantRead string
		probed   bool
	}{
		"repository anchor reads supports":    {true, EvidenceFilter{RepositoryID: "repository:r1", Limit: 5}, "JOIN supply_chain_impact_finding_supports AS fact", true},
		"service anchor reads supports":       {true, EvidenceFilter{ServiceID: "service:a", Limit: 5}, "JOIN supply_chain_impact_finding_supports AS fact", true},
		"workload anchor reads supports":      {true, EvidenceFilter{WorkloadID: "workload:w", Limit: 5}, "JOIN supply_chain_impact_finding_supports AS fact", true},
		"cve only reads no impact finding":    {true, EvidenceFilter{CVEID: "CVE-2026-1", Limit: 5}, "JOIN fact_records AS fact", false},
		"gate off keeps the legacy fact read": {false, EvidenceFilter{RepositoryID: "repository:r1", Limit: 5}, "JOIN fact_records AS fact", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := &supportsEvidenceQueryer{ready: true}
			store := NewPostgresEvidenceStoreWithReadModel(reader, tc.gate)
			if _, err := store.ListAdvisoryEvidence(t.Context(), tc.filter); err != nil {
				t.Fatalf("list = %v, want a page", err)
			}
			probed := len(reader.queries) == 2
			if probed != tc.probed || (!probed && len(reader.queries) != 1) {
				t.Fatalf("issued %d statements, want probe=%v", len(reader.queries), tc.probed)
			}
			read := reader.queries[len(reader.queries)-1]
			if !strings.Contains(read, tc.wantRead) {
				t.Fatalf("read does not contain %q:\n%s", tc.wantRead, read)
			}
			if tc.wantRead == "JOIN supply_chain_impact_finding_supports AS fact" &&
				strings.Contains(read, "fact.fact_kind = 'reducer_supply_chain_impact_finding'") {
				t.Fatal("a supports read still reads impact finding facts")
			}
		})
	}
}

func TestEvidenceSupportsRefusesWhenNotReady(t *testing.T) {
	t.Parallel()
	for name, reader := range map[string]*supportsEvidenceQueryer{
		"behind the contract or fenced": {ready: false},
		"readiness probe failed":        {probeErr: errors.New("relation does not exist")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := NewPostgresEvidenceStoreWithReadModel(reader, true)
			_, err := store.ListAdvisoryEvidence(t.Context(), EvidenceFilter{RepositoryID: "repository:r1", Limit: 5})
			if !errors.Is(err, ErrEvidenceSupportsUnavailable) {
				t.Fatalf("list = %v, want ErrEvidenceSupportsUnavailable", err)
			}
			if len(reader.queries) != 1 {
				t.Fatalf("a refused call issued %d statements, want only the probe (never the fact read)", len(reader.queries))
			}
		})
	}
}

func TestEvidenceSupportsDerivationFailsOnALostMarker(t *testing.T) {
	t.Parallel()
	for name, marker := range map[string]string{
		"repository probe": "AND fact.payload->>'repository_id' = selector.repository_id",
		"scope scan":       "        WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'\n          AND fact.is_tombstone = FALSE\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mutated := strings.Replace(ListEvidenceQuery, marker, "", 1)
			if mutated == ListEvidenceQuery {
				t.Fatalf("mutation %q changed nothing: the marker text moved", name)
			}
			if _, err := deriveEvidenceSupportsQuery(mutated); err == nil {
				t.Fatalf("derivation accepted a legacy query that lost the %s marker", name)
			}
		})
	}
	if _, err := deriveEvidenceSupportsQuery(ListEvidenceQuery); err != nil {
		t.Fatalf("derivation of the shipped query failed: %v", err)
	}
}

// TestEvidenceSupportsReadReferencesEveryBind proves all nine arguments are
// referenced and that only the advisory facts, never an impact finding row, read
// the fact table.
func TestEvidenceSupportsReadReferencesEveryBind(t *testing.T) {
	t.Parallel()
	for _, token := range []string{"$1", "$2", "$3", "$5", "$6", "$7", "$8", "$9"} {
		if !strings.Contains(ListEvidenceFromSupportsQuery, token) {
			t.Errorf("supports evidence read never references %s", token)
		}
	}
	if !strings.Contains(ListEvidenceFromSupportsQuery, "FROM fact_records AS fact") {
		t.Error("the advisory fact legs no longer read fact_records")
	}
	if strings.Contains(ListEvidenceFromSupportsQuery, "reducer_supply_chain_impact_finding") {
		t.Error("the supports evidence read still names the impact finding fact kind")
	}
}
