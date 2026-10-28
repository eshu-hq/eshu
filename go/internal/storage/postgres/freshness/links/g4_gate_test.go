// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// g4Limit is gate G4's bound for the 600-key link (#7127 ruling 8.4), judged
// on one sample. #9275 does not move it: no retry, no median, no wider bound.
const g4Limit = 100 * time.Millisecond

// g4Report is everything the G4 gate prints about its one sample, so a slow
// hosted-runner sample names its phase and its host instead of a bare total.
type g4Report struct {
	// samples is the number of timed links the verdict rests on. It is 1: the
	// gate judges a single sample, and the report says so.
	samples int
	total   time.Duration
	phases  linksfreshnessstore.LinkPhases
	// control is the durable-commit probe: single-row autocommit inserts
	// taken immediately before the gated link. It is logged, never gated.
	control []time.Duration
	// settings is the SHOW output for fsync, synchronous_commit and
	// shared_buffers.
	settings string
	// walDelta is the pg_stat_wal and pg_stat_checkpointer movement across the
	// control and the gated link.
	walDelta string
	// host is the runner's CPU model, load and pressure, where /proc has them.
	host string
}

// String renders the one diagnostic line the gate logs and appends to its
// failure.
func (r g4Report) String() string {
	var control []string
	for _, d := range r.control {
		control = append(control, d.Round(10*time.Microsecond).String())
	}
	return fmt.Sprintf("G4 samples=%d total=%s phases[ensure_cursor=%s begin=%s locks=%s statement=%s advance=%s commit=%s] "+
		"control_commits[%s] settings[%s] wal[%s] host[%s]",
		r.samples, r.total,
		r.phases.EnsureCursor, r.phases.Begin, r.phases.Locks, r.phases.Statement, r.phases.Advance, r.phases.Commit,
		strings.Join(control, " "), r.settings, r.walDelta, r.host)
}

// g4Verdict returns the gate's failure text, or "" when the sample passes.
// The comparison is strict: a link of exactly 100 ms passes.
func g4Verdict(r g4Report) string {
	if r.total <= g4Limit {
		return ""
	}
	return fmt.Sprintf("600-key link took %s, gate G4 allows %s on one sample (samples=%d; host load may explain it; see the evidence file): %s",
		r.total, g4Limit, r.samples, r)
}

// TestG4GateStillFailsAt100msPlus keeps the G4 bound where #7127 ruling 8.4
// put it: 100 ms on one sample. 100 ms passes and anything above fails, down
// to a single nanosecond, so a raised bound or a median cannot slip in. The
// failure text carries samples=1 and every phase name.
func TestG4GateStillFailsAt100msPlus(t *testing.T) {
	report := func(total time.Duration) g4Report {
		return g4Report{samples: 1, total: total, phases: linksfreshnessstore.LinkPhases{Commit: total / 2}}
	}
	for _, tc := range []struct {
		name  string
		total time.Duration
		fails bool
	}{
		{"well under", 13 * time.Millisecond, false},
		{"exactly 100ms", 100 * time.Millisecond, false},
		{"one nanosecond over", 100*time.Millisecond + time.Nanosecond, true},
		{"101ms", 101 * time.Millisecond, true},
		{"the hosted outlier, 181.64ms", 181640 * time.Microsecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := g4Verdict(report(tc.total))
			if (got != "") != tc.fails {
				t.Fatalf("g4Verdict(%s) = %q, want failure=%v", tc.total, got, tc.fails)
			}
			if !tc.fails {
				return
			}
			for _, want := range []string{
				"samples=1", "ensure_cursor=", "begin=", "locks=", "statement=", "advance=", "commit=",
				"control_commits[", "settings[", "wal[", "host[",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("failure text lacks %q:\n%s", want, got)
				}
			}
		})
	}
}

// cpuModelName returns the first "model name" of /proc/cpuinfo text, or ""
// when there is none (non-x86 kernels name it "Model" or "Hardware").
func cpuModelName(cpuinfo string) string {
	for _, line := range strings.Split(cpuinfo, "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(key) == "model name" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func TestCPUModelName(t *testing.T) {
	const cpuinfo = "processor\t: 0\nvendor_id\t: AuthenticAMD\nmodel name\t: AMD EPYC 7763 64-Core Processor\ncpu MHz\t\t: 2445.426\n\nprocessor\t: 1\nmodel name\t: other\n"
	if got := cpuModelName(cpuinfo); got != "AMD EPYC 7763 64-Core Processor" {
		t.Fatalf("cpuModelName = %q", got)
	}
	if got := cpuModelName("processor\t: 0\n"); got != "" {
		t.Fatalf("cpuModelName without a model name = %q, want empty", got)
	}
}

// hostSnapshot describes the machine the gate ran on, from /proc when it is
// there (Linux hosted runners). Every missing file reads "n/a": the snapshot
// is evidence for a slow sample, not a requirement of a fast one.
func hostSnapshot() string {
	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			return "n/a"
		}
		return strings.Join(strings.Fields(strings.ReplaceAll(string(b), "\n", " | ")), " ")
	}
	model := "n/a"
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		if m := cpuModelName(string(b)); m != "" {
			model = m
		}
	}
	return fmt.Sprintf("cpu=%q loadavg=%q psi_cpu=%q psi_io=%q",
		model, read("/proc/loadavg"), read("/proc/pressure/cpu"), read("/proc/pressure/io"))
}

// TestG4ReportGapsNamesEveryEmptyDiagnostic pins the guard the live G4 test
// runs on the report timedG4Link returns (#9275): an empty report names every
// missing diagnostic, a populated one names none, and a single empty field is
// caught on its own, so a refactor that drops one diagnostic cannot stay green.
func TestG4ReportGapsNamesEveryEmptyDiagnostic(t *testing.T) {
	populated := func() g4Report {
		return g4Report{
			samples:  1,
			total:    13 * time.Millisecond,
			control:  make([]time.Duration, g4ControlInserts),
			settings: "fsync=on synchronous_commit=on shared_buffers=128MB",
			walDelta: "lsn_bytes=8192",
			host:     `cpu="n/a" loadavg="n/a" psi_cpu="n/a" psi_io="n/a"`,
		}
	}
	if gaps := g4ReportGaps(populated()); len(gaps) != 0 {
		t.Fatalf("populated report has gaps: %v", gaps)
	}
	if gaps := g4ReportGaps(g4Report{}); len(gaps) != 4 {
		t.Fatalf("empty report gaps = %v, want control, settings, wal and host", gaps)
	}
	for name, mutate := range map[string]func(*g4Report){
		"control":  func(r *g4Report) { r.control = r.control[:g4ControlInserts-1] },
		"settings": func(r *g4Report) { r.settings = "" },
		"wal":      func(r *g4Report) { r.walDelta = "" },
		"host":     func(r *g4Report) { r.host = "" },
	} {
		r := populated()
		mutate(&r)
		gaps := g4ReportGaps(r)
		if len(gaps) != 1 || !strings.Contains(gaps[0], name) {
			t.Errorf("report with only %s emptied: gaps = %v", name, gaps)
		}
	}
	requireG4ReportPopulated(t, populated())
}
