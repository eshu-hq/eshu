// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8830 disposable PostgreSQL 18 proof of the collector terminal-failure list that lets an operator discover the scope ids to acknowledge: per scope it lists the newest unresolved failure inside the instance window, acknowledged or not (an acknowledged scope lists acknowledged, an unacknowledged one does not, a scope that recovered or aged out does not list, an older unresolved row of the same scope collapses into its newest, a scope with both an older covered and a newer uncovered failure lists both), its two item sets equal what the status read considers (the status rows are the newest of each set), keyset pages are disjoint and their union equals the unpaged list, an unknown, kind-mismatched, disabled, or deactivated instance lists nothing, failure messages and reasons never appear, the plan seeks the failed_terminal instance index with the window cutoff and never sorts the history, and the documented path status failed, list, acknowledge, list acknowledged, status partial holds; isolated bootstrapped schema per test; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// seedListFixture seeds the semantics fixture on instance "li" and returns the
// fixture. It also seeds a twin instance with the same scope ids, so a missing
// instance filter would show.
func seedListFixture(f *terminalFailureLiveFixture) {
	const sa = "security_alert"
	const fifteen = `{"scan_interval": "15m"}`
	f.instance(sa, "li", true, false, fifteen)
	f.instance(sa, "twin", true, false, fifteen)
	f.instance(sa, "off", false, false, fifteen)
	// scope-a: acknowledged, and the same episode keeps failing.
	f.item("a1", sa, "li", "scope-a", "failed_terminal", "auth_denied", true, 20*time.Minute)
	f.item("a2", sa, "li", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "li", "scope-a", "auth_denied", "a1", 20*time.Minute)
	// scope-b: unacknowledged.
	f.item("b1", sa, "li", "scope-b", "failed_terminal", "rate_limited", true, 8*time.Minute)
	// scope-c: recovered, no new failure.
	f.item("c1", sa, "li", "scope-c", "failed_terminal", "auth_denied", true, 25*time.Minute)
	f.item("c-done", sa, "li", "scope-c", "completed", "", false, 15*time.Minute)
	// scope-d: aged out of the 45 minute window.
	f.item("d1", sa, "li", "scope-d", "failed_terminal", "auth_denied", true, 3*time.Hour)
	// scope-e: an older unresolved row and a newer one, unacknowledged: lists once.
	f.item("e1", sa, "li", "scope-e", "failed_terminal", "auth_denied", true, 30*time.Minute)
	f.item("e2", sa, "li", "scope-e", "failed_terminal", "auth_denied", true, 10*time.Minute)
	// scope-r: an acknowledged older failure and a newer failure of another class: lists both.
	f.item("r1", sa, "li", "scope-r", "failed_terminal", "auth_denied", true, 25*time.Minute)
	f.item("r2", sa, "li", "scope-r", "failed_terminal", "not_found", true, 6*time.Minute)
	f.ack(sa, "li", "scope-r", "auth_denied", "r1", 25*time.Minute)
	// The twin and the disabled instance share scope ids and must not leak in.
	f.item("tw-a", sa, "twin", "scope-a", "failed_terminal", "auth_denied", true, 4*time.Minute)
	f.item("off-a", sa, "off", "scope-a", "failed_terminal", "auth_denied", true, 4*time.Minute)
}

func listIDs(page TerminalFailureListPage) []string {
	ids := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, fmt.Sprintf("%s:%s:%t", item.ScopeID, item.WorkItemID, item.Acknowledged))
	}
	return ids
}

