// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Observer half of fakeLeg.

func (l *fakeLeg) Snapshot(context.Context) (Snapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.snapshotFailures > 0 {
		l.snapshotFailures--
		return Snapshot{}, fmt.Errorf("fake: snapshot unavailable")
	}
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
	for id, it := range l.items {
		if strings.HasPrefix(id, strings.TrimSuffix(itemFamily, "%")) {
			c.Prefixed["workflow_work_items|"+string(it.item.Status)]++
		} else {
			c.Other["workflow_work_items|"+string(it.item.Status)]++
		}
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

func (l *fakeLeg) AnalyzedAt(context.Context) (time.Time, error) {
	if l.f.loseInStats {
		l.mu.Lock()
		l.f.loseInStats = false
		l.dropOneItemLocked()
		l.mu.Unlock()
	}
	if l.coldStats {
		return time.Time{}, nil
	}
	return time.Now(), nil
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

func (l *fakeLeg) DeletePrefixed(_ context.Context, seed int64, _ int) (map[string]int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	mine := strings.TrimSuffix(seedItemPattern(seed), "%")
	deleted := map[string]int{}
	for id, c := range l.claims {
		if strings.HasPrefix(c.item, mine) {
			delete(l.claims, id)
			deleted["workflow_claims"]++
		}
	}
	for id := range l.items {
		if strings.HasPrefix(id, mine) {
			delete(l.items, id)
			deleted["workflow_work_items"]++
		}
	}
	l.quotaW = map[string]int{}
	return deleted, nil
}
