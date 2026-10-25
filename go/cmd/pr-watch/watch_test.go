// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const jobDetails = "https://github.com/eshu-hq/eshu/actions/runs/500/job/777"

func TestFirstPollReportsBaselineAndExistingUnresolvedThreads(t *testing.T) {
	w := newWorld()
	f := openPR(1, "aaaa1111")
	f.threads = []thr{
		{id: "T1", path: "go/x.go", line: 12, author: "codex", body: "nil deref here"},
		{id: "T2", resolved: true, path: "go/y.go", line: 3, author: "codex", body: "done already"},
	}
	f.reviews = []note{{id: "R1", author: "codex", state: "COMMENTED", body: "see threads"}}
	f.comments = []note{{id: "C1", author: "alice", body: "older chatter"}}
	w.seed(1, f)
	h := newHarness(t, []int{1}, w)

	lines := h.poll()
	wantKinds(t, lines, "BASELINE", "NEW-THREAD")
	base := parseEvent(t, lines[0])
	if base.kv["head"] != "aaaa1111" || base.kv["unresolved"] != "1" || base.kv["reviews"] != "1" || base.kv["comments"] != "1" {
		t.Fatalf("baseline fields wrong: %s", lines[0])
	}
	th := parseEvent(t, lines[1])
	if th.kv["thread"] != "T1" || th.kv["author"] != "codex" || th.kv["at"] != "go/x.go:12" || th.kv["body"] != "nil deref here" {
		t.Fatalf("thread fields wrong: %s", lines[1])
	}
}

func TestUnchangedStateEmitsNothing(t *testing.T) {
	w := newWorld()
	f := openPR(1, "aaaa1111")
	f.rollup, f.total = "FAILURE", 2
	f.checks = []ck{
		{run: 11, name: "go-core", status: "COMPLETED", result: "FAILURE", url: jobDetails},
		{run: 12, name: "lint", status: "COMPLETED", result: "SUCCESS"},
	}
	f.mergeable, f.mergeState = "CONFLICTING", "DIRTY"
	f.threads = []thr{{id: "T1", path: "a.go", line: 1, author: "codex", body: "x"}}
	w.seed(1, f)
	w.logs["777"] = "step output\n##[error]Process completed with exit code 1.\n"
	w.mergeTree["ba5e0001..aaaa1111"] = mergeTreeResult{out: "treeoid\nCHANGELOG.md\n", code: 1}
	h := newHarness(t, []int{1}, w)

	first := h.poll()
	wantKinds(t, first, "BASELINE", "NEW-THREAD", "CI-FAIL", "CONFLICT")
	for i := 0; i < 5; i++ {
		if got := h.poll(); got != nil {
			t.Fatalf("poll %d on unchanged state printed %v", i+2, got)
		}
	}
}

func TestNewThreadReviewAndCommentAreReportedOnceFromAnyAuthor(t *testing.T) {
	w := newWorld()
	p1 := openPR(1, "aaaa1111")
	p2 := p1
	p2.comments = []note{
		{id: "C1", author: "alice", body: "please add a test"},
		{id: "C2", author: "cloudflare-workers-and-pages", body: "deployed preview"},
		{id: "C3", author: "github-actions", body: "welcome"},
		{id: "C4", author: "linuxdynasty", body: "our own comment counts"},
	}
	p2.reviews = []note{
		{id: "R1", author: "codex", state: "COMMENTED", body: "findings inline"},
		{id: "R2", author: "linuxdynasty", state: "APPROVED", body: "ok"},
		{id: "R3", author: "linuxdynasty", state: "PENDING", body: "unsent draft"},
	}
	p2.threads = []thr{{id: "T1", path: "a.go", line: 4, author: "cursor", body: "race"}}
	w.seed(1, p1, p2, p2)
	h := newHarness(t, []int{1}, w)

	wantKinds(t, h.poll(), "BASELINE")
	got := h.poll()
	wantKinds(t, got, "NEW-THREAD", "NEW-REVIEW", "NEW-REVIEW", "NEW-COMMENT", "NEW-COMMENT")
	if a := parseEvent(t, got[3]).kv["author"]; a != "alice" {
		t.Fatalf("first comment author = %q", a)
	}
	if a := parseEvent(t, got[4]).kv["author"]; a != "linuxdynasty" {
		t.Fatalf("own comment must be reported, got author %q", a)
	}
	if h.poll() != nil {
		t.Fatalf("repeat poll must stay silent")
	}
}

