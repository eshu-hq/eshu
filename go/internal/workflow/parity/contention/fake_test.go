// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
	"github.com/eshu-hq/eshu/go/internal/workflow/parity"
)

// faults seeds one violation into the fake backend.
type faults struct {
	doubleClaimAt  int  // the n-th grant hands out an item that is still live
	regressTokenAt int  // the n-th re-grant reuses the previous token
	loseItemAt     int  // after the n-th grant one pending item row vanishes
	grantRealAt    int  // the n-th grant returns a non-synthetic item
	touchRealAt    int  // the n-th grant also edits a non-synthetic row
	acceptStale    bool // stale-token writes succeed
	terminalTwice  int  // the n-th grant's item also gets a phantom completed claim row
	dbDualAt       int  // the n-th grant's item also gets a second active claim row the client never sees
	staleBeatOK    bool // only stale heartbeats succeed (no state change the database detectors see)
}

type fakeItem struct {
	item      workflow.WorkItem
	token     int64
	claimID   string
	owner     string
	visibleAt time.Time
}

type fakeClaim struct {
	id, item, owner string
	token           int64
	status          string
	lease, pinned   time.Time
	finished        time.Time
}

// fakeLeg is an in-memory claim plane that implements parity.Backend and
// Observer over the same state, with optional seeded violations. Its mutex
// stands in for the database; the harness under test is the code exercised.
type fakeLeg struct {
	mu      sync.Mutex
	f       faults
	pin     bool
	budget  int
	quota   string
	items   map[string]*fakeItem
	claims  map[string]*fakeClaim
	order   []string
	grants  int
	quotaW  map[string]int
	others  map[string]int // non-synthetic rows by census key
	regrant map[string]bool
}

func newFakeLeg(f faults, pin bool, budget int) *fakeLeg {
	return &fakeLeg{
		f: f, pin: pin, budget: budget, quota: "package_registry",
		items: map[string]*fakeItem{}, claims: map[string]*fakeClaim{}, quotaW: map[string]int{},
		others:  map[string]int{"workflow_work_items|pending": 3, "collector_instances|true": 2},
		regrant: map[string]bool{},
	}
}

func (l *fakeLeg) Name() string { return "fake" }

func (l *fakeLeg) CreateRun(context.Context, workflow.Run) error { return nil }

func (l *fakeLeg) EnqueueWorkItems(_ context.Context, items []workflow.WorkItem) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, it := range items {
		if _, ok := l.items[it.WorkItemID]; !ok {
			l.items[it.WorkItemID] = &fakeItem{item: it}
			l.order = append(l.order, it.WorkItemID)
		}
	}
	return nil
}

func (l *fakeLeg) AdmitGuarded(_ context.Context, _ workflow.Run, items []workflow.WorkItem) (workflow.RunAdmission, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, it := range items {
		for _, cur := range l.items {
			// The real guard keys a target on scope and acceptance unit
			// (plus tenant fields the harness leaves empty).
			if cur.item.ScopeID == it.ScopeID && cur.item.AcceptanceUnitID == it.AcceptanceUnitID &&
				!terminalStatus(cur.item.Status) {
				return workflow.RunAdmission{}, nil
			}
		}
	}
	for _, it := range items {
		l.items[it.WorkItemID] = &fakeItem{item: it}
		l.order = append(l.order, it.WorkItemID)
	}
	return workflow.RunAdmission{EligibleTargets: len(items), InsertedWorkItems: len(items)}, nil
}

func (l *fakeLeg) ReconcileCollectorInstances(context.Context, time.Time, []workflow.DesiredCollectorInstance) error {
	return errors.New("fake: the harness must never reconcile collector instances")
}

func (l *fakeLeg) UpsertCompletenessStates(context.Context, []workflow.CompletenessState) error {
	return nil
}
func (l *fakeLeg) ReconcileWorkflowRuns(context.Context, time.Time) (int, error) { return 0, nil }
func (l *fakeLeg) ListCollectorInstances(context.Context) ([]workflow.CollectorInstance, error) {
	return nil, nil
}

