// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
	"time"
)

func TestHungCommandTimesOutOnceAndTheLoopMovesOn(t *testing.T) {
	w := newWorld()
	w.seed(1, openPR(1, "aaaa1111"))
	w.seed(2, openPR(2, "bbbb2222"))
	w.hangPR[1] = true
	h := newHarness(t, []int{1, 2}, w)
	h.wt.cfg.GHTimeout = 30 * time.Millisecond
	// NewWatcher captured the timeouts; rebuild so the new value applies.
	h.wt = NewWatcher(h.wt.cfg, w.Exec, h.out, h.wt.now)

	start := time.Now()
	got := h.poll()
	if time.Since(start) > 5*time.Second {
		t.Fatalf("a hung gh call blocked the poll for %s", time.Since(start))
	}
	wantKinds(t, got, "ERROR", "BASELINE")
	e := parseEvent(t, got[0])
	if e.pr != "1" || !strings.Contains(e.kv["msg"], "timed out") || !strings.Contains(e.kv["msg"], "gh api graphql") {
		t.Fatalf("timeout must name the command: %s", got[0])
	}
	if again := h.poll(); len(again) != 0 {
		t.Fatalf("the same timeout must not repeat, got %v", again)
	}
	delete(w.hangPR, 1)
	wantKinds(t, h.poll(), "BASELINE")
}

func TestGitChildrenNeverPromptForCredentials(t *testing.T) {
	w := newWorld()
	f := openPR(1, "aaaa1111")
	f.mergeable, f.mergeState = "CONFLICTING", "DIRTY"
	w.seed(1, f, openPR(1, "bbbb2222"))
	w.mergeTree["ba5e0001..aaaa1111"] = mergeTreeResult{out: "t\nCHANGELOG.md\n", code: 1}
	w.localRef, w.localSHA = "drive/branch", "bbbb2222"
	h := newHarness(t, []int{1}, w)
	h.poll()
	h.poll()
	seen := 0
	for _, c := range w.calls {
		if c.Name != "git" {
			continue
		}
		seen++
		if !strings.Contains(strings.Join(c.Env, "\n"), "GIT_TERMINAL_PROMPT=0") {
			t.Fatalf("git %v must run with GIT_TERMINAL_PROMPT=0, env %v", c.Args, c.Env)
		}
	}
	if seen < 3 {
		t.Fatalf("expected fetch, merge-tree, and head-source git calls, saw %d", seen)
	}
}

func TestTestMergeUsesDoubleDashAndNeverMovesTrackingRefs(t *testing.T) {
	w := newWorld()
	f := openPR(1, "aaaa1111")
	f.mergeable, f.mergeState = "CONFLICTING", "DIRTY"
	w.seed(1, f)
	w.mergeTree["ba5e0001..aaaa1111"] = mergeTreeResult{out: "t\nCHANGELOG.md\n", code: 1}
	h := newHarness(t, []int{1}, w)
	h.poll()

	var fetch, merge []string
	for _, c := range w.calls {
		if c.Name == "git" && c.Args[0] == "fetch" {
			fetch = c.Args
		}
		if c.Name == "git" && c.Args[0] == "merge-tree" {
			merge = c.Args
		}
	}
	if got, want := strings.Join(fetch, " "), "fetch --quiet --no-write-fetch-head --no-tags --refmap= -- origin main refs/pull/1/head"; got != want {
		t.Fatalf("fetch args = %q, want %q", got, want)
	}
	if got, want := strings.Join(merge, " "), "merge-tree --write-tree --name-only --no-messages -- ba5e0001 aaaa1111"; got != want {
		t.Fatalf("merge-tree args = %q, want %q", got, want)
	}
}

func TestRefsFromPRDataAreValidatedBeforeGit(t *testing.T) {
	cases := map[string]func(*fx){
		"base ref that looks like an option": func(f *fx) { f.base = "--upload-pack=/tmp/x" },
		"base ref git rejects":               func(f *fx) { f.base = "bad-ref" },
		"head that is not a hex sha":         func(f *fx) { f.head = "--output=x" },
		"base sha that is not a hex sha":     func(f *fx) { f.baseOID = "-x" },
	}
	for name, mutate := range cases {
		w := newWorld()
		f := openPR(1, "aaaa1111")
		f.mergeable, f.mergeState = "CONFLICTING", "DIRTY"
		mutate(&f)
		w.seed(1, f)
		h := newHarness(t, []int{1}, w)
		got := h.poll()
		wantKinds(t, got, "BASELINE", "CONFLICT")
		e := parseEvent(t, got[1])
		if e.kv["test_merge"] != "unavailable" || !strings.Contains(e.kv["reason"], "refus") {
			t.Fatalf("%s: want an unavailable test merge that says it refused, got %s", name, got[1])
		}
		for _, c := range w.calls {
			if c.Name == "git" && (c.Args[0] == "fetch" || c.Args[0] == "merge-tree") {
				t.Fatalf("%s: git %v must not run on unvalidated PR data", name, c.Args)
			}
		}
	}
}

func TestHeadMovingDuringPaginationIsAReadError(t *testing.T) {
	w := newWorld()
	first := openPR(1, "aaaa1111")
	first.rollup, first.total, first.nextCursor = "SUCCESS", 101, "CUR1"
	second := first
	second.head, second.nextCursor = "bbbb2222", ""
	w.seed(1, first)
	w.pages["CUR1"] = second
	h := newHarness(t, []int{1}, w)

	got := h.poll()
	wantKinds(t, got, "ERROR")
	if !strings.Contains(got[0], "head moved") {
		t.Fatalf("a mixed read must say the head moved: %s", got[0])
	}
}

func TestFollowUpChecksPageUsesTheLeanQuery(t *testing.T) {
	w := newWorld()
	first := openPR(1, "aaaa1111")
	first.rollup, first.total, first.nextCursor = "SUCCESS", 101, "CUR1"
	second := first
	second.nextCursor = ""
	w.seed(1, first)
	w.pages["CUR1"] = second
	h := newHarness(t, []int{1}, w)
	h.poll()

	var queries []string
	for _, c := range w.calls {
		for _, a := range c.Args {
			if strings.HasPrefix(a, "query=") {
				queries = append(queries, a)
			}
		}
	}
	if len(queries) != 2 {
		t.Fatalf("expected a full read and one follow-up page, got %d queries", len(queries))
	}
	if strings.Contains(queries[1], "reviewThreads") || strings.Contains(queries[1], "comments(last") {
		t.Fatalf("the follow-up page must not re-read threads, reviews, or comments")
	}
}

func TestAgentRuleGuardHoldsEvenIfGeneratedListNamesOne(t *testing.T) {
	if got := classifyConflict([]string{"AGENTS.md"}, []string{"AGENTS.md"}); got != "count-choice-lines" {
		t.Fatalf("an agent-rule file is never a recipe file, got %q", got)
	}
}