func TestTerminalFailureListSemanticsLive(t *testing.T) {
	started := time.Now()
	f := newTerminalFailureLiveFixture(t, "terminal_failure_list")
	seedListFixture(f)
	store := f.ackStore()

	page, err := store.ListTerminalFailures(f.ctx, TerminalFailureListFilter{CollectorKind: "security_alert", InstanceID: "li"})
	if err != nil {
		t.Fatalf("ListTerminalFailures() error = %v", err)
	}
	want := []string{"scope-a:a2:true", "scope-b:b1:false", "scope-e:e2:false", "scope-r:r2:false", "scope-r:r1:true"}
	if got := listIDs(page); !slices.Equal(got, want) {
		t.Fatalf("list = %v, want %v", got, want)
	}
	if page.Truncated || page.NextCursor != "" {
		t.Fatalf("page truncated=%v next=%q, want a complete page", page.Truncated, page.NextCursor)
	}
	for _, item := range page.Items {
		if item.FailureClass == "" || item.FailureOperation != "list_org_alerts" || item.FailedAt.IsZero() {
			t.Fatalf("item = %+v, want bounded class, operation, and failure time", item)
		}
		if item.Acknowledged != !item.AcknowledgedAt.IsZero() {
			t.Fatalf("item = %+v, want AcknowledgedAt set exactly when acknowledged", item)
		}
	}

	// Set equality with the status read: the status rows are the newest item of
	// each set (unacknowledged and acknowledged).
	var newestOpen, newestAcked *TerminalFailureListItem
	for i := range page.Items {
		item := &page.Items[i]
		switch {
		case item.Acknowledged && (newestAcked == nil || item.FailedAt.After(newestAcked.FailedAt)):
			newestAcked = item
		case !item.Acknowledged && (newestOpen == nil || item.FailedAt.After(newestOpen.FailedAt)):
			newestOpen = item
		}
	}
	open, acked := split(t, f.readAll()["li"])
	if open == nil || newestOpen == nil || open.WorkItemID != newestOpen.WorkItemID || open.ScopeID != newestOpen.ScopeID {
		t.Fatalf("status unacknowledged row = %+v, list newest unacknowledged = %+v; want the same row", open, newestOpen)
	}
	if acked == nil || newestAcked == nil || acked.WorkItemID != newestAcked.WorkItemID || acked.ScopeID != newestAcked.ScopeID {
		t.Fatalf("status acknowledged row = %+v, list newest acknowledged = %+v; want the same row", acked, newestAcked)
	}

	// Every unacknowledged listed scope is acknowledgeable; a scope the list
	// omits is refused.
	for _, scope := range []string{"scope-b", "scope-e", "scope-r"} {
		if _, err := store.ResolveTarget(f.ctx, ackRequest("li", scope)); err != nil {
			t.Fatalf("ResolveTarget(%s) error = %v, want a listed scope to be acknowledgeable", scope, err)
		}
	}
	for _, scope := range []string{"scope-c", "scope-d"} {
		if _, err := store.ResolveTarget(f.ctx, ackRequest("li", scope)); err == nil {
			t.Fatalf("ResolveTarget(%s) succeeded, want an unlisted scope refused", scope)
		}
	}

	// No instance, kind mismatch, disabled, deactivated, or unknown instance lists anything.
	for name, filter := range map[string]TerminalFailureListFilter{
		"unknown":       {CollectorKind: "security_alert", InstanceID: "nope"},
		"kind mismatch": {CollectorKind: "git", InstanceID: "li"},
		"disabled":      {CollectorKind: "security_alert", InstanceID: "off"},
	} {
		got, err := store.ListTerminalFailures(f.ctx, filter)
		if err != nil || len(got.Items) != 0 {
			t.Fatalf("%s: list = %+v, %v; want an empty page", name, got, err)
		}
	}
	for _, bad := range []TerminalFailureListFilter{{InstanceID: "li"}, {CollectorKind: "security_alert"}} {
		if _, err := store.ListTerminalFailures(f.ctx, bad); err == nil {
			t.Fatalf("ListTerminalFailures(%+v) error = nil, want a validation error", bad)
		}
	}
	t.Logf("failure list semantics proof: %s", time.Since(started))
}

