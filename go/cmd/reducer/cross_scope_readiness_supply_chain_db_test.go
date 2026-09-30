// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// supplyChainReadinessWiringDB answers the queries one supply_chain_impact pass
// makes before the floor fires: the base scope-fact read, the shared
// active-evidence read, and the producer-quiescence probe.
type supplyChainReadinessWiringDB struct {
	fakeReducerDB
	probedProducerQuiescence bool
	readReadinessWait        bool
	// issuedFencingToken records that the pass asked the wired issuer for its
	// fencing token (#7142) before it loaded any evidence.
	issuedFencingToken bool
}

func (f *supplyChainReadinessWiringDB) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	// The fencing-token issuer (#7142): Handle draws the token from the
	// supply_chain_impact_fencing_token_seq sequence before the evidence load.
	if strings.Contains(query, "supply_chain_impact_fencing_token_seq") {
		f.issuedFencingToken = true
		return &crossScopeReadinessRows{rows: [][]any{{int64(1)}}}, nil
	}
	// The producer-scope quiescence probe. Identified by the projector-drain
	// fence, which no other reducer query carries. supply_chain_impact declares
	// two producer domains, so this answers for both collector kinds: each is
	// registered and neither is quiescent.
	if strings.Contains(query, "FROM fact_work_items AS projector_work") {
		f.probedProducerQuiescence = true
		return &crossScopeReadinessRows{rows: [][]any{
			{"oci_registry:ghcr.io/acme", false},
		}}, nil
	}
	// The shared cross-scope active-evidence read resolves nothing. Checked
	// before the generic fact_records branch below, which its FROM clause also
	// matches. Combined with the not-quiescent probe answer above, that is the
	// #5709 condition: an empty producer join whose producers have not
	// published yet.
	if strings.Contains(query, "legacy_facts") {
		return &crossScopeReadinessRows{}, nil
	}
	if strings.Contains(query, "FROM fact_records") {
		return &crossScopeReadinessRows{rows: [][]any{
			supplyChainReadinessConsumptionFactRow("scope-123", "generation-456"),
		}}, nil
	}
	// The readiness-wait ledger read (#6814). No wait stands, so the deferral
	// anchors at the claimed row exactly as before the ledger existed.
	if strings.Contains(query, "FROM reducer_readiness_waits") {
		f.readReadinessWait = true
		return &crossScopeReadinessRows{}, nil
	}
	return f.fakeReducerDB.QueryContext(ctx, query, args...)
}

// BeginReadOnlyRepeatableRead satisfies the snapshot seam shared reducer wiring
// requires. The transaction routes straight back to this fake.
func (f *supplyChainReadinessWiringDB) BeginReadOnlyRepeatableRead(
	context.Context,
) (db.Transaction, error) {
	return supplyChainReadinessTx{database: f}, nil
}

// Begin satisfies db.Beginner so the transaction-backed supply-chain impact
// writer (#6831) is wired and the domain registers. The transaction routes
// straight back to this fake.
func (f *supplyChainReadinessWiringDB) Begin(context.Context) (db.Transaction, error) {
	return supplyChainReadinessTx{database: f}, nil
}

// supplyChainReadinessTx is a pass-through transaction over the fake database.
type supplyChainReadinessTx struct {
	database *supplyChainReadinessWiringDB
}

func (t supplyChainReadinessTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return t.database.ExecContext(ctx, query, args...)
}

func (t supplyChainReadinessTx) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	return t.database.QueryContext(ctx, query, args...)
}

func (supplyChainReadinessTx) Commit() error { return nil }

func (supplyChainReadinessTx) Rollback() error { return nil }

// supplyChainReadinessConsumptionFactRow builds one fact_records row in
// listFactsQuery column order for a package-consumption correlation carrying a
// repository ID. That repository ID is what puts a producer-reachable dimension
// in the pass's active-evidence filter and so arms the floor.
func supplyChainReadinessConsumptionFactRow(scopeID, generationID string) []any {
	payload, err := json.Marshal(map[string]any{
		"package_id":    "pkg:npm/example",
		"repository_id": "repo://example/api",
		"outcome":       "exact",
	})
	if err != nil {
		panic(err)
	}
	return []any{
		"fact-consumption-1",
		scopeID,
		generationID,
		"reducer_package_consumption_correlation",
		"reducer_package_consumption_correlation:pkg:npm/example:repo://example/api",
		"",
		"package_registry",
		int64(1),
		facts.SourceConfidenceReported,
		"eshu_reducer",
		"reducer_package_consumption_correlation:pkg:npm/example",
		"",
		"",
		time.Now().UTC(),
		false,
		payload,
	}
}
