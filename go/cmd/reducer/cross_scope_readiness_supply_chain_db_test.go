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
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
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
	// Advisory-gated publication is not in use here: no public advisory
	// authority is registered, so the expected-authorities count is zero and
	// the generation list is empty. Checked before the generic fact_records
	// branch below, whose substring the list query's joins do not carry but
	// the target CTEs might. Zero registrations keep the pass on static
	// evidence alone; a registered set with a broken witness fail-closes in
	// production and is covered by the live admission tests.
	if strings.Contains(query, "FROM scanner_advisory_public_registrations") {
		if strings.HasPrefix(strings.TrimSpace(query), "SELECT count(*)") {
			return &crossScopeReadinessRows{rows: [][]any{{0}}}, nil
		}
		return &crossScopeReadinessRows{}, nil
	}
	// Authorized scan targets are absent too: no local-namespace target
	// exists, and the current-target and receipt censuses are empty, so the
	// targets load completes with nothing to publish.
	if strings.Contains(query, "FROM scanner_image_targets WHERE authorization_namespace") {
		return &crossScopeReadinessRows{rows: [][]any{{false}}}, nil
	}
	if strings.Contains(query, "FROM authorized ORDER BY scan_scope_id") {
		return &crossScopeReadinessRows{}, nil
	}
	// The protected-policy engagement probe (#9299). No role policy is
	// installed in this scenario, so protection reads disengaged and the
	// handler keeps the plain static load exactly as before protection
	// existed; the armed floor below is unaffected.
	if strings.Contains(query, "FROM eshu_security.role_policy_state") {
		return &crossScopeReadinessRows{}, nil
	}
	// Protected static branches (lane-expanded with the derived-scope and
	// advisory-kind exclusion) answer empty like their legacy twins: the
	// pass carries no advisory or derived facts in this scenario.
	if strings.Contains(query, "fact.fact_kind NOT IN ('vulnerability.cve'") {
		return &crossScopeReadinessRows{}, nil
	}
	// The shared cross-scope active-evidence read resolves nothing: every
	// #8880 branch statement, the repository scope resolution, and the
	// canonical identity page answer empty. Checked before the generic fact_records branch below, which the
	// branch FROM clauses also match. Combined with the not-quiescent probe
	// answer above, that is the #5709 condition: an empty producer join whose
	// producers have not published yet.
	if query == factimpact.ScopeResolutionSQL ||
		strings.Contains(query, "FROM container_image_identity_current_support_facts_for(") {
		return &crossScopeReadinessRows{}, nil
	}
	for _, branch := range factimpact.Branches() {
		if query == branch.SQL {
			return &crossScopeReadinessRows{}, nil
		}
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
