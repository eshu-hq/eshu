// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
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

// supportsPageRowForTest is one supports page row in the page statement's select
// list: fact id, scope, generation, source confidence, is_active, payload (nil
// when the row is not active).
func supportsPageRowForTest(factID string, active bool, payloadJSON string) []any {
	var payload []byte
	if active {
		payload = []byte(payloadJSON)
	}
	return []any{factID, "scope-1", "gen-1", "exact", active, payload}
}

// TestListActiveSecurityAlertReconciliationFactsReadsImpactFromSupports pins
// #11198 and the hot-key ruling: the alert and consumption facts come from their
// own statement and cursor, the impact findings from the supports page statement
// on its own raw cursor, and the loader returns the fact rows then the supports
// rows, each in the statement's order. An inactive raw row is read and dropped.
func TestListActiveSecurityAlertReconciliationFactsReadsImpactFromSupports(t *testing.T) {
	t.Parallel()

	impactPayload := `{"repository_id":"acme/app","package_id":"pkg:npm/x","cve_id":"CVE-1","impact_status":"affected_exact"}`
	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{true}}},  // supports readiness
		{rows: [][]any{{false}}}, // keyed sidecar not ready: the legacy fact statement
		{rows: [][]any{
			reconciliationFactRow("fact-a", "security_alert.repository_alert", `{"repository_id":"acme/app"}`),
			reconciliationFactRow("fact-m", "reducer_package_consumption_correlation", `{"repository_id":"acme/app","package_id":"pkg:npm/x"}`),
		}},
		{rows: [][]any{
			supportsPageRowForTest("fact-b", false, ""),
			supportsPageRowForTest("fact-c", true, impactPayload),
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
	if want := "fact-a,fact-m,fact-c"; strings.Join(order, ",") != want {
		t.Fatalf("fact order = %v, want %s (fact rows, then the active supports rows)", order, want)
	}
	if got[2].FactKind != facts.ReducerSupplyChainImpactFindingFactKind || got[2].Payload["impact_status"] != "affected_exact" {
		t.Fatalf("impact envelope = %+v, want the impact kind with the verbatim payload", got[2])
	}
	// Two data statements, one per stream: the legacy fact statement and the
	// first-page supports statement.
	var data []string
	for _, q := range database.queries {
		if strings.Contains(q.query, "fact_records") || strings.Contains(q.query, "supply_chain_impact_finding_supports AS s") {
			data = append(data, q.query)
		}
	}
	supportsFirst := supplyChainImpactSupportsPageQuery(securityAlertReconciliationSupportsArms, 0)
	if len(data) != 2 || data[0] != listActiveSecurityAlertReconciliationFactsQuery || data[1] != supportsFirst {
		t.Fatalf("data statements = %d, want the legacy fact statement then the first supports page", len(data))
	}
}

// TestSecurityAlertReconciliationSupportsDrainsOnTheRawCursor pins the reader
// contract of the hot-key ruling: the cursor advances on the last RAW fact id,
// and the drain stops only on a raw page shorter than the page size. A full raw
// page of inactive rows (superseded generations) must not end the drain, and a
// page of active rows below the page size must not either when the raw page is
// full.
func TestSecurityAlertReconciliationSupportsDrainsOnTheRawCursor(t *testing.T) {
	t.Parallel()

	var inactive [][]any
	for i := 0; i < listFactsByKindPageSize; i++ {
		inactive = append(inactive, supportsPageRowForTest(fmt.Sprintf("fact-%04d", i), false, ""))
	}
	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{true}}},
		{rows: [][]any{{false}}},
		{rows: nil}, // the fact stream: empty
		{rows: inactive},
		{rows: [][]any{supportsPageRowForTest("fact-9999", true, `{"repository_id":"acme/app"}`)}},
	}}
	store := NewFactStore(database)

	got, err := store.ListActiveSecurityAlertReconciliationFacts(context.Background(),
		securityalert.SecurityAlertReconciliationFactFilter{RepositoryIDs: []string{"acme/app"}})
	if err != nil {
		t.Fatalf("ListActiveSecurityAlertReconciliationFacts() error = %v, want nil", err)
	}
	if len(got) != 1 || got[0].FactID != "fact-9999" {
		t.Fatalf("loaded = %+v, want the one active row of the second page", got)
	}
	var pages []fakeQueryCall
	for _, q := range database.queries {
		if strings.Contains(q.query, "supply_chain_impact_finding_supports AS s") {
			pages = append(pages, q)
		}
	}
	if len(pages) != 2 {
		t.Fatalf("supports pages = %d, want 2: a full raw page of inactive rows must continue the drain", len(pages))
	}
	if last := pages[1].args[len(pages[1].args)-1]; last != fmt.Sprintf("fact-%04d", listFactsByKindPageSize-1) {
		t.Fatalf("second page cursor = %v, want the last RAW fact id of page 1", last)
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

	want := "WHERE s.package_id = k.v AND s." + supplyChainImpactSupportsPackageIndexPredicate
	for name, statement := range map[string]string{
		"first page": supplyChainImpactSupportsPageQuery(securityAlertReconciliationSupportsArms, 0),
		"next page":  supplyChainImpactSupportsPageQuery(securityAlertReconciliationSupportsArms, 5),
	} {
		if !strings.Contains(statement, want) {
			t.Errorf("%s statement missing the guarded package arm %q", name, want)
		}
	}
	if supplyChainImpactSupportsPackageIndexPredicate != "package_id <> ''" {
		t.Errorf("package index predicate = %q, want %q", supplyChainImpactSupportsPackageIndexPredicate, "package_id <> ''")
	}
}