func TestResolvedThenReopenedThreadIsReportedAgain(t *testing.T) {
	w := newWorld()
	open := openPR(1, "aaaa1111")
	open.threads = []thr{{id: "T1", path: "a.go", line: 1, author: "codex", body: "x"}}
	resolved := open
	resolved.threads = []thr{{id: "T1", resolved: true, path: "a.go", line: 1, author: "codex", body: "x"}}
	w.seed(1, open, resolved, open, open)
	h := newHarness(t, []int{1}, w)

	wantKinds(t, h.poll(), "BASELINE", "NEW-THREAD")
	if h.poll() != nil {
		t.Fatalf("resolving a thread must not emit")
	}
	wantKinds(t, h.poll(), "NEW-THREAD")
	if h.poll() != nil {
		t.Fatalf("still-open thread must stay silent")
	}
}

func TestCIFailureReportsJobAndLogTailOncePerRun(t *testing.T) {
	w := newWorld()
	green := openPR(1, "aaaa1111")
	green.rollup, green.total = "PENDING", 1
	green.checks = []ck{{run: 11, name: "go-core", status: "IN_PROGRESS", url: jobDetails}}
	red := green
	red.rollup = "FAILURE"
	red.checks = []ck{{run: 11, name: "go-core", status: "COMPLETED", result: "FAILURE", url: jobDetails}}
	rerun := red
	rerun.checks = []ck{{run: 12, name: "go-core", status: "COMPLETED", result: "FAILURE", url: jobDetails}}
	w.seed(1, green, red, red, rerun, rerun)
	w.logs["777"] = "2026-10-25T11:00:00.0000000Z \x1b[36;1mgo test ./...\x1b[0m\n" +
		"2026-10-25T11:00:01.0000000Z one\n2026-10-25T11:00:02.0000000Z two\n" +
		"2026-10-25T11:00:03.0000000Z --- FAIL: TestBoom\n" +
		"2026-10-25T11:00:04.0000000Z ##[error]Process completed with exit code 1.\n" +
		"2026-10-25T11:00:05.0000000Z Post job cleanup.\n"
	h := newHarness(t, []int{1}, w)

	wantKinds(t, h.poll(), "BASELINE")
	got := h.poll()
	wantKinds(t, got, "CI-FAIL")
	e := parseEvent(t, got[0])
	if e.kv["job"] != "go-core" || e.kv["result"] != "FAILURE" {
		t.Fatalf("bad CI-FAIL fields: %s", got[0])
	}
	tail := e.kv["tail"]
	if !strings.Contains(tail, "--- FAIL: TestBoom") || !strings.Contains(tail, "##[error]Process completed with exit code 1.") {
		t.Fatalf("tail must end at the error line, got %q", tail)
	}
	if strings.Contains(tail, "\x1b") || strings.Contains(tail, "2026-10-25T") || strings.Contains(tail, "Post job cleanup") {
		t.Fatalf("tail must drop ANSI, timestamps, and post-error cleanup, got %q", tail)
	}
	if strings.Count(tail, "|") != 2 {
		t.Fatalf("tail must hold exactly LogLines=3 lines, got %q", tail)
	}
	if h.poll() != nil {
		t.Fatalf("same failed run must not be reported twice")
	}
	wantKinds(t, h.poll(), "CI-FAIL") // a rerun that fails again is a new run
	if h.poll() != nil {
		t.Fatalf("same rerun must not be reported twice")
	}
}