func terminalStatus(s workflow.WorkItemStatus) bool {
	return s == workflow.WorkItemStatusCompleted || s == workflow.WorkItemStatusFailedTerminal
}

func (l *fakeLeg) ClaimNextEligible(_ context.Context, sel workflow.ClaimSelector, now time.Time, lease time.Duration) (workflow.WorkItem, workflow.Claim, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var pick *fakeItem
	if l.f.doubleClaimAt > 0 && l.grants+1 >= l.f.doubleClaimAt {
		for _, id := range l.order {
			if it := l.items[id]; it != nil && it.item.Status == workflow.WorkItemStatusClaimed &&
				it.item.CollectorKind == sel.CollectorKind {
				l.f.doubleClaimAt = 0 // hand out one live item, once
				pick = it
				break
			}
		}
	}
	for _, id := range l.order {
		if pick != nil {
			break
		}
		it := l.items[id]
		if it == nil || it.item.CollectorKind != sel.CollectorKind || it.item.CollectorInstanceID != sel.CollectorInstanceID {
			continue
		}
		if it.item.Status != workflow.WorkItemStatusPending || it.visibleAt.After(now) {
			continue
		}
		if string(sel.CollectorKind) == l.quota && l.budget > 0 && l.quotaW[l.bucket(it, now)] >= l.budget {
			continue
		}
		pick = it
		break
	}
	if pick == nil {
		return workflow.WorkItem{}, workflow.Claim{}, false, nil
	}
	l.grants++
	if string(sel.CollectorKind) == l.quota {
		l.quotaW[l.bucket(pick, now)]++
	}
	if l.regrant[pick.item.WorkItemID] && l.f.regressTokenAt > 0 && l.grants >= l.f.regressTokenAt {
		l.f.regressTokenAt = 0 // reuse the previous token once
	} else {
		pick.token++
	}
	l.regrant[pick.item.WorkItemID] = true
	pick.item.Status, pick.claimID, pick.owner = workflow.WorkItemStatusClaimed, sel.ClaimID, sel.OwnerID
	claim := &fakeClaim{
		id: sel.ClaimID, item: pick.item.WorkItemID, owner: sel.OwnerID, token: pick.token,
		status: "active", lease: now.Add(lease),
	}
	l.claims[claim.id] = claim
	item := pick.item
	if l.f.grantRealAt > 0 && l.grants == l.f.grantRealAt {
		item.WorkItemID = "real-item-1"
	}
	if l.f.terminalTwice > 0 && l.grants == l.f.terminalTwice {
		// Tokens below the item's current one and distinct, so only the
		// double-terminal detector can see this fault.
		for n, phantom := range []string{"a", "b"} {
			l.claims[sel.ClaimID+"-phantom-"+phantom] = &fakeClaim{
				id:   sel.ClaimID + "-phantom-" + phantom,
				item: pick.item.WorkItemID, owner: sel.OwnerID, token: -int64(n), status: "completed",
			}
		}
	}
	if l.f.dbDualAt > 0 && l.grants == l.f.dbDualAt {
		// Two server-side holders the client never hears about: visible
		// only to the database sampler until the reaper expires them.
		for n := range 2 {
			id := fmt.Sprintf("%s-server-twin-%d", sel.ClaimID, n)
			l.claims[id] = &fakeClaim{
				id: id, item: pick.item.WorkItemID, owner: "other-owner",
				token: -int64(n + 1), status: "active", lease: now.Add(lease),
			}
		}
	}
	if l.f.touchRealAt > 0 && l.grants == l.f.touchRealAt {
		l.others["workflow_work_items|claimed"]++
		l.others["workflow_work_items|pending"]--
	}
	if l.f.loseItemAt > 0 && l.grants == l.f.loseItemAt {
		for _, id := range l.order {
			if it := l.items[id]; it != nil && it.item.Status == workflow.WorkItemStatusPending {
				delete(l.items, id)
				break
			}
		}
	}
	return item, workflow.Claim{
		ClaimID: claim.id, WorkItemID: claim.item, FencingToken: claim.token,
		OwnerID: claim.owner, Status: workflow.ClaimStatusActive, LeaseExpiresAt: claim.lease,
	}, true, nil
}

