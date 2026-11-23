// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"

	"github.com/eshu-hq/eshu-sdk/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// reconciliationFactRow is one fact_records row as the reconciliation loader
// reads it (the full envelope select list).
func reconciliationFactRow(factID, kind, payloadJSON string) []any {
	return supplyChainImpactFindingFactRowForTest(factID, kind, payloadJSON)
}

// TestListActiveSecurityAlertReconciliationFactsReadsImpactFromSupports pins
// #11198: the impact finding rows come from the per-support table in the same
// statement as the alert and consumption facts (one UNION ALL, one keyset), and
// the loader returns the rows in the order the statement returned them, which is
// the database's ordering of the fact ids.
func TestListActiveSecurityAlertReconciliationFactsReadsImpactFromSupports(t *testing.T) {
	t.Parallel()

	impactPayload := `{"repository_id":"acme/app","package_id":"pkg:npm/x","cve_id":"CVE-1","impact_status":"affected_exact"}`
	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{true}}},  // supports readiness
		{rows: [][]any{{false}}}, // keyed sidecar not ready: the legacy fact arm
		{rows: [][]any{
			reconciliationFactRow("fact-a", "security_alert.repository_alert", `{"repository_id":"acme/app"}`),
			reconciliationFactRow("fact-c", "reducer_supply_chain_impact_finding", impactPayload),
			reconciliationFactRow("fact-m", "reducer_package_consumption_correlation", `{"repository_id":"acme/app","package_id":"pkg:npm/x"}`),
		}},
	}}
	store := NewFactStore(database)

	got, err := store.ListActiveSecurityAlertReconciliationFacts(context.Background(),
		securityalert.SecurityAlertReconciliationFactFilter{RepositoryIDs: []string{"acme/app"}})
	if err != nil {
		t.Fatalf("ListActiveSecurityAlertReconciliationFacts() error = %v, want nil", err)
	}
	var order []string
	for _, envelope := range got {
		order = append(order, envelope.FactID)
	}
	if want := "fact-a,fact-c,fact-m"; strings.Join(order, ",") != want {
		t.Fatalf("fact order = %v, want %s (the statement's order)", order, want)
	}
	if got[1].FactKind != facts.ReducerSupplyChainImpactFindingFactKind || got[1].Payload["impact_status"] != "affected_exact" {
		t.Fatalf("impact envelope = %+v, want the impact kind with the verbatim payload", got[1])
	}
	// One data statement, the merged one: no separate supports statement.
	var data []string
	for _, q := range database.queries {
		if strings.Contains(q.query, "UNION ALL") {
			data = append(data, q.query)
		}
	}
	if len(data) != 1 || data[0] != listActiveSecurityAlertReconciliationFactsQuery {
		t.Fatalf("data statements = %d, want exactly the merged legacy statement", len(data))
	}
}

// TestListActiveSecurityAlertReconciliationFactsRefusesWhenSupportsNotReady
// pins the fail-closed contract: a not-ready supports table returns the
// retryable refusal and reads nothing else.
func TestListActiveSecurityAlertReconciliationFactsRefusesWhenSupportsNotReady(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{{false}}}}}
	store := NewFactStore(database)

	_, err := store.ListActiveSecurityAlertReconciliationFacts(context.Background(),
		securityalert.SecurityAlertReconciliationFactFilter{RepositoryIDs: []string{"acme/app"}})
	var notReady *supplychaincore.SupplyChainImpactSupportsNotReadyError
	if !errors.As(err, &notReady) || !notReady.Retryable() {
		t.Fatalf("error = %v, want the retryable *supplychaincore.SupplyChainImpactSupportsNotReadyError", err)
	}
	if len(database.queries) != 1 {
		t.Fatalf("queries = %d, want only the readiness probe", len(database.queries))
	}
}

// supplyChainImpactFindingFactRowForTest is a fact_records row in the loader's
// full select-list order.
func supplyChainImpactFindingFactRowForTest(factID, kind, payloadJSON string) []any {
	return []any{
		factID, "scope-1", "gen-1", kind, "stable:" + factID, "1.0.0", "reducer", int64(0),
		"exact", "reducer", "key:" + factID, "", "", fixedFactObservedAtForTest(), false, []byte(payloadJSON),
	}
}

func fixedFactObservedAtForTest() time.Time {
	return time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
}

// TestSupplyChainImpactFindingPayloadCarriesNoIDArrays pins why the supports
// loader drops the two legacy payload array arms (payload->'cve_ids' and
// 'ghsa_ids'): the typed finding payload the writer encodes has no such field,
// so those arms never matched a finding fact. A new field with either name
// fails here, and the supports query must regain the arm.
func TestSupplyChainImpactFindingPayloadCarriesNoIDArrays(t *testing.T) {
	t.Parallel()

	encoded, err := factschema.EncodeReducerSupplyChainImpactFinding(reducerderivedv1.SupplyChainImpactFinding{})
	if err != nil {
		t.Fatalf("EncodeReducerSupplyChainImpactFinding() error = %v, want nil", err)
	}
	typ := reflect.TypeOf(reducerderivedv1.SupplyChainImpactFinding{})
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if tag == "cve_ids" || tag == "ghsa_ids" {
			t.Fatalf("finding payload field %q exists: the supports reconciliation query must reproduce the legacy %s arm", tag, tag)
		}
	}
	for _, key := range []string{"cve_ids", "ghsa_ids"} {
		if _, present := encoded[key]; present {
			t.Fatalf("encoded empty finding carries %q", key)
		}
	}
}

// TestSupplyChainImpactSupportsPackageIndexMatchesTheArm pins that the package
// arm of the reconciliation read carries the exact predicate of the partial
// package_id btree, so the planner can use the index for it (#10880 PR-B2c).
func TestSupplyChainImpactSupportsPackageIndexMatchesTheArm(t *testing.T) {
	t.Parallel()

	want := "(support." + supplyChainImpactSupportsPackageIndexPredicate + " AND support.package_id = ANY($2::text[]))"
	for name, statement := range map[string]string{
		"legacy": listActiveSecurityAlertReconciliationFactsQuery,
		"keyed":  listActiveSecurityAlertReconciliationFactsKeyedQuery,
	} {
		if !strings.Contains(statement, want) {
			t.Errorf("%s statement missing the guarded package arm %q", name, want)
		}
	}
	if supplyChainImpactSupportsPackageIndexPredicate != "package_id <> ''" {
		t.Errorf("package index predicate = %q, want %q", supplyChainImpactSupportsPackageIndexPredicate, "package_id <> ''")
	}
}
