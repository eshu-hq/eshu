// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ck is a check in a fixture. kind "status" builds a legacy status context.
type ck struct {
	run     int64
	name    string
	status  string
	result  string
	url     string
	status2 bool
}

type thr struct {
	id       string
	resolved bool
	path     string
	line     int
	author   string
	body     string
}

type note struct {
	id, author, state, body string
}

// fx is one poll's worth of PR state, rendered into the GraphQL wire format.
type fx struct {
	num        int
	state      string
	head       string
	headRef    string
	base       string
	baseOID    string
	mergeable  string
	mergeState string
	rollup     string
	total      int
	checks     []ck
	nextCursor string
	threads    []thr
	reviews    []note
	comments   []note
	mergedAt   string
	mergeOID   string
	title      string
	author     string
}

// base returns a healthy open PR fixture.
func openPR(num int, head string) fx {
	return fx{
		num: num, state: "OPEN", head: head, headRef: "drive/branch", base: "main",
		baseOID: "base0001", mergeable: "MERGEABLE", mergeState: "CLEAN",
		rollup: "PENDING", title: "a commit", author: "linuxdynasty",
	}
}

func actor(login string) any {
	if login == "" {
		return nil
	}
	return map[string]any{"login": login}
}

// wire renders the fixture exactly as GitHub's GraphQL API returns it.
func (f fx) wire() []byte {
	nodes := make([]any, 0, len(f.checks))
	for _, c := range f.checks {
		if c.status2 {
			nodes = append(nodes, map[string]any{"__typename": "StatusContext", "context": c.name, "state": c.result, "targetUrl": c.url})
			continue
		}
		nodes = append(nodes, map[string]any{"__typename": "CheckRun", "databaseId": c.run, "name": c.name, "status": c.status, "conclusion": nilIfEmpty(c.result), "detailsUrl": c.url})
	}
	page := map[string]any{"hasNextPage": f.nextCursor != "", "endCursor": nilIfEmpty(f.nextCursor)}
	var rollup any
	if f.rollup != "" {
		rollup = map[string]any{"state": f.rollup, "contexts": map[string]any{"totalCount": f.total, "pageInfo": page, "nodes": nodes}}
	}
	threads := []any{}
	for _, t := range f.threads {
		threads = append(threads, map[string]any{
			"id": t.id, "isResolved": t.resolved, "path": t.path, "line": t.line,
			"comments": map[string]any{"nodes": []any{map[string]any{"id": t.id + "-c1", "author": actor(t.author), "body": t.body, "url": "https://example.test/thread/" + t.id}}},
		})
	}
	toNodes := func(ns []note, withState bool) []any {
		out := []any{}
		for _, n := range ns {
			m := map[string]any{"id": n.id, "author": actor(n.author), "body": n.body, "url": "https://example.test/" + n.id}
			if withState {
				m["state"] = n.state
			}
			out = append(out, m)
		}
		return out
	}
	var merged any
	var mergeCommit any
	if f.mergedAt != "" {
		merged = f.mergedAt
		mergeCommit = map[string]any{"oid": f.mergeOID}
	}
	pr := map[string]any{
		"number": f.num, "state": f.state, "mergedAt": merged, "mergeCommit": mergeCommit,
		"headRefName": f.headRef, "headRefOid": f.head, "baseRefName": f.base,
		"baseRef":   map[string]any{"target": map[string]any{"oid": f.baseOID}},
		"mergeable": f.mergeable, "mergeStateStatus": f.mergeState,
		"reviewThreads": map[string]any{"nodes": threads},
		"reviews":       map[string]any{"nodes": toNodes(f.reviews, true)},
		"comments":      map[string]any{"nodes": toNodes(f.comments, false)},
		"commits": map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{
			"oid": f.head, "messageHeadline": f.title,
			"author":            map[string]any{"user": actor(f.author)},
			"statusCheckRollup": rollup,
		}}}},
	}
	raw, err := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": pr}}})
	if err != nil {
		panic(err)
	}
	return raw
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// world is the seeded fake of GitHub and the local git checkout. Each PR has a
// script of fixtures; the Nth first-page read of a PR returns the Nth fixture
// (the last one repeats). Follow-up check pages are served by cursor.
type world struct {
	mu        sync.Mutex
	script    map[int][]fx
	reads     map[int]int
	pages     map[string]fx
	logs      map[string]string
	mergeTree map[string]mergeTreeResult // key: base+".."+head
	localRef  string
	localSHA  string
	readErr   map[int]error
	calls     []Command
	fetches   []string
}

type mergeTreeResult struct {
	out  string
	code int
}

func newWorld() *world {
	return &world{
		script: map[int][]fx{}, reads: map[int]int{}, pages: map[string]fx{},
		logs: map[string]string{}, mergeTree: map[string]mergeTreeResult{}, readErr: map[int]error{},
	}
}

func (w *world) seed(num int, polls ...fx) { w.script[num] = append(w.script[num], polls...) }

func argValue(args []string, key string) string {
	for i, a := range args {
		if (a == "-f" || a == "-F") && i+1 < len(args) && strings.HasPrefix(args[i+1], key+"=") {
			return strings.TrimPrefix(args[i+1], key+"=")
		}
	}
	return ""
}