func (l *fakeLeg) bucket(it *fakeItem, now time.Time) string {
	return it.item.ScopeID + "|" + now.Truncate(time.Hour).Format(time.RFC3339)
}

// live returns the item and claim for a fenced mutation, or rejects it.
func (l *fakeLeg) live(m workflow.ClaimMutation) (*fakeItem, *fakeClaim, error) {
	it, c := l.items[m.WorkItemID], l.claims[m.ClaimID]
	if it == nil || c == nil {
		return nil, nil, fmt.Errorf("fake: unknown claim: %w", workflow.ErrClaimRejected)
	}
	if c.status != "active" || it.claimID != m.ClaimID || it.token != m.FencingToken || c.owner != m.OwnerID {
		if l.f.acceptStale {
			return it, c, nil
		}
		return it, c, fmt.Errorf("fake: stale fence: %w", workflow.ErrClaimRejected)
	}
	return it, c, nil
}

func (l *fakeLeg) HeartbeatClaim(_ context.Context, m workflow.ClaimMutation) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, c, err := l.live(m)
	if err != nil {
		if l.f.staleBeatOK {
			return nil
		}
		return err
	}
	c.lease = m.ObservedAt.Add(m.LeaseDuration)
	return nil
}

func (l *fakeLeg) finish(m workflow.ClaimMutation, claimStatus string, itemStatus workflow.WorkItemStatus) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if c := l.claims[m.ClaimID]; c != nil && c.status == claimStatus && c.token == m.FencingToken && !l.f.acceptStale {
		return nil // identical replay acks (#9428)
	}
	it, c, err := l.live(m)
	if err != nil {
		return err
	}
	c.status, c.finished = claimStatus, m.ObservedAt
	it.item.Status, it.claimID, it.visibleAt = itemStatus, "", m.VisibleAt
	return nil
}

func (l *fakeLeg) CompleteClaim(_ context.Context, m workflow.ClaimMutation) error {
	return l.finish(m, "completed", workflow.WorkItemStatusCompleted)
}

func (l *fakeLeg) ReleaseClaim(_ context.Context, m workflow.ClaimMutation) error {
	return l.finish(m, "released", workflow.WorkItemStatusPending)
}

func (l *fakeLeg) FailClaimRetryable(_ context.Context, m workflow.ClaimMutation) error {
	return l.finish(m, "failed_retryable", workflow.WorkItemStatusPending)
}

func (l *fakeLeg) FailClaimTerminal(_ context.Context, m workflow.ClaimMutation) error {
	return l.finish(m, "failed_terminal", workflow.WorkItemStatusFailedTerminal)
}

func (l *fakeLeg) ReapExpiredClaims(_ context.Context, asOf time.Time, limit int, delay time.Duration) ([]workflow.Claim, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []workflow.Claim
	for _, c := range l.claims {
		if len(out) >= limit || c.status != "active" || c.lease.After(asOf) || c.pinned.After(asOf) {
			continue
		}
		c.status, c.finished = "expired", asOf
		if it := l.items[c.item]; it != nil && it.claimID == c.id {
			it.item.Status, it.claimID, it.visibleAt = workflow.WorkItemStatusPending, "", asOf.Add(delay)
		}
		out = append(out, workflow.Claim{ClaimID: c.id, WorkItemID: c.item, FencingToken: c.token})
	}
	return out, nil
}

