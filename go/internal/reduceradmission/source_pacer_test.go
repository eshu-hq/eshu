// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"errors"
	"testing"
	"time"
)

func pacerWith(checker reducerAdmissionSourceChecker) *SourcePacer {
	return &SourcePacer{writer: reducerAdmissionWriter{
		sourceChecker: checker,
		sourceConfig:  admissionSourceTestConfig(),
	}}
}

// TestSourcePacerReportsSlowedSourceWithGrantDelay proves the projector's
// pre-write consult: a slowed source reports paced with the verdict's delay,
// a clear source reports not paced, and an empty source skips the consult.
func TestSourcePacerReportsSlowedSourceWithGrantDelay(t *testing.T) {
	t.Parallel()

	checker := &pacedChecker{retryAfter: 7 * time.Second, slowedRounds: 1}
	pacer := pacerWith(checker)

	if paced, after := pacer.SourcePaced(context.Background(), "git"); !paced || after != 7*time.Second {
		t.Fatalf("SourcePaced(slowed) = (%v, %s), want (true, 7s)", paced, after)
	}
	if paced, _ := pacer.SourcePaced(context.Background(), "git"); paced {
		t.Fatal("SourcePaced(cleared) = true, want false after release")
	}
	calls := checker.calls
	if paced, _ := pacer.SourcePaced(context.Background(), "  "); paced || checker.calls != calls {
		t.Fatal("an unattributed source must skip the consult and report not paced")
	}
}

// TestSourcePacerFailsOpen proves an unreadable signal and a cancelled
// context report not paced, so the projector never blocks on a signal outage.
func TestSourcePacerFailsOpen(t *testing.T) {
	t.Parallel()

	failing := pacerWith(&failingSourceChecker{failed: map[string]error{"git": errors.New("statement timeout")}})
	if paced, _ := failing.SourcePaced(context.Background(), "git"); paced {
		t.Fatal("consult error reported paced, want fail open")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	canceling := pacerWith(&failingSourceChecker{failed: map[string]error{"git": context.Canceled}})
	if paced, _ := canceling.SourcePaced(cancelled, "git"); paced {
		t.Fatal("cancelled context reported paced, want not paced")
	}
}

// TestNewSourcePacerIsNilWhenLegDisabled proves a deployment without the
// ESHU_SOURCE_BACKPRESSURE_* thresholds gets no pacer, so nothing is consulted
// on the projector path, and a partial set is a startup error.
func TestNewSourcePacerIsNilWhenLegDisabled(t *testing.T) {
	t.Parallel()

	pacer, err := NewSourcePacer(nil, func(string) string { return "" }, nil, nil)
	if err != nil || pacer != nil {
		t.Fatalf("NewSourcePacer(empty env) = (%v, %v), want (nil, nil)", pacer, err)
	}
	partial := func(key string) string {
		if key == sourceBackpressureEngageAgeEnv {
			return "1h"
		}
		return ""
	}
	if _, err := NewSourcePacer(nil, partial, nil, nil); err == nil {
		t.Fatal("NewSourcePacer(partial thresholds) error = nil, want a startup error")
	}
}
