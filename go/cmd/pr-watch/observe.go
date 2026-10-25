// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strconv"
	"strings"
)

// noiseAuthors post on every PR (deploy previews, invites, merge-bot status)
// and never need a reply. Every other author counts, including this
// account's own reviews.
var noiseAuthors = map[string]bool{
	"cloudflare-workers-and-pages": true,
	"github-actions":               true,
	"mergify":                      true,
}

func isNoise(author string) bool {
	return noiseAuthors[strings.TrimSuffix(author, "[bot]")]
}

// observe compares one fresh snapshot with what was already reported for the
// PR and prints an event for each change.
func (w *Watcher) observe(ctx context.Context, token string, st *prState, snap Snapshot) {
	if snap.State != "OPEN" {
		w.observeEnd(st, snap)
		return
	}
	w.observeHead(ctx, st, snap)
	w.observeThreads(st, snap)
	w.observeReviews(st, snap)
	w.observeComments(st, snap)
	w.observeChecks(ctx, token, st, snap)
	w.observeConflict(ctx, st, snap)
	w.observeGreen(st, snap)
	st.baselined = true
}

// observeEnd reports a merge or close once and stops watching the PR.
func (w *Watcher) observeEnd(st *prState, snap Snapshot) {
	if st.done {
		return
	}
	st.done = true
	if snap.State == "MERGED" {
		w.emit("MERGED", snap.Number, "commit", snap.MergeCommit, "at", snap.MergedAt,
			"note", "merged: stop pushing to this branch")
		return
	}
	w.emit("CLOSED", snap.Number, "note", "closed without merge: stop pushing to this branch")
}

// observeHead prints the baseline on the first read and HEAD-MOVED when the
// head SHA changes afterwards.
func (w *Watcher) observeHead(ctx context.Context, st *prState, snap Snapshot) {
	if !st.baselined {
		unresolved := 0
		for _, t := range snap.Threads {
			if !t.Resolved {
				unresolved++
			}
		}
		reviews, comments := 0, 0
		for _, r := range snap.Reviews {
			if countsReview(r) {
				reviews++
			}
			st.notified["review:"+r.ID] = true
		}
		for _, c := range snap.Comments {
			if !isNoise(c.Author) {
				comments++
			}
			st.notified["comment:"+c.ID] = true
		}
		w.emit("BASELINE", snap.Number, "head", short(snap.Head), "state", snap.State,
			"rollup", snap.RollupState, "checks", strconv.Itoa(snap.TotalChecks), "merge", snap.MergeState,
			"unresolved", strconv.Itoa(unresolved), "reviews", strconv.Itoa(reviews), "comments", strconv.Itoa(comments))
	} else if snap.Head != st.head {
		w.emit("HEAD-MOVED", snap.Number, "from", short(st.head), "to", short(snap.Head),
			"by", snap.HeadAuthor, "title", clip(snap.HeadTitle), "source", w.headSource(ctx, snap))
	}
	st.head = snap.Head
}

// headSource says whether the local checkout holds the new head. A head that
// the checkout's branch does not hold was pushed by someone else.
func (w *Watcher) headSource(ctx context.Context, snap Snapshot) string {
	branch, err := w.exec(ctx, Command{
		Name: w.cfg.Git, Dir: w.cfg.Dir, Env: w.cfg.Environ,
		Args: []string{"symbolic-ref", "--short", "-q", "HEAD"},
	})
	if err != nil || strings.TrimSpace(string(branch)) != snap.HeadRef {
		return "unknown"
	}
	sha, err := w.exec(ctx, Command{
		Name: w.cfg.Git, Dir: w.cfg.Dir, Env: w.cfg.Environ,
		Args: []string{"rev-parse", "HEAD"},
	})
	if err != nil {
		return "unknown"
	}
	if strings.TrimSpace(string(sha)) == snap.Head {
		return "local"
	}
	return "foreign"
}

// observeThreads reports each unresolved thread once. A thread that is
// resolved and later reopened is reported again.
func (w *Watcher) observeThreads(st *prState, snap Snapshot) {
	live := map[string]bool{}
	for _, t := range snap.Threads {
		if t.Resolved {
			continue
		}
		live[t.ID] = true
		if st.openThreads[t.ID] {
			continue
		}
		st.openThreads[t.ID] = true
		w.emit("NEW-THREAD", snap.Number, "thread", t.ID, "author", t.Author,
			"at", t.Path+":"+strconv.Itoa(t.Line), "url", t.URL, "body", clip(t.Body))
	}
	for id := range st.openThreads {
		if !live[id] {
			delete(st.openThreads, id)
		}
	}
}