func (l *fakeLeg) PinForCommit(_ context.Context, claimID string, token int64, _, _, _ string, deadline time.Time) (string, error) {
	if !l.pin {
		return "", parity.ErrPinNotSupported
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.claims[claimID]
	if c == nil || c.status != "active" || c.token != token {
		if l.f.acceptStale && c != nil {
			return "commit", nil
		}
		return "", fmt.Errorf("fake: stale pin: %w", workflow.ErrClaimRejected)
	}
	if deadline.After(c.pinned) {
		c.pinned = deadline
	}
	return "commit-" + claimID, nil
}

// Observer half.

func (l *fakeLeg) Snapshot(context.Context) (Snapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := Snapshot{ItemsByStatus: map[string]int{}}
	for _, it := range l.items {
		s.ItemsByStatus[string(it.item.Status)]++
		s.ItemTotal++
	}
	perItem := map[string]int{}
	for _, c := range l.claims {
		if c.status == "active" {
			s.ActiveClaims++
			perItem[c.item]++
		}
	}
	for item, n := range perItem {
		if n > 1 {
			s.DualLive = append(s.DualLive, item)
		}
	}
	return s, nil
}

func (l *fakeLeg) FencingViolations(context.Context) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	var bad []string
	for _, c := range l.claims {
		key := fmt.Sprintf("%s#%d", c.item, c.token)
		it := l.items[c.item]
		if seen[key] || (it != nil && it.token < c.token) {
			bad = append(bad, c.item)
		}
		seen[key] = true
	}
	sort.Strings(bad)
	return bad, nil
}

func (l *fakeLeg) DoubleTerminal(context.Context) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	perItem := map[string]int{}
	var twice []string
	for _, c := range l.claims {
		if c.status == "completed" || c.status == "failed_terminal" {
			perItem[c.item]++
			if perItem[c.item] == 2 {
				twice = append(twice, c.item)
			}
		}
	}
	sort.Strings(twice)
	return twice, nil
}

func (l *fakeLeg) Census(context.Context) (Census, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := Census{Prefixed: map[string]int{}, Other: map[string]int{}}
	for _, it := range l.items {
		c.Prefixed["workflow_work_items|"+string(it.item.Status)]++
	}
	for _, cl := range l.claims {
		c.Prefixed["workflow_claims|"+cl.status]++
	}
	for k, v := range l.others {
		c.Other[k] = v
	}
	return c, nil
}

func (l *fakeLeg) ClaimRows(_ context.Context, idList []string) (map[string]ClaimRow, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]ClaimRow{}
	for _, id := range idList {
		if c := l.claims[id]; c != nil {
			out[id] = ClaimRow{
				ClaimID: id, WorkItemID: c.item, FencingToken: c.token, Status: c.status,
				LeaseExpiresAt: c.lease, FinishedAt: c.finished, PinnedUntil: c.pinned,
			}
		}
	}
	return out, nil
}

func (l *fakeLeg) QuotaBuckets(context.Context) ([]QuotaBucket, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []QuotaBucket
	for key, n := range l.quotaW {
		scopeID, window, _ := strings.Cut(key, "|")
		start, _ := time.Parse(time.RFC3339, window)
		out = append(out, QuotaBucket{Component: l.quota, ScopeID: scopeID, WindowStart: start, Grants: n})
	}
	return out, nil
}

func (l *fakeLeg) NonTerminalItems(_ context.Context, limit int) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, id := range l.order {
		if it := l.items[id]; it != nil && !terminalStatus(it.item.Status) && len(out) < limit {
			out = append(out, id+":"+string(it.item.Status))
		}
	}
	return out, nil
}

func (l *fakeLeg) DeletePrefixed(context.Context, int) (map[string]int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	deleted := map[string]int{"workflow_claims": len(l.claims), "workflow_work_items": len(l.items)}
	l.claims, l.items, l.quotaW = map[string]*fakeClaim{}, map[string]*fakeItem{}, map[string]int{}
	return deleted, nil
}