func TestCIFailureLogFetchFailureStillReports(t *testing.T) {
	w := newWorld()
	red := openPR(1, "aaaa1111")
	red.rollup, red.total = "FAILURE", 1
	red.checks = []ck{{run: 11, name: "lint", status: "COMPLETED", result: "TIMED_OUT", url: jobDetails}}
	w.seed(1, red)
	h := newHarness(t, []int{1}, w)

	got := h.poll()
	wantKinds(t, got, "BASELINE", "CI-FAIL")
	if tail := parseEvent(t, got[1]).kv["tail"]; !strings.Contains(tail, "log unavailable") {
		t.Fatalf("tail = %q, want a log-unavailable marker", tail)
	}
}

func TestFailureOnSecondChecksPageIsFound(t *testing.T) {
	w := newWorld()
	first := openPR(1, "aaaa1111")
	first.rollup, first.total = "FAILURE", 101
	first.nextCursor = "CUR1"
	for i := 0; i < 100; i++ {
		first.checks = append(first.checks, ck{run: int64(1000 + i), name: "ok", status: "COMPLETED", result: "SUCCESS"})
	}
	second := first
	second.nextCursor = ""
	second.checks = []ck{{run: 2000, name: "late-gate", status: "COMPLETED", result: "FAILURE", url: "https://example.test/x"}}
	w.seed(1, first)
	w.pages["CUR1"] = second
	h := newHarness(t, []int{1}, w)

	got := h.poll()
	wantKinds(t, got, "BASELINE", "CI-FAIL")
	if j := parseEvent(t, got[1]).kv["job"]; j != "late-gate" {
		t.Fatalf("failure on page two not found, job = %q", j)
	}
}

func TestConflictBehindAndRecovery(t *testing.T) {
	w := newWorld()
	clean := openPR(1, "aaaa1111")
	conflict := clean
	conflict.mergeable, conflict.mergeState = "CONFLICTING", "DIRTY"
	movedBase := conflict
	movedBase.baseOID = "ba5e0002"
	behind := clean
	behind.mergeState = "BEHIND"
	w.seed(1, clean, conflict, conflict, clean, conflict, movedBase, behind)
	w.mergeTree["ba5e0001..aaaa1111"] = mergeTreeResult{out: "tree\nCHANGELOG.md\nspecs/live-postgres-readiness-weights.v1.json\n", code: 1}
	w.mergeTree["ba5e0002..aaaa1111"] = mergeTreeResult{out: "tree\ngo/internal/reducer/x.go\nCHANGELOG.md\n", code: 1}
	h := newHarness(t, []int{1}, w)

	wantKinds(t, h.poll(), "BASELINE")
	got := h.poll()
	wantKinds(t, got, "CONFLICT")
	e := parseEvent(t, got[0])
	if e.kv["state"] != "CONFLICTING" || e.kv["test_merge"] != "conflict" || e.kv["recipe"] != "4a" ||
		e.kv["files"] != "CHANGELOG.md,specs/live-postgres-readiness-weights.v1.json" {
		t.Fatalf("mechanical conflict misreported: %s", got[0])
	}
	if len(w.fetches) != 1 || !strings.Contains(w.fetches[0], "refs/pull/1/head") {
		t.Fatalf("test merge must fetch the pull head, got %v", w.fetches)
	}
	if h.poll() != nil {
		t.Fatalf("same conflict must not repeat")
	}
	if h.poll() != nil {
		t.Fatalf("returning to clean must not emit")
	}
	wantKinds(t, h.poll(), "CONFLICT") // conflicted again after being clean
	got = h.poll()                     // base moved while still conflicting
	wantKinds(t, got, "CONFLICT")
	if r := parseEvent(t, got[0]).kv["recipe"]; r != "count-choice-lines" {
		t.Fatalf("a source-file conflict is not a recipe case, got %q", r)
	}
	// BEHIND with a clean merge: the same base and head now merge cleanly.
	w.mergeTree["ba5e0001..aaaa1111"] = mergeTreeResult{out: "tree\n"}
	got = h.poll()
	wantKinds(t, got, "CONFLICT")
	b := parseEvent(t, got[0])
	if b.kv["state"] != "BEHIND" || b.kv["test_merge"] != "clean" || b.kv["recipe"] != "rebase" {
		t.Fatalf("BEHIND misreported: %s", got[0])
	}
}

