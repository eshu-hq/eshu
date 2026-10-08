// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The G7/G8 scale rounds enforce rule PD of docs/internal/timing-proof-rules.md
// (from the #7127 PR-3d timing proof), mirroring the PD1-PD5 shape of
// docs/internal/evidence/7127-ledger-retention-timing.sh:
//
//	PD1  declareScaleHost appends `uptime` and `docker ps` to the gate file at
//	     the start and end of the run. The host must be quiet.
//	PD2  waitForScaleQuiet waits for the 1-minute load to fall below the
//	     threshold (default half the CPU count) before each round; the round
//	     records load1 at its start and at its end.
//	PD3  startScaleLoadSampler samples load1 every second during the round and
//	     records the maximum.
//	PD4  a round is valid only if every load sample is below the threshold and
//	     the base-binary canary (the bare_b aggregate through a test binary
//	     built at the base ref) ran within its bound. An invalid round is
//	     re-run; an over-bound canary means the backend was starved.
//	PD5  the loops re-run invalid rounds until the wanted valid count or the
//	     deadline passes; fewer valid rounds means the host could not be
//	     quieted, which is not a code result.
//
// checkScaleLoads and checkScaleCanary are the pure rule; the unit tests in
// scale_load_rule_unit_test.go feed them injected samples, since a quiet host
// cannot be created on demand.
const (
	// scaleLoadThresholdEnv overrides the PD headroom bound (default half
	// the CPU count, read from the machine).
	scaleLoadThresholdEnv = "ESHU_CHANGED_SINCE_LINK_SCALE_LOAD_THRESHOLD"
	// scaleControlMaxSecondsEnv overrides the canary bound (default
	// scaleDefaultControlMaxSeconds).
	scaleControlMaxSecondsEnv = "ESHU_CHANGED_SINCE_LINK_SCALE_CONTROL_MAX_SECONDS"
	// scaleBaseRefEnv pins the ref the canary binary is built from (default
	// the merge base with origin/main, else HEAD).
	scaleBaseRefEnv = "ESHU_CHANGED_SINCE_LINK_SCALE_BASE_REF"
	// scaleCanaryEnv selects the canary entry in the base binary.
	scaleCanaryEnv = "ESHU_CHANGED_SINCE_LINK_SCALE_CANARY"
	// scaleGateFileEnv names the PD1 gate file (unset: no host declaration).
	scaleGateFileEnv = "ESHU_CHANGED_SINCE_LINK_SCALE_GATE_FILE"
)

// scaleDefaultControlMaxSeconds is the canary bound in seconds, derived from
// the committed G7 bare_b spread in
// docs/internal/evidence/7127-link-writer-g7-results.json (max 8.50s, mean
// 5.86, sd 1.24, n=10 on 18 CPUs): 12s sits 3.5s (2.8 sd) above the max, so a
// quiet control passes and a starved one (the #7127 incident's bad control
// ran 8.5x its quiet time) fails. TestScaleCanaryBoundCoversCommittedG7Spread
// locks the derivation.
const scaleDefaultControlMaxSeconds = 12.0

// scaleLoadSamplerTick is the PD3 in-run sampling cadence.
const scaleLoadSamplerTick = time.Second

// scalePD carries the per-process PD inputs for the G7/G8 rounds: the scale
// DSN, the base-binary canary path, and the fixed load and canary bounds.
type scalePD struct {
	dsn        string
	canary     string
	threshold  float64
	controlMax float64
}

// scaleLoadThreshold returns the PD headroom bound: the env override when it
// parses positive, else half the CPU count of this machine.
func scaleLoadThreshold() float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(scaleLoadThresholdEnv)), 64); err == nil && v > 0 {
		return v
	}
	return float64(runtime.NumCPU()) / 2
}

// scaleControlMaxSeconds returns the canary bound: the env override when it
// parses positive, else the derived default.
func scaleControlMaxSeconds() float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(scaleControlMaxSecondsEnv)), 64); err == nil && v > 0 {
		return v
	}
	return scaleDefaultControlMaxSeconds
}

// scaleLoadVerdict is a PD validity decision with the reason an invalid round
// records. Reason is empty when Valid is true.
type scaleLoadVerdict struct {
	Valid  bool
	Reason string
}

// checkScaleLoads applies the PD headroom rule: every sample, start through
// end, must be readable and strictly below the threshold. A one-minute load
// average lags a burst by seconds, so the in-run maximum is what catches the
// spike the start and end samples miss.
func checkScaleLoads(threshold float64, loads []float64) scaleLoadVerdict {
	if threshold <= 0 {
		return scaleLoadVerdict{Reason: fmt.Sprintf("no positive load threshold: %v", threshold)}
	}
	if len(loads) == 0 {
		return scaleLoadVerdict{Reason: "no load samples recorded"}
	}
	max := loads[0]
	for _, load := range loads {
		if load < 0 {
			return scaleLoadVerdict{Reason: "unreadable host load; the round proves nothing"}
		}
		if load > max {
			max = load
		}
	}
	if max >= threshold {
		return scaleLoadVerdict{Reason: fmt.Sprintf("in-run max load %.2f at or above threshold %.2f over %d samples", max, threshold, len(loads))}
	}
	return scaleLoadVerdict{Valid: true}
}

