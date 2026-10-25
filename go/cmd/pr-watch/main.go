// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command pr-watch is documented in doc.go.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultRepo     = "eshu-hq/eshu"
	defaultInterval = 60 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	home, _ := os.UserHomeDir()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Environ(), home, osExec))
}

// run implements the CLI. It is separate from main so tests can supply
// arguments, a fake Exec, and captured output. Every path that stops the
// watcher prints a final WATCH line on stdout, so a background Monitor never
// loses a watcher without a reason.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, environ []string, home string, exec Exec) int {
	fs := flag.NewFlagSet("pr-watch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", defaultRepo, "repository as owner/name")
	dir := fs.String("dir", ".", "checkout used for the local test merge and for gh")
	account := fs.String("account", "", "GitHub login to pin; inferred from -dir under ~/personal-repos when empty")
	interval := fs.Duration("interval", defaultInterval, "pause between polls")
	stable := fs.Int("stable-reads", 2, "identical complete reads of the full check set before GREEN")
	logLines := fs.Int("log-lines", 12, "log lines carried by a CI-FAIL event")
	maxPolls := fs.Int("max-polls", 0, "stop after this many polls; 0 runs until every PR is merged or closed")
	once := fs.Bool("once", false, "poll once and exit (same as -max-polls 1)")
	ghBin := fs.String("gh", "gh", "gh executable")
	gitBin := fs.String("git", "git", "git executable")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: pr-watch [flags] <pr-number>...")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	prs, err := parsePRs(fs.Args())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "pr-watch: %v\n", err)
		fs.Usage()
		return 2
	}
	if *interval <= 0 || *stable < 1 || *logLines < 1 || *maxPolls < 0 {
		_, _ = fmt.Fprintln(stderr, "pr-watch: -interval, -stable-reads, and -log-lines must be positive and -max-polls non-negative")
		return 2
	}
	if *once {
		*maxPolls = 1
	}
	login, err := resolveAccount(*account, *dir, home)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "%s WATCH state=exit code=1 reason=%s\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"), formatValue(err.Error()))
		_, _ = fmt.Fprintf(stderr, "pr-watch: %v\n", err)
		return 1
	}
	cfg := Config{
		Repo: *repo, Dir: *dir, Account: login, GH: *ghBin, Git: *gitBin, PRs: prs,
		Interval: *interval, StableReads: *stable, LogLines: *logLines, MaxPolls: *maxPolls, Environ: environ,
	}
	w := NewWatcher(cfg, exec, stdout, time.Now)
	if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintf(stderr, "pr-watch: %v\n", err)
		return 1
	}
	return 0
}

// parsePRs reads pull request numbers, accepting "123" and "#123", and drops
// duplicates while keeping the given order.
func parsePRs(args []string) ([]int, error) {
	if len(args) == 0 {
		return nil, errors.New("at least one pull request number is required")
	}
	seen := map[int]bool{}
	var prs []int
	for _, a := range args {
		n, err := strconv.Atoi(strings.TrimPrefix(a, "#"))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%q is not a pull request number", a)
		}
		if !seen[n] {
			seen[n] = true
			prs = append(prs, n)
		}
	}
	return prs, nil
}