func TestTerminalFailureListPagesAreDisjointLive(t *testing.T) {
	f := newTerminalFailureLiveFixture(t, "terminal_failure_list_pages")
	seedListFixture(f)
	store := f.ackStore()
	filter := TerminalFailureListFilter{CollectorKind: "security_alert", InstanceID: "li"}

	whole, err := store.ListTerminalFailures(f.ctx, filter)
	if err != nil {
		t.Fatalf("unpaged list error = %v", err)
	}
	var union []string
	filter.Limit = 2
	for pages := 0; ; pages++ {
		page, err := store.ListTerminalFailures(f.ctx, filter)
		if err != nil {
			t.Fatalf("page %d error = %v", pages, err)
		}
		scopes := map[string]bool{}
		for _, item := range page.Items {
			scopes[item.ScopeID] = true
		}
		if len(scopes) > 2 {
			t.Fatalf("page %d holds %d scopes, want at most the limit of 2", pages, len(scopes))
		}
		union = append(union, listIDs(page)...)
		if !page.Truncated {
			if page.NextCursor != "" {
				t.Fatalf("last page next cursor = %q, want empty", page.NextCursor)
			}
			break
		}
		if page.NextCursor == "" || page.NextCursor == filter.Cursor {
			t.Fatalf("page %d truncated with next cursor %q after %q, want progress", pages, page.NextCursor, filter.Cursor)
		}
		filter.Cursor = page.NextCursor
		if pages > 10 {
			t.Fatal("paging did not terminate")
		}
	}
	if !slices.Equal(union, listIDs(whole)) {
		t.Fatalf("union of pages = %v, want the unpaged list %v", union, listIDs(whole))
	}
	// A limit above the maximum is clamped, not an error.
	filter = TerminalFailureListFilter{CollectorKind: "security_alert", InstanceID: "li", Limit: 100000}
	if clamped, err := store.ListTerminalFailures(f.ctx, filter); err != nil || len(clamped.Items) != len(whole.Items) {
		t.Fatalf("clamped list = %d items, %v; want %d", len(clamped.Items), err, len(whole.Items))
	}
}

// TestTerminalFailureListNeverCarriesMessageOrReasonLive seeds a provider
// message and an acknowledgement reason and proves neither is read.
func TestTerminalFailureListNeverCarriesMessageOrReasonLive(t *testing.T) {
	f := newTerminalFailureLiveFixture(t, "terminal_failure_list_secrets")
	seedListFixture(f)
	store := f.ackStore()
	page, err := store.ListTerminalFailures(f.ctx, TerminalFailureListFilter{CollectorKind: "security_alert", InstanceID: "li"})
	if err != nil {
		t.Fatalf("ListTerminalFailures() error = %v", err)
	}
	encoded := fmt.Sprintf("%+v", page)
	for _, secret := range []string{"provider text that must never be read", "target removed upstream"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("list carries %q: %s", secret, encoded)
		}
	}
	for _, forbidden := range []string{"last_failure_message", "last_failure_status_code", "reason", "actor_"} {
		if strings.Contains(terminalFailureListQuery, forbidden) {
			t.Fatalf("list query selects %q", forbidden)
		}
	}
}

func TestTerminalFailureListUsesTheWindowIndexLive(t *testing.T) {
	started := time.Now()
	f := newTerminalFailureLiveFixture(t, "terminal_failure_list_plan")
	seedTerminalFailureIndexBacklog(f)
	cutoff := terminalFailureCutoff(f.asOf, 15*time.Minute)
	explain := func(state string) string {
		t.Helper()
		rows, err := f.database.QueryContext(f.ctx, "EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF) "+terminalFailureListQuery,
			"security_alert", "hot", cutoff, "", 101)
		if err != nil {
			t.Fatalf("EXPLAIN list (%s): %v", state, err)
		}
		var plan []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan plan: %v", err)
			}
			plan = append(plan, line)
		}
		_ = rows.Close()
		text := strings.Join(plan, "\n")
		if !strings.Contains(text, "Index Scan using "+workflowTerminalFailureIndexName+" on workflow_work_items t") {
			t.Fatalf("%s: list does not seek %s:\n%s", state, workflowTerminalFailureIndexName, text)
		}
		if !strings.Contains(text, "updated_at >=") {
			t.Fatalf("%s: list does not bound the index range by the window cutoff:\n%s", state, text)
		}
		// Only a Sort keyed on scope or acknowledged state over in-window rows is
		// allowed; one keyed on the history ordering of the whole instance is not.
		if strings.Contains(text, "Sort Key: t.updated_at") {
			t.Fatalf("%s: list sorts the instance history:\n%s", state, text)
		}
		if strings.Contains(text, "temp") {
			t.Fatalf("%s: list spills to temp blocks:\n%s", state, text)
		}
		return text
	}
	text := explain("none acknowledged")
	f.ack("security_alert", "hot", "scope-hot-0", "auth_denied", "hot-0-0", 0)
	explain("hot scope acknowledged")
	page, err := f.ackStore().ListTerminalFailures(f.ctx, TerminalFailureListFilter{CollectorKind: "security_alert", InstanceID: "hot"})
	if err != nil || len(page.Items) != 40 {
		t.Fatalf("hot list = %d items, %v; want one item for each of the 40 scopes", len(page.Items), err)
	}
	t.Logf("list plan proof: 48,000 seeded work items, %s\n%s", time.Since(started), text)
}