// Exec implements Exec for the fake world.
func (w *world) Exec(_ context.Context, c Command) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, c)
	switch c.Name {
	case "gh":
		return w.gh(c)
	case "git":
		return w.git(c)
	}
	return nil, fmt.Errorf("unexpected command %s", c)
}

func (w *world) gh(c Command) ([]byte, error) {
	a := c.Args
	switch {
	case len(a) >= 2 && a[0] == "auth" && a[1] == "token":
		return []byte("tok-" + a[len(a)-1] + "\n"), nil
	case len(a) >= 2 && a[0] == "api" && a[1] == "graphql":
		num, _ := strconv.Atoi(argValue(a, "number"))
		if err := w.readErr[num]; err != nil {
			return nil, err
		}
		if after := argValue(a, "after"); after != "" {
			f, ok := w.pages[after]
			if !ok {
				return nil, fmt.Errorf("no page for cursor %q", after)
			}
			return f.wire(), nil
		}
		script := w.script[num]
		if len(script) == 0 {
			return nil, fmt.Errorf("no script for pr %d", num)
		}
		i := w.reads[num]
		w.reads[num]++
		if i >= len(script) {
			i = len(script) - 1
		}
		return script[i].wire(), nil
	case len(a) >= 3 && a[0] == "api" && a[1] == "--allow-escape-sequences":
		id := a[2][strings.LastIndex(a[2], "jobs/")+len("jobs/"):]
		id = strings.TrimSuffix(id, "/logs")
		if log, ok := w.logs[id]; ok {
			return []byte(log), nil
		}
		return nil, &ExitError{Code: 1, Stderr: "gh: Not Found (HTTP 404)"}
	}
	return nil, fmt.Errorf("unexpected gh call %s", c)
}

func (w *world) git(c Command) ([]byte, error) {
	a := c.Args
	switch a[0] {
	case "fetch":
		w.fetches = append(w.fetches, strings.Join(a[1:], " "))
		return nil, nil
	case "merge-tree":
		key := a[len(a)-2] + ".." + a[len(a)-1]
		r, ok := w.mergeTree[key]
		if !ok {
			return nil, &ExitError{Code: 128, Stderr: "fatal: unknown commits " + key}
		}
		if r.code != 0 {
			return []byte(r.out), &ExitError{Code: r.code}
		}
		return []byte(r.out), nil
	case "symbolic-ref":
		return []byte(w.localRef + "\n"), nil
	case "rev-parse":
		return []byte(w.localSHA + "\n"), nil
	}
	return nil, fmt.Errorf("unexpected git call %s", c)
}

// harness drives a Watcher over a world with a fixed clock and captured output.
type harness struct {
	t   *testing.T
	w   *world
	out *bytes.Buffer
	wt  *Watcher
}

func newHarness(t *testing.T, prs []int, w *world) *harness {
	t.Helper()
	out := &bytes.Buffer{}
	cfg := Config{
		Repo: "eshu-hq/eshu", Dir: "/work/tree", Account: "acct", GH: "gh", Git: "git",
		PRs: prs, StableReads: 2, LogLines: 3,
		Environ: []string{"PATH=/bin", "GH_TOKEN=ambient-wrong", "GITHUB_TOKEN=ambient-wrong"},
	}
	now := func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }
	return &harness{t: t, w: w, out: out, wt: NewWatcher(cfg, w.Exec, out, now)}
}

// poll runs one poll and returns only the new output lines it produced.
func (h *harness) poll() []string {
	h.t.Helper()
	before := h.out.Len()
	h.wt.Poll(context.Background())
	added := h.out.String()[before:]
	if added == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(added, "\n"), "\n")
}

// event is a parsed output line.
type event struct {
	kind string
	pr   string
	kv   map[string]string
	raw  string
}

func parseEvent(t *testing.T, line string) event {
	t.Helper()
	rest := strings.TrimSpace(line)
	_, rest, _ = strings.Cut(rest, " ") // timestamp
	kind, rest, _ := strings.Cut(rest, " ")
	e := event{kind: kind, kv: map[string]string{}, raw: line}
	for rest != "" {
		rest = strings.TrimLeft(rest, " ")
		key, after, ok := strings.Cut(rest, "=")
		if !ok {
			e.kv[strings.TrimSpace(rest)] = ""
			break
		}
		var val string
		if strings.HasPrefix(after, `"`) {
			end := 1
			for end < len(after) {
				if after[end] == '\\' {
					end += 2
					continue
				}
				if after[end] == '"' {
					break
				}
				end++
			}
			unq, err := strconv.Unquote(after[:end+1])
			if err != nil {
				t.Fatalf("bad quoted value in %q: %v", line, err)
			}
			val, rest = unq, after[end+1:]
		} else {
			val, rest, _ = strings.Cut(after, " ")
		}
		if key == "pr" {
			e.pr = val
		}
		e.kv[key] = val
	}
	return e
}

func kinds(t *testing.T, lines []string) []string {
	t.Helper()
	var out []string
	for _, l := range lines {
		out = append(out, parseEvent(t, l).kind)
	}
	return out
}

func wantKinds(t *testing.T, lines []string, want ...string) {
	t.Helper()
	got := kinds(t, lines)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("event kinds = %v, want %v\nlines:\n%s", got, want, strings.Join(lines, "\n"))
	}
}