func TestConflictWhenTestMergeUnavailableStillReports(t *testing.T) {
	w := newWorld()
	conflict := openPR(1, "aaaa1111")
	conflict.mergeable, conflict.mergeState = "CONFLICTING", "DIRTY"
	w.seed(1, conflict)
	h := newHarness(t, []int{1}, w) // no mergeTree entry: git exits 128

	got := h.poll()
	wantKinds(t, got, "BASELINE", "CONFLICT")
	e := parseEvent(t, got[1])
	if e.kv["test_merge"] != "unavailable" || e.kv["recipe"] != "unknown" {
		t.Fatalf("an unavailable test merge has no files to classify, got %s", got[1])
	}
}

func TestUnknownMergeabilityNeedsTwoPolls(t *testing.T) {
	w := newWorld()
	unknown := openPR(1, "aaaa1111")
	unknown.mergeable, unknown.mergeState = "UNKNOWN", "UNKNOWN"
	w.seed(1, unknown, unknown, unknown)
	w.mergeTree["ba5e0001..aaaa1111"] = mergeTreeResult{out: "tree\nCHANGELOG.md\n", code: 1}
	h := newHarness(t, []int{1}, w)

	wantKinds(t, h.poll(), "BASELINE")
	got := h.poll()
	wantKinds(t, got, "CONFLICT")
	if s := parseEvent(t, got[0]).kv["state"]; s != "UNKNOWN" {
		t.Fatalf("state = %q", s)
	}
	if h.poll() != nil {
		t.Fatalf("persistent UNKNOWN must report once")
	}
}

func TestHeadMovedReportsSourceAgainstLocalCheckout(t *testing.T) {
	w := newWorld()
	w.seed(1, openPR(1, "aaaa1111"), openPR(1, "bbbb2222"), openPR(1, "bbbb2222"), openPR(1, "cccc3333"))
	w.localRef, w.localSHA = "drive/branch", "bbbb2222"
	h := newHarness(t, []int{1}, w)

	wantKinds(t, h.poll(), "BASELINE")
	got := h.poll()
	wantKinds(t, got, "HEAD-MOVED")
	e := parseEvent(t, got[0])
	if e.kv["from"] != "aaaa1111" || e.kv["to"] != "bbbb2222" || e.kv["source"] != "local" {
		t.Fatalf("own push misreported: %s", got[0])
	}
	if h.poll() != nil {
		t.Fatalf("unchanged head must stay silent")
	}
	got = h.poll()
	wantKinds(t, got, "HEAD-MOVED")
	if s := parseEvent(t, got[0]).kv["source"]; s != "foreign" {
		t.Fatalf("a head the local checkout does not hold must be foreign, got %q", s)
	}
}

func TestMergedAndClosedStopWatching(t *testing.T) {
	w := newWorld()
	merged := openPR(1, "aaaa1111")
	merged.state, merged.mergedAt, merged.mergeOID = "MERGED", "2026-10-25T12:30:00Z", "mmmm9999"
	closed := openPR(2, "dddd4444")
	closed.state = "CLOSED"
	w.seed(1, openPR(1, "aaaa1111"), merged)
	w.seed(2, openPR(2, "dddd4444"), closed)
	h := newHarness(t, []int{1, 2}, w)

	wantKinds(t, h.poll(), "BASELINE", "BASELINE")
	got := h.poll()
	wantKinds(t, got, "MERGED", "CLOSED")
	if c := parseEvent(t, got[0]).kv["commit"]; c != "mmmm9999" {
		t.Fatalf("merge commit = %q", c)
	}
	callsBefore := len(w.calls)
	if remaining := h.wt.Poll(context.Background()); remaining != 0 {
		t.Fatalf("remaining open PRs = %d, want 0", remaining)
	}
	if len(w.calls) != callsBefore {
		t.Fatalf("finished PRs must not be polled again")
	}
}

