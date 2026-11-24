// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	envFakeVetMode    = "ESHU_TEST_FAKE_VET_MODE"
	envFakeVetCount   = "ESHU_TEST_FAKE_VET_COUNT_FILE"
	envFakeVetPayload = "ESHU_TEST_FAKE_VET_PAYLOAD"
	envFakeVetExit    = "ESHU_TEST_FAKE_VET_EXIT"
)

// TestMain lets the compiled test binary act as a deterministic fake `go tool
// vet` subprocess when ESHU_TEST_FAKE_VET_MODE=1 is set in the environment.
func TestMain(m *testing.M) {
	if os.Getenv(envFakeVetMode) == "1" {
		os.Exit(runFakeVet(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func runFakeVet(args []string) int {
	if countFile := os.Getenv(envFakeVetCount); countFile != "" {
		f, err := os.OpenFile(countFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "fake vet count file: %v\n", err)
			return 99
		}
		_, _ = fmt.Fprintln(f, strings.Join(args, " "))
		_ = f.Close()
	}

	if len(args) > 0 && strings.HasSuffix(args[len(args)-1], ".cfg") {
		raw, err := os.ReadFile(args[len(args)-1])
		if err == nil {
			var cfg vetConfig
			if json.Unmarshal(raw, &cfg) == nil && cfg.VetxOutput != "" {
				payload := os.Getenv(envFakeVetPayload)
				if payload == "" {
					payload = "vetx-default-payload"
				}
				if err := os.WriteFile(cfg.VetxOutput, []byte(payload), 0o600); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "fake vet write vetx: %v\n", err)
					return 98
				}
			}
		}
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "fake-vet:%s\n", strings.Join(args, " "))
	}

	if exitStr := os.Getenv(envFakeVetExit); exitStr != "" {
		code, err := strconv.Atoi(exitStr)
		if err == nil && code != 0 {
			_, _ = fmt.Fprintln(os.Stderr, "fake-vet diagnostic failure")
			return code
		}
	}
	return 0
}

type fakeVetHarness struct {
	t         *testing.T
	binPath   string
	countFile string
	cacheDir  string
	goCache   string
}

func newFakeVetHarness(t *testing.T) *fakeVetHarness {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	dir := t.TempDir()
	h := &fakeVetHarness{
		t:         t,
		binPath:   exe,
		countFile: filepath.Join(dir, "invocations.log"),
		cacheDir:  filepath.Join(dir, "vetx-cache"),
		goCache:   filepath.Join(dir, "gocache"),
	}
	if err := os.MkdirAll(h.goCache, 0o750); err != nil {
		t.Fatalf("mkdir gocache: %v", err)
	}
	t.Setenv(envFakeVetMode, "1")
	t.Setenv(envFakeVetCount, h.countFile)
	t.Setenv(envFakeVetPayload, "vetx-bytes-v1")
	t.Setenv(envFakeVetExit, "0")
	return h
}

func (h *fakeVetHarness) envMap(overrides map[string]string) func(string) string {
	base := map[string]string{
		envRealTool: h.binPath,
		envCacheDir: h.cacheDir,
		envGoCache:  h.goCache,
	}
	for k, v := range overrides {
		base[k] = v
	}
	return func(k string) string {
		if v, ok := base[k]; ok {
			return v
		}
		return os.Getenv(k)
	}
}

func (h *fakeVetHarness) invocations() int {
	h.t.Helper()
	raw, err := os.ReadFile(h.countFile)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		h.t.Fatalf("read count file: %v", err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func writeCfg(t *testing.T, dir string, cfg *vetConfig) string {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	path := filepath.Join(dir, "vet.cfg")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	return path
}

func writeTestFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestRunMissingRealToolReturns2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-V=full"}, func(string) string { return "" }, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), envRealTool) {
		t.Fatalf("stderr = %q, want mention of %s", stderr.String(), envRealTool)
	}
}

func TestRunPassthroughNonCfgArgs(t *testing.T) {
	h := newFakeVetHarness(t)
	for _, args := range [][]string{{"-V=full"}, {"-flags"}} {
		var stdout, stderr bytes.Buffer
		code := run(args, h.envMap(nil), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("args %v exit code = %d, want 0 (stderr=%q)", args, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), args[0]) {
			t.Fatalf("args %v stdout = %q, want %q", args, stdout.String(), args[0])
		}
	}
	if got := h.invocations(); got != 2 {
		t.Fatalf("real vet invocations = %d, want 2", got)
	}
}