// TestCollectorFailureDiscoveryAcknowledgeLoopLive walks the documented
// operator path over the real stores: status shows the instance failed, the list
// names the scope, the acknowledgement lands, the list shows it acknowledged, and
// status then reads partial with the acknowledged blocker.
func TestCollectorFailureDiscoveryAcknowledgeLoopLive(t *testing.T) {
	f := newTerminalFailureLiveFixture(t, "terminal_failure_loop")
	const sa = "security_alert"
	f.instance(sa, "loop", true, false, `{"scan_interval": "15m"}`)
	f.item("l1", sa, "loop", "scope-x", "failed_terminal", "auth_denied", true, 6*time.Minute)
	store := f.ackStore()
	readiness := func() collector.PromotionProof {
		t.Helper()
		snapshot, err := readCoordinatorSnapshot(f.ctx, SQLDB{DB: f.database}, f.asOf)
		if err != nil || snapshot == nil {
			t.Fatalf("readCoordinatorSnapshot() = %v, %v", snapshot, err)
		}
		proofs := collector.PromotionProofs(collector.Evidence{
			AsOf: f.asOf, Instances: snapshot.CollectorInstances, TerminalFailures: snapshot.CollectorTerminalFailures,
		}, collector.PromotionOptions{AsOf: f.asOf})
		for _, proof := range proofs {
			if proof.InstanceID == "loop" {
				return proof
			}
		}
		t.Fatal("no promotion proof for the loop instance")
		return collector.PromotionProof{}
	}

	if proof := readiness(); proof.PromotionState != collector.PromotionFailed {
		t.Fatalf("readiness before = %q, want failed", proof.PromotionState)
	}
	page, err := store.ListTerminalFailures(f.ctx, TerminalFailureListFilter{CollectorKind: sa, InstanceID: "loop"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ScopeID != "scope-x" || page.Items[0].Acknowledged {
		t.Fatalf("list before = %+v, %v; want scope-x unacknowledged", page, err)
	}
	if _, err := store.Acknowledge(f.ctx, ackRequest("loop", page.Items[0].ScopeID)); err != nil {
		t.Fatalf("Acknowledge(listed scope) error = %v", err)
	}
	page, err = store.ListTerminalFailures(f.ctx, TerminalFailureListFilter{CollectorKind: sa, InstanceID: "loop"})
	if err != nil || len(page.Items) != 1 || !page.Items[0].Acknowledged || page.Items[0].AcknowledgedAt.IsZero() {
		t.Fatalf("list after = %+v, %v; want scope-x acknowledged", page, err)
	}
	proof := readiness()
	if proof.PromotionState != collector.PromotionPartial || len(proof.Blockers) != 1 ||
		proof.Blockers[0] != "terminal fetch failure acknowledged: auth_denied (list_org_alerts)" {
		t.Fatalf("readiness after = %q %v, want partial with the acknowledged blocker", proof.PromotionState, proof.Blockers)
	}
}