// checkScaleCanary applies the PD4 control rule: the base-binary canary must
// have run, and within its bound. An over-bound control means the backend was
// starved, which invalidates the round on both sides.
func checkScaleCanary(seconds, maxSeconds float64) scaleLoadVerdict {
	if seconds < 0 {
		return scaleLoadVerdict{Reason: "canary control did not run"}
	}
	if maxSeconds <= 0 {
		return scaleLoadVerdict{Reason: fmt.Sprintf("no positive canary bound: %v", maxSeconds)}
	}
	if seconds > maxSeconds {
		return scaleLoadVerdict{Reason: fmt.Sprintf("canary control took %.2fs, over the %.2fs bound; the backend was starved", seconds, maxSeconds)}
	}
	return scaleLoadVerdict{Valid: true}
}

// startScaleLoadSampler takes the start sample synchronously, then samples
// read every tick until stop is called; stop takes the end sample and returns
// every sample, start first and end last. A non-positive tick samples start
// and end only. read must be safe for concurrent use.
func startScaleLoadSampler(read func() float64, tick time.Duration) (stop func() []float64) {
	samples := []float64{read()}
	if tick <= 0 {
		return func() []float64 { return append(samples, read()) }
	}
	var mu sync.Mutex
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				mu.Lock()
				samples = append(samples, read())
				mu.Unlock()
			}
		}
	}()
	return func() []float64 {
		close(done)
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		return append(samples, read())
	}
}

// waitForScaleQuiet polls read every poll until the load is readable and below
// the threshold, calling onWait before each sleep so the caller can record the
// wait. It returns the last load at once when the stop time has passed; a zero
// stop time waits without bound.
func waitForScaleQuiet(read func() float64, threshold float64, poll time.Duration, stopAt time.Time, onWait func(load float64)) float64 {
	for {
		load := read()
		if load >= 0 && load < threshold {
			return load
		}
		if !stopAt.IsZero() && !time.Now().Before(stopAt) {
			return load
		}
		onWait(load)
		if poll > 0 {
			time.Sleep(poll)
		}
	}
}

// resolveScaleBaseRef picks the ref the canary binary is built from: the merge
// base with origin/main, or HEAD when the checkout has no origin/main (a
// same-binary control still measures backend starvation). run executes a
// command and returns its stdout; tests inject a fake.
func resolveScaleBaseRef(run func(name string, args ...string) (string, error)) (string, error) {
	if out, err := run("git", "merge-base", "HEAD", "origin/main"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			return ref, nil
		}
	}
	out, err := run("git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve canary base: %w", err)
	}
	if ref := strings.TrimSpace(out); ref != "" {
		return ref, nil
	}
	return "", errors.New("resolve canary base: empty HEAD")
}

// scaleBaseRef resolves the canary base: the explicit env ref, else the merge
// base with origin/main, else HEAD.
func scaleBaseRef(t *testing.T) string {
	t.Helper()
	if ref := strings.TrimSpace(os.Getenv(scaleBaseRefEnv)); ref != "" {
		return ref
	}
	ref, err := resolveScaleBaseRef(func(name string, args ...string) (string, error) {
		out, err := exec.Command(name, args...).Output()
		return string(out), err
	})
	if err != nil {
		t.Fatalf("scale canary base: %v", err)
	}
	return ref
}

// scaleCanaryTestFiles are the scale-harness test files copied into the base
// worktree so the base binary runs the same canary entry against unchanged
// production code. They must stay free of helpers from the package's other
// _test.go files, which compile from the base tree.
var scaleCanaryTestFiles = []string{"scale_live_test.go", "scale_load_rule_test.go"}

