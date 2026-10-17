// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain strips the git environment a hook exports (GIT_DIR and friends) so
// the scratch repositories below never resolve to the repository whose hook is
// running this test. Without it a pre-push run would aim every git call here
// at the real repository.
func TestMain(m *testing.M) {
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_PREFIX"} {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

// scratchRepo is a throwaway git repository the seeded tests drive through
// the real run entry point.
type scratchRepo struct {
	t    *testing.T
	root string
}

func newScratchRepo(t *testing.T) *scratchRepo {
	t.Helper()
	r := &scratchRepo{t: t, root: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.git("config", "user.email", "dirgate@example.invalid")
	r.git("config", "user.name", "dirgate test")
	r.git("config", "commit.gpgsign", "false")
	return r
}

func (r *scratchRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.root}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, errb.String())
	}
	return strings.TrimSpace(out.String())
}

func (r *scratchRepo) write(rel, content string) {
	r.t.Helper()
	p := filepath.Join(r.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// addGoFiles writes count non-test .go files named prefix000.go.. into the
// go/-relative directory dir, starting at index from.
func (r *scratchRepo) addGoFiles(dir, prefix string, from, count int) {
	r.t.Helper()
	for i := from; i < from+count; i++ {
		r.write(fmt.Sprintf("go/%s/%s%03d.go", dir, prefix, i), "package x\n")
	}
}

func (r *scratchRepo) rm(rel string) {
	r.t.Helper()
	r.git("rm", "-q", "-f", rel)
}

// commit stages everything and commits with message.
func (r *scratchRepo) commit(message string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", message)
	return r.git("rev-parse", "HEAD")
}

// result is one run of the tool.
type result struct {
	code int
	out  string
}

func (r *scratchRepo) run(args ...string) result {
	return r.runEnv(nil, args...)
}

func (r *scratchRepo) runEnv(env map[string]string, args ...string) result {
	r.t.Helper()
	var out, errb bytes.Buffer
	getenv := func(k string) string { return env[k] }
	code := run(append([]string{"-repo", r.root}, args...), &out, &errb, getenv)
	return result{code: code, out: out.String() + errb.String()}
}

func (res result) mustExit(t *testing.T, want int, contains ...string) {
	t.Helper()
	if res.code != want {
		t.Fatalf("exit = %d, want %d\n%s", res.code, want, res.out)
	}
	for _, c := range contains {
		if !strings.Contains(res.out, c) {
			t.Fatalf("output missing %q\n%s", c, res.out)
		}
	}
}

// baseline writes go/cmd/dirgate/baseline-*.tsv from the current HEAD tree with
// the real generator and commits them, the way the landing PR does.
func (r *scratchRepo) baseline() {
	r.t.Helper()
	res := r.run("-base-ref", "HEAD", "baseline")
	if res.code != 0 {
		r.t.Fatalf("baseline generator exit = %d\n%s", res.code, res.out)
	}
	r.commit("generate dirgate baselines")
}

// fastImport feeds a git fast-import stream to the repository.
func (r *scratchRepo) fastImport(stream string) {
	r.t.Helper()
	cmd := exec.Command("git", "-C", r.root, "fast-import", "--quiet")
	cmd.Stdin = strings.NewReader(stream)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		r.t.Fatalf("git fast-import: %v\n%s", err, errb.String())
	}
}

// chainCommits returns a fast-import stream that adds count commits on ref, each
// parented on the previous one. The first commit starts from parent (a commit id
// or ref). firstFile, when set, is added by the first commit; later commits
// change nothing. msgs[0] is the first commit's message.
func chainCommits(ref, parent string, count int, firstFile, firstMsg string) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		msg := fmt.Sprintf("chain commit %d", i)
		if i == 0 {
			msg = firstMsg
		}
		fmt.Fprintf(&b, "commit %s\ncommitter t <t@example.invalid> %d +0000\ndata %d\n%s\n", ref, 1700000000+i, len(msg), msg)
		if i == 0 {
			fmt.Fprintf(&b, "from %s\n", parent)
			if firstFile != "" {
				fmt.Fprintf(&b, "M 100644 inline %s\ndata 10\npackage x\n\n", firstFile)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// dataRows returns the non-comment, non-empty lines of a TSV.
func dataRows(content string) []string {
	var rows []string
	for _, l := range strings.Split(content, "\n") {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		rows = append(rows, l)
	}
	return rows
}