func TestGreenAfterTwoStableReadsOfTheFullCheckSet(t *testing.T) {
	w := newWorld()
	mk := func(rollup string, total int) fx {
		f := openPR(1, "aaaa1111")
		f.rollup, f.total = rollup, total
		f.checks = append(f.checks, requiredChecks()...)
		for i := len(f.checks); i < total && i < 100; i++ {
			f.checks = append(f.checks, ck{run: int64(i + 1), name: "c", status: "COMPLETED", result: "SUCCESS"})
		}
		return f
	}
	wave1 := mk("SUCCESS", 5)
	wave2 := mk("SUCCESS", 7) // a second wave of checks registered
	pendingBeyondPage := mk("PENDING", 150)
	w.seed(1, wave1, wave2, wave2, wave2, pendingBeyondPage, mk("SUCCESS", 7), mk("SUCCESS", 7))
	h := newHarness(t, []int{1}, w)

	wantKinds(t, h.poll(), "BASELINE") // 5 checks, first read
	if h.poll() != nil {
		t.Fatalf("total changed 5 -> 7: first read of the new set is not stable")
	}
	got := h.poll()
	wantKinds(t, got, "GREEN")
	if g := parseEvent(t, got[0]); g.kv["checks"] != "7" || g.kv["head"] != "aaaa1111" {
		t.Fatalf("green fields wrong: %s", got[0])
	}
	if h.poll() != nil {
		t.Fatalf("green must be reported once")
	}
	if h.poll() != nil {
		t.Fatalf("a PENDING rollup past the first page must not read as green")
	}
	if h.poll() != nil {
		t.Fatalf("first SUCCESS read after pending is not yet stable")
	}
	wantKinds(t, h.poll(), "GREEN") // pending then green again is a new green
}

func TestEveryGHCallIsPinnedToTheAccountToken(t *testing.T) {
	w := newWorld()
	red := openPR(1, "aaaa1111")
	red.rollup, red.total = "FAILURE", 1
	red.checks = []ck{{run: 11, name: "go-core", status: "COMPLETED", result: "FAILURE", url: jobDetails}}
	w.seed(1, red)
	w.logs["777"] = "##[error]x\n"
	h := newHarness(t, []int{1}, w)
	h.poll()

	var sawToken, sawAPI bool
	for _, c := range w.calls {
		if c.Name != "gh" {
			continue
		}
		hasPinned, hasAmbient := false, false
		for _, kv := range c.Env {
			hasPinned = hasPinned || kv == "GH_TOKEN=tok-acct"
			hasAmbient = hasAmbient || strings.HasSuffix(kv, "ambient-wrong")
		}
		if c.Args[0] == "auth" {
			sawToken = true
			if hasPinned || hasAmbient {
				t.Fatalf("token lookup must run against the keyring with no token env: %v", c.Env)
			}
			if c.Args[len(c.Args)-1] != "acct" {
				t.Fatalf("token lookup must name the account, got %v", c.Args)
			}
			continue
		}
		sawAPI = true
		if !hasPinned || hasAmbient {
			t.Fatalf("gh %v ran without the pinned token or with an ambient one: %v", c.Args[:2], c.Env)
		}
	}
	if !sawToken || !sawAPI {
		t.Fatalf("expected token lookup and API calls, got %d calls", len(w.calls))
	}
}

func TestReadErrorsAreReportedOnceThenRecover(t *testing.T) {
	w := newWorld()
	w.seed(1, openPR(1, "aaaa1111"))
	w.readErr[1] = errors.New("HTTP 502")
	h := newHarness(t, []int{1}, w)

	got := h.poll()
	wantKinds(t, got, "ERROR")
	if !strings.Contains(got[0], "502") {
		t.Fatalf("error must carry the cause: %s", got[0])
	}
	if h.poll() != nil {
		t.Fatalf("same error must not repeat")
	}
	delete(w.readErr, 1)
	wantKinds(t, h.poll(), "BASELINE")
}