// buildScaleBaseBinary compiles the links test package at baseRef into dir and
// returns the binary path. The base worktree carries the current scale-harness
// files (as the #7127 shell copies its timing test into its base worktree),
// so the canary entry exists whatever the base predates. A failed build is
// fatal: a scale gate that silently runs without its control is worse than no
// run.
func buildScaleBaseBinary(t *testing.T, dir, baseRef string) string {
	t.Helper()
	wt := filepath.Join(dir, "base-wt")
	if out, err := exec.Command("git", "worktree", "add", "--detach", wt, baseRef).CombinedOutput(); err != nil {
		t.Fatalf("scale canary worktree at %s: %v\n%s", baseRef, err, out)
	}
	t.Cleanup(func() {
		if out, err := exec.Command("git", "worktree", "remove", "--force", wt).CombinedOutput(); err != nil {
			t.Errorf("scale canary worktree remove: %v\n%s", err, out)
		}
	})
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("scale canary: runtime.Caller failed")
	}
	srcDir := filepath.Dir(thisFile)
	for _, name := range scaleCanaryTestFiles {
		blob, err := os.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			t.Fatalf("scale canary copy %s: %v", name, err)
		}
		dst := filepath.Join(wt, "go", "internal", "storage", "postgres", "freshness", "links", name)
		if err := os.WriteFile(dst, blob, 0o644); err != nil {
			t.Fatalf("scale canary write %s: %v", dst, err)
		}
	}
	binary := filepath.Join(dir, "scale_before")
	cmd := exec.Command("go", "test", "-c", "-o", binary, "./internal/storage/postgres/freshness/links")
	cmd.Dir = filepath.Join(wt, "go")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scale canary build at %s: %v\n%s", baseRef, err, out)
	}
	return binary
}

// runScaleCanary executes the base binary's TestLinkScaleCanary entry against
// dsn and returns the control's seconds. A failed canary run is fatal: the
// binary is broken, not the host, so re-running the round cannot help. Only
// an over-bound control time invalidates the round (see checkScaleCanary).
func runScaleCanary(t *testing.T, binary, dsn string) float64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run", `^TestLinkScaleCanary$`, "-test.count=1")
	cmd.Env = append(os.Environ(), scaleCanaryEnv+"=1", scaleDSNEnv+"="+dsn)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scale canary run: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "CANARY ")
		if !ok {
			continue
		}
		var payload struct {
			Seconds float64 `json:"seconds"`
		}
		if err := json.Unmarshal([]byte(rest), &payload); err != nil {
			t.Fatalf("scale canary parse %q: %v", rest, err)
		}
		return payload.Seconds
	}
	t.Fatalf("scale canary: no CANARY line in output:\n%s", out)
	return -1
}

// declareScaleHost appends the PD1 host record (`uptime` and `docker ps`) to
// the gate file. Unset gate file: nothing to declare. A write failure is a
// test error: the run would lack provenance.
func declareScaleHost(t *testing.T, label string) {
	t.Helper()
	path := strings.TrimSpace(os.Getenv(scaleGateFileEnv))
	if path == "" {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "== %s %s\n", label, time.Now().UTC().Format(time.RFC3339))
	if out, err := exec.Command("uptime").CombinedOutput(); err == nil {
		b.Write(out)
	} else {
		fmt.Fprintf(&b, "uptime: %v\n", err)
	}
	if out, err := exec.Command("docker", "ps", "--format", "{{.Names}} {{.Status}}").CombinedOutput(); err == nil {
		b.Write(out)
	} else {
		fmt.Fprintf(&b, "docker ps: %v\n", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Errorf("scale gate file %s: %v", path, err)
		return
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(b.String()); err != nil {
		t.Errorf("scale gate file %s: %v", path, err)
	}
}

// TestLinkScaleCanary is the PD4 control entry, executed by runScaleCanary in
// the base binary: one bare_b aggregate at the link's transaction settings,
// rolled back, with its seconds on a CANARY line. It skips unless
// ESHU_CHANGED_SINCE_LINK_SCALE_CANARY=1; a selected run without a DSN is an
// operator error and fails.
func TestLinkScaleCanary(t *testing.T) {
	if os.Getenv(scaleCanaryEnv) == "" {
		t.Skipf("set %s=1 to run the scale canary entry", scaleCanaryEnv)
	}
	dsn := strings.TrimSpace(os.Getenv(scaleDSNEnv))
	if dsn == "" {
		t.Fatalf("canary selected without %s", scaleDSNEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = raw.Close() }()
	target := scaleTargets[0]
	f1 := scaleGeneration(t, ctx, raw, target, "F1")
	tx, err := raw.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, s := range []string{`SET LOCAL work_mem = '256MB'`, `SET LOCAL plan_cache_mode = force_custom_plan`, `SET LOCAL statement_timeout = '120s'`} {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	began := time.Now()
	if _, err := tx.ExecContext(ctx, bareAggregateSQL(t), target, f1); err != nil {
		t.Fatalf("bare_b: %v", err)
	}
	fmt.Printf("CANARY {\"seconds\": %.6f}\n", time.Since(began).Seconds())
}

func hostLoad1() float64 {
	var out []byte
	var err error
	if runtime.GOOS == "darwin" {
		out, err = exec.Command("sysctl", "-n", "vm.loadavg").Output()
	} else {
		out, err = os.ReadFile("/proc/loadavg")
	}
	if err != nil {
		return -1
	}
	fields := strings.Fields(strings.Trim(string(out), "{} \n"))
	if len(fields) == 0 {
		return -1
	}
	// Unparseable output reads as unreadable, not as zero: a silent 0.0
	// would pass the headroom check for a host that proved nothing.
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return -1
	}
	return v
}
