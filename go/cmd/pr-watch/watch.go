// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"io"
	"strconv"
	"strings"
	"time"
)

// Config holds the watcher settings that do not change while it runs.
type Config struct {
	// Repo is "owner/name".
	Repo string
	// Dir is the checkout used for the local test merge and for source
	// detection of a moved head. gh also runs there.
	Dir string
	// Account is the GitHub login whose keyring token every gh call uses.
	Account string
	// GH and Git name the executables.
	GH, Git string
	// PRs are the pull request numbers to watch.
	PRs []int
	// Interval is the pause between polls.
	Interval time.Duration
	// StableReads is how many consecutive identical, complete reads of the
	// full check set make a PR green.
	StableReads int
	// LogLines is how many log lines a CI-FAIL event carries.
	LogLines int
	// MaxPolls stops the watcher after this many polls; zero means run until
	// every PR is merged or closed.
	MaxPolls int
	// Environ is the parent environment handed to child processes, minus any
	// ambient GitHub token.
	Environ []string
}

// prState is what the watcher remembers about one PR between polls. It holds
// only markers of what was already reported; every fact it compares against is
// read fresh from GitHub on each poll.
type prState struct {
	baselined    bool
	done         bool
	head         string
	notified     map[string]bool // review, comment, and failed-run IDs already reported
	openThreads  map[string]bool // unresolved threads already reported
	conflictKey  string
	unknownPolls int
	stableKey    string
	stableReads  int
	greenKey     string
	lastErr      string
}

// Watcher polls pull requests and prints one line per change.
type Watcher struct {
	cfg   Config
	exec  Exec
	out   io.Writer
	now   func() time.Time
	state map[int]*prState
}

// NewWatcher builds a Watcher. exec runs gh and git; out receives event lines;
// now supplies timestamps.
func NewWatcher(cfg Config, exec Exec, out io.Writer, now func() time.Time) *Watcher {
	if cfg.StableReads < 1 {
		cfg.StableReads = 2
	}
	if cfg.LogLines < 1 {
		cfg.LogLines = 12
	}
	w := &Watcher{cfg: cfg, exec: exec, out: out, now: now, state: map[int]*prState{}}
	for _, n := range cfg.PRs {
		w.state[n] = &prState{notified: map[string]bool{}, openThreads: map[string]bool{}}
	}
	return w
}

// Poll reads every unfinished PR once, prints the events that changed since
// the last poll, and returns how many PRs are still open.
func (w *Watcher) Poll(ctx context.Context) int {
	active := w.active()
	if len(active) == 0 {
		return 0
	}
	token, err := w.token(ctx)
	if err != nil {
		for _, n := range active {
			w.reportError(n, err)
		}
		return len(active)
	}
	for _, n := range active {
		snap, err := w.fetchSnapshot(ctx, token, n)
		if err != nil {
			w.reportError(n, err)
			continue
		}
		w.state[n].lastErr = ""
		w.observe(ctx, token, w.state[n], snap)
	}
	return len(w.active())
}

// active lists the PRs that are not yet merged or closed, in argument order.
func (w *Watcher) active() []int {
	var out []int
	for _, n := range w.cfg.PRs {
		if !w.state[n].done {
			out = append(out, n)
		}
	}
	return out
}

// reportError prints a read failure once per distinct message, so a flapping
// API does not flood the stream and a dead watcher is never silent.
func (w *Watcher) reportError(pr int, err error) {
	msg := clip(err.Error())
	st := w.state[pr]
	if st.lastErr == msg {
		return
	}
	st.lastErr = msg
	w.emit("ERROR", pr, "msg", msg)
}

// Run polls until every PR is merged or closed, MaxPolls is reached, or ctx is
// cancelled. It always prints a WATCH line before returning, so a Monitor never
// sees the watcher end without a reason.
func (w *Watcher) Run(ctx context.Context) error {
	prs := make([]string, len(w.cfg.PRs))
	for i, n := range w.cfg.PRs {
		prs[i] = strconv.Itoa(n)
	}
	w.emit("WATCH", 0, "state", "start", "repo", w.cfg.Repo, "prs", strings.Join(prs, ","),
		"account", w.cfg.Account, "interval", w.cfg.Interval.String())
	for polls := 1; ; polls++ {
		if w.Poll(ctx) == 0 {
			w.emit("WATCH", 0, "state", "done", "reason", "every watched PR is merged or closed")
			return nil
		}
		if w.cfg.MaxPolls > 0 && polls >= w.cfg.MaxPolls {
			w.emit("WATCH", 0, "state", "stopped", "reason", "max-polls reached")
			return nil
		}
		select {
		case <-ctx.Done():
			w.emit("WATCH", 0, "state", "stopped", "reason", "interrupted")
			return ctx.Err()
		case <-time.After(w.cfg.Interval):
		}
	}
}
