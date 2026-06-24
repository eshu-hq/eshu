package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillgenFragmentsDir writes a minimal but valid skill-fragments/ tree to
// a temp dir and returns the path. The fragments are the seven canonical
// ids from the S1 design; the body text is the shortest valid Markdown
// for each id, with byte_citation values that always parse and never
// collide with the canonical repo-root fragments.
func skillgenFragmentsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ids := []string{
		"operating-standard",
		"truth-labels",
		"capability-profiles",
		"reducer-invariant",
		"local-first",
		"bundle-reproduction",
		"per-collector-matrix",
	}
	for i, id := range ids {
		body := "---\n" +
			"id: " + id + "\n" +
			"version: 1.0.0\n" +
			"byte_citation: docs/test/" + id + ".md#1-1\n" +
			"description: test " + id + "\n" +
			"---\n\n" +
			"# " + id + " Title\n\nbody line " + id + "\n"
		path := filepath.Join(dir, id+".md")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write fragment %s: %v", path, err)
		}
		_ = i
	}
	return dir
}

func TestRun_GenWritesAllHostFiles(t *testing.T) {
	t.Parallel()
	fragments := skillgenFragmentsDir(t)
	expected := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"gen", "-fragments", fragments, "-expected", expected}, &stdout, &stderr); err != nil {
		t.Fatalf("run gen: %v\nstderr:\n%s", err, stderr.String())
	}
	// Every host must produce a file at <expected>/<host>/<output_path>.
	for _, host := range []string{"claude-code", "cursor", "codex"} {
		hostDir := filepath.Join(expected, host)
		entries, err := os.ReadDir(hostDir)
		if err != nil {
			t.Fatalf("expected/host dir for %s: %v", host, err)
		}
		if len(entries) == 0 {
			t.Errorf("host %s: expected/ dir is empty", host)
		}
	}
	// The summary line should report how many files were written.
	if !strings.Contains(stdout.String(), "wrote 3 host files") {
		t.Errorf("stdout missing summary line:\n%s", stdout.String())
	}
}

func TestRun_CheckExitsZeroOnBaseline(t *testing.T) {
	t.Parallel()
	fragments := skillgenFragmentsDir(t)
	expected := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"gen", "-fragments", fragments, "-expected", expected}, &stdout, &stderr); err != nil {
		t.Fatalf("run gen: %v", err)
	}
	stdout.Reset()
	if err := run([]string{"check", "-fragments", fragments, "-expected", expected}, &stdout, &stderr); err != nil {
		t.Fatalf("run check: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "in lockstep") {
		t.Errorf("check stdout missing 'in lockstep' line:\n%s", stdout.String())
	}
}

func TestRun_CheckExitsNonZeroOnDrift(t *testing.T) {
	t.Parallel()
	fragments := skillgenFragmentsDir(t)
	expected := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"gen", "-fragments", fragments, "-expected", expected}, &stdout, &stderr); err != nil {
		t.Fatalf("run gen: %v", err)
	}
	// Hand-edit one byte in the on-disk baseline.
	claudePath := filepath.Join(expected, "claude-code", ".claude", "skills", "eshu", "SKILL.md")
	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("baseline is empty")
	}
	data[0] ^= 0x01 // flip a bit
	if err := os.WriteFile(claudePath, data, 0o644); err != nil {
		t.Fatalf("write drifted baseline: %v", err)
	}
	stdout.Reset()
	err = run([]string{"check", "-fragments", fragments, "-expected", expected}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run check on drifted baseline: error = nil, want drift error")
	}
	if !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("error = %v, want contains 'drifted'", err)
	}
	if !strings.Contains(stdout.String(), "drifted") {
		t.Errorf("stdout missing drift summary:\n%s", stdout.String())
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"bogus"}, &stdout, &stderr); err == nil {
		t.Fatal("run(bogus) error = nil, want unknown subcommand error")
	}
}

func TestRun_NoArgs(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if err := run([]string{}, &stdout, &stderr); err == nil {
		t.Fatal("run() error = nil, want usage error")
	}
}

func TestRun_FragmentsDirMissing(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{"gen", "-fragments", "/this/path/does/not/exist/anywhere", "-expected", t.TempDir()}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run gen with missing fragments: error = nil, want error")
	}
}