func TestRunPassthroughVetxOnlyFalseNeverCached(t *testing.T) {
	h := newFakeVetHarness(t)
	src := writeTestFile(t, filepath.Join(t.TempDir(), "target.go"), "package target\n")
	work := filepath.Join(t.TempDir(), "b001")
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatal(err)
	}
	cfg := &vetConfig{
		ID:         "example.invalid/target",
		ImportPath: "example.invalid/target",
		GoVersion:  "go1.27",
		GoFiles:    []string{src},
		VetxOnly:   false,
		VetxOutput: filepath.Join(work, "vet.out"),
	}
	cfgPath := writeCfg(t, work, cfg)

	t.Setenv(envFakeVetExit, "1")
	var stdout, stderr bytes.Buffer
	code := run([]string{cfgPath}, h.envMap(nil), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "fake-vet diagnostic failure") {
		t.Fatalf("stderr = %q, want fake vet diagnostic", stderr.String())
	}

	// Even on success, VetxOnly == false must run real vet every time.
	t.Setenv(envFakeVetExit, "0")
	for i := 0; i < 2; i++ {
		stdout.Reset()
		stderr.Reset()
		if code := run([]string{cfgPath}, h.envMap(nil), &stdout, &stderr); code != 0 {
			t.Fatalf("run %d exit code = %d, want 0", i, code)
		}
	}
	if got := h.invocations(); got != 3 {
		t.Fatalf("real vet invocations = %d, want 3 (never cached)", got)
	}
}