// countsReview reports whether a review is worth a line: submitted, and not
// from a noise author. A pending review is an unsent draft only its author sees.
func countsReview(r Review) bool {
	return r.State != "PENDING" && !isNoise(r.Author)
}

func (w *Watcher) observeReviews(st *prState, snap Snapshot) {
	for _, r := range snap.Reviews {
		key := "review:" + r.ID
		if st.notified[key] || !countsReview(r) {
			continue
		}
		st.notified[key] = true
		w.emit("NEW-REVIEW", snap.Number, "review", r.ID, "author", r.Author, "state", r.State, "url", r.URL, "body", clip(r.Body))
	}
}

func (w *Watcher) observeComments(st *prState, snap Snapshot) {
	for _, c := range snap.Comments {
		key := "comment:" + c.ID
		if st.notified[key] || isNoise(c.Author) {
			continue
		}
		st.notified[key] = true
		w.emit("NEW-COMMENT", snap.Number, "comment", c.ID, "author", c.Author, "url", c.URL, "body", clip(c.Body))
	}
}

// observeChecks reports each failed check run once, with the end of its log.
func (w *Watcher) observeChecks(ctx context.Context, token string, st *prState, snap Snapshot) {
	for _, c := range snap.Checks {
		key := "ci:" + c.ID
		if !c.Failing() || st.notified[key] {
			continue
		}
		st.notified[key] = true
		kv := []string{"job", c.Name, "result", c.Result, "run", c.ID, "url", c.URL}
		if tail, ok := w.failureTail(ctx, token, c); ok {
			kv = append(kv, "tail", tail)
		}
		w.emit("CI-FAIL", snap.Number, kv...)
	}
}

// observeConflict reports CONFLICTING, BEHIND, or a persistent UNKNOWN once per
// (state, head, base) with a local test merge and a recipe hint.
func (w *Watcher) observeConflict(ctx context.Context, st *prState, snap Snapshot) {
	state := ""
	switch {
	case snap.Mergeable == "CONFLICTING" || snap.MergeState == "DIRTY":
		state = "CONFLICTING"
	case snap.MergeState == "BEHIND":
		state = "BEHIND"
	}
	if snap.Mergeable == "UNKNOWN" {
		st.unknownPolls++
		if state == "" && st.unknownPolls >= 2 {
			state = "UNKNOWN"
		}
	} else {
		st.unknownPolls = 0
	}
	if state == "" {
		st.conflictKey = ""
		return
	}
	key := state + "|" + snap.Head + "|" + snap.BaseOID
	if key == st.conflictKey {
		return
	}
	st.conflictKey = key
	kv := []string{"state", state, "head", short(snap.Head), "base", short(snap.BaseOID)}
	kv = append(kv, conflictFields(w.testMerge(ctx, snap), readGeneratedList(w.cfg.Dir))...)
	w.emit("CONFLICT", snap.Number, kv...)
}

// observeGreen reports GREEN once the full check set has been read complete,
// successful, and unchanged (same head, same total) on StableReads polls in a
// row. The rollup state and total count come from the head commit's rollup, so
// a pending check past the first page of 100 still blocks green.
func (w *Watcher) observeGreen(st *prState, snap Snapshot) {
	complete := snap.RollupState == "SUCCESS" && snap.TotalChecks > 0
	for _, c := range snap.Checks {
		if c.Pending() {
			complete = false
		}
	}
	if !complete {
		st.stableKey, st.stableReads, st.greenKey = "", 0, ""
		return
	}
	key := snap.Head + "|" + strconv.Itoa(snap.TotalChecks)
	if key == st.stableKey {
		st.stableReads++
	} else {
		st.stableKey, st.stableReads = key, 1
	}
	if st.stableReads < w.cfg.StableReads || st.greenKey == key {
		return
	}
	st.greenKey = key
	unresolved := 0
	for _, t := range snap.Threads {
		if !t.Resolved {
			unresolved++
		}
	}
	w.emit("GREEN", snap.Number, "head", short(snap.Head), "checks", strconv.Itoa(snap.TotalChecks),
		"reads", strconv.Itoa(st.stableReads), "merge", snap.MergeState, "unresolved", strconv.Itoa(unresolved))
}
