// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
)

// TestRunReportsChildOnlyPeakRSS is the regression for rusage inheritance:
// Go spawns with CLONE_VFORK|CLONE_VM, so the child's ru_maxrss reports the
// parent's RSS. The parent here holds 512 MiB; a child-only measurement of
// a fake that touches 32 MiB must land between the two.
func TestRunReportsChildOnlyPeakRSS(t *testing.T) {
	engine, _ := newTestEngine(t, nil)
	ballast := make([]byte, 512<<20)
	for i := range ballast {
		ballast[i] = 1
	}

	run, err := engine.run(context.Background(), fakePNG(t, 20, 10, "hold"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.peakRSS < 32<<20 {
		t.Fatalf("peak RSS %d bytes, want at least the child's 32 MiB", run.peakRSS)
	}
	if run.peakRSS >= 512<<20 {
		t.Fatalf("peak RSS %d bytes reflects the 512 MiB parent, not the child", run.peakRSS)
	}
	if ballast[len(ballast)-1] != 1 {
		t.Fatal("ballast collected early")
	}
}

// TestRecognizeAppliesAddressSpaceLimitToChild proves RLIMIT_AS reaches the
// child by reading the limit the child itself observes. Whether an
// allocation then fails depends on the runtime (a Go fake pre-reserves its
// arenas), so allocation failure is proven against real tesseract in
// scripts/prove-ocr-sandbox.sh instead.
func TestRecognizeAppliesAddressSpaceLimitToChild(t *testing.T) {
	t.Parallel()
	const limit = int64(1) << 61
	engine, _ := newTestEngine(t, func(cfg *Config) { cfg.MaxAddressSpaceBytes = limit })

	result, err := engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, "limits")})
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(result.Regions) != 1 || result.Regions[0].Text != "AS="+strconv.FormatInt(limit, 10) {
		t.Fatalf("child observed %+v, want AS=%d", result.Regions, limit)
	}
}

func TestRunClassifiesExternalSignalAsSignaled(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)

	_, err := engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, "selfkill")})
	assertOutcome(t, err, OutcomeSignaled)
	if !strings.Contains(err.Error(), "SIGKILL") {
		t.Fatalf("signaled error does not name the signal: %v", err)
	}
}

func TestParseVmHWM(t *testing.T) {
	t.Parallel()
	cases := map[string]int64{
		"Name:\ttesseract\nVmHWM:\t   77292 kB\nVmRSS:\t 1 kB\n": 77292 * 1024,
		"VmHWM:\tgarbage kB\n": 0,
		"VmHWM:\t12 MB\n":      0,
		"Name:\tzombie\n":      0,
	}
	for input, want := range cases {
		if got := parseVmHWM([]byte(input)); got != want {
			t.Fatalf("parseVmHWM(%q) = %d, want %d", input, got, want)
		}
	}
}