func TestRunVetxOnlyMissThenHitAcrossDifferentWorkDirs(t *testing.T) {
	h := newFakeVetHarness(t)
	src := writeTestFile(t, filepath.Join(t.TempDir(), "dep.go"), "package dep\n")

	work1 := filepath.Join(t.TempDir(), "b010")
	pfile1 := writeTestFile(t, filepath.Join(work1, "_pkg_.a"), "compiled-export-data-1")
	pvetx1 := writeTestFile(t, filepath.Join(work1, "dep.vetx"), "dep-facts-1")
	out1 := filepath.Join(work1, "vet.out")
	stdout1 := filepath.Join(work1, "vet.stdout")
	cfg1 := writeCfg(t, work1, &vetConfig{
		ID:          "example.invalid/dep",
		Compiler:    "gc",
		ImportPath:  "example.invalid/dep",
		GoVersion:   "go1.27",
		GoFiles:     []string{src},
		ImportMap:   map[string]string{"fmt": "fmt"},
		PackageFile: map[string]string{"fmt": pfile1},
		PackageVetx: map[string]string{"fmt": pvetx1},
		VetxOnly:    true,
		VetxOutput:  out1,
		Stdout:      stdout1,
	})

	t.Setenv(envFakeVetPayload, "cached-vetx-content-42")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-tags", "tag_alpha", "-printf=true", cfg1}, h.envMap(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("first run exit = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	if got := h.invocations(); got != 1 {
		t.Fatalf("invocations after miss = %d, want 1", got)
	}

	// Second invocation in a brand new $WORK/b099 with a DIFFERENT -tags value,
	// identical artifact contents, and a Go 1.27-style Stdout file path must HIT
	// and materialize empty Stdout.
	work2 := filepath.Join(t.TempDir(), "b099")
	pfile2 := writeTestFile(t, filepath.Join(work2, "_pkg_.a"), "compiled-export-data-1")
	pvetx2 := writeTestFile(t, filepath.Join(work2, "dep.vetx"), "dep-facts-1")
	out2 := filepath.Join(work2, "vet.out")
	stdout2 := filepath.Join(work2, "vet.stdout")
	cfg2 := writeCfg(t, work2, &vetConfig{
		ID:          "example.invalid/dep",
		Compiler:    "gc",
		ImportPath:  "example.invalid/dep",
		GoVersion:   "go1.27",
		GoFiles:     []string{src},
		ImportMap:   map[string]string{"fmt": "fmt"},
		PackageFile: map[string]string{"fmt": pfile2},
		PackageVetx: map[string]string{"fmt": pvetx2},
		VetxOnly:    true,
		VetxOutput:  out2,
		Stdout:      stdout2,
	})

	t.Setenv(envFakeVetPayload, "should-not-be-written")
	if code := run([]string{"-tags=tag_beta", "-printf=true", cfg2}, h.envMap(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("second run exit = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	if got := h.invocations(); got != 1 {
		t.Fatalf("invocations after hit = %d, want 1", got)
	}
	gotBytes, err := os.ReadFile(out2)
	if err != nil {
		t.Fatalf("read cached vetx output: %v", err)
	}
	if string(gotBytes) != "cached-vetx-content-42" {
		t.Fatalf("cached vetx bytes = %q, want %q", string(gotBytes), "cached-vetx-content-42")
	}
	gotStdout, err := os.ReadFile(stdout2)
	if err != nil {
		t.Fatalf("read hit stdout file: %v", err)
	}
	if len(gotStdout) != 0 {
		t.Fatalf("hit stdout file len = %d, want 0", len(gotStdout))
	}
}

func TestRunInvalidatesOnArtifactSourceFlagAndVersionChanges(t *testing.T) {
	h := newFakeVetHarness(t)
	srcDir := t.TempDir()
	src := writeTestFile(t, filepath.Join(srcDir, "pkg.go"), "package pkg\n")

	makeAndRun := func(flags []string, goVersion, pfileBody, pvetxBody string) int {
		t.Helper()
		work := filepath.Join(t.TempDir(), "b007")
		pfile := writeTestFile(t, filepath.Join(work, "_pkg_.a"), pfileBody)
		pvetx := writeTestFile(t, filepath.Join(work, "dep.vetx"), pvetxBody)
		cfgPath := writeCfg(t, work, &vetConfig{
			ID:          "example.invalid/pkg",
			ImportPath:  "example.invalid/pkg",
			GoVersion:   goVersion,
			GoFiles:     []string{src},
			PackageFile: map[string]string{"dep": pfile},
			PackageVetx: map[string]string{"dep": pvetx},
			VetxOnly:    true,
			VetxOutput:  filepath.Join(work, "vet.out"),
		})
		args := append(append([]string{}, flags...), cfgPath)
		var stdout, stderr bytes.Buffer
		if code := run(args, h.envMap(nil), &stdout, &stderr); code != 0 {
			t.Fatalf("run(%v) exit = %d, want 0 (stderr=%q)", args, code, stderr.String())
		}
		return h.invocations()
	}

	if got := makeAndRun([]string{"-printf=true"}, "go1.27", "pkg-v1", "vetx-v1"); got != 1 {
		t.Fatalf("initial miss invocations = %d, want 1", got)
	}
	// Identical inputs -> hit.
	if got := makeAndRun([]string{"-printf=true"}, "go1.27", "pkg-v1", "vetx-v1"); got != 1 {
		t.Fatalf("repeat hit invocations = %d, want 1", got)
	}
	// PackageFile content changed -> miss.
	if got := makeAndRun([]string{"-printf=true"}, "go1.27", "pkg-v2", "vetx-v1"); got != 2 {
		t.Fatalf("after PackageFile change invocations = %d, want 2", got)
	}
	// PackageVetx content changed -> miss.
	if got := makeAndRun([]string{"-printf=true"}, "go1.27", "pkg-v2", "vetx-v2"); got != 3 {
		t.Fatalf("after PackageVetx change invocations = %d, want 3", got)
	}
	// Flag args changed -> miss.
	if got := makeAndRun([]string{"-printf=false"}, "go1.27", "pkg-v2", "vetx-v2"); got != 4 {
		t.Fatalf("after flag change invocations = %d, want 4", got)
	}
	// GoVersion changed -> miss.
	if got := makeAndRun([]string{"-printf=false"}, "go1.26", "pkg-v2", "vetx-v2"); got != 5 {
		t.Fatalf("after GoVersion change invocations = %d, want 5", got)
	}
	// Source GoFile modified -> miss.
	writeTestFile(t, src, "package pkg\nfunc Changed() {}\n")
	future := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(src, future, future); err != nil {
		t.Fatalf("chtimes src: %v", err)
	}
	if got := makeAndRun([]string{"-printf=false"}, "go1.26", "pkg-v2", "vetx-v2"); got != 6 {
		t.Fatalf("after GoFiles change invocations = %d, want 6", got)
	}
}

func TestRunWorkGeneratedGoFilesHitWhenIdenticalAndMissWhenChanged(t *testing.T) {
	h := newFakeVetHarness(t)
	src := writeTestFile(t, filepath.Join(t.TempDir(), "cgo_pkg.go"), "package cgopkg\n")

	runWithCgo := func(subdir, generatedBody string) int {
		t.Helper()
		workRoot := t.TempDir()
		actionDir := filepath.Join(workRoot, subdir)
		cgoFile := writeTestFile(t, filepath.Join(actionDir, "_cgo_gotypes.go"), generatedBody)
		cfgPath := writeCfg(t, actionDir, &vetConfig{
			ID:         "example.invalid/cgopkg",
			ImportPath: "example.invalid/cgopkg",
			GoVersion:  "go1.27",
			GoFiles:    []string{src, cgoFile},
			VetxOnly:   true,
			VetxOutput: filepath.Join(actionDir, "vet.out"),
		})
		var stdout, stderr bytes.Buffer
		if code := run([]string{cfgPath}, h.envMap(nil), &stdout, &stderr); code != 0 {
			t.Fatalf("run exit = %d, want 0 (stderr=%q)", code, stderr.String())
		}
		return h.invocations()
	}

	if got := runWithCgo("b001", "package cgopkg\nvar _Ctype_int int\n"); got != 1 {
		t.Fatalf("initial cgo miss = %d, want 1", got)
	}
	// Different $WORK and different bNNN subdir, identical _cgo_gotypes.go content -> HIT.
	if got := runWithCgo("b042", "package cgopkg\nvar _Ctype_int int\n"); got != 1 {
		t.Fatalf("identical cgo content across $WORK = %d, want 1 (hit)", got)
	}
	// Different _cgo_gotypes.go content (e.g. build tag altered cgo output) -> MISS.
	if got := runWithCgo("b043", "package cgopkg\nvar _Ctype_long int64\n"); got != 2 {
		t.Fatalf("changed cgo content = %d, want 2 (miss)", got)
	}
}
