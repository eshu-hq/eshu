// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow/parity"
)

// Leg is one side under test: its backend, its database observer, and which
// §3 steps apply to it.
type Leg struct {
	// Name is "in-tree" or "satellite".
	Name     string
	Backend  parity.Backend
	Observer Observer
	// Headroom may be nil: every headroom field is then NOT_AVAILABLE.
	Headroom HeadroomSource
	// Pin is true where ValidateAndPinClaimForCommit exists (satellite).
	Pin bool
	// Advisory is true where guarded admission exists (in-tree).
	Advisory bool
	// Quota is true when the leg enforces Spec.QuotaBudget on Options.QuotaKind.
	Quota bool
	// HarnessReaper makes the harness call ReapExpiredClaims at
	// reap_interval. The live leg leaves it false: the reap route is
	// global, so only the deployed reaper may run there.
	HarnessReaper bool
	// ServerLatency describes the server-side claim latency source, or why
	// none exists (the v0.1.1 claim plane has no latency histogram).
	ServerLatency string
	// DatabaseName is the leg database actually connected (the in-tree
	// generated disposable name, or the satellite database).
	DatabaseName string
	// ClockSkew is the lease-boundary tolerance between harness and server.
	ClockSkew time.Duration
}

// Mix is the §5 weighted operation mix. End weights apply to every claim on
// the main kind; the remaining shares are independent seeded draws.
type Mix struct {
	Complete      float64 `json:"complete"`
	Release       float64 `json:"release"`
	FailRetryable float64 `json:"fail_retryable"`
	FailTerminal  float64 `json:"fail_terminal"`
	HeartbeatSkip float64 `json:"heartbeat_skip"`
	LongHold      float64 `json:"long_hold"`
	Pin           float64 `json:"pin"`
	PinOutlive    float64 `json:"pin_outlive"`
	ZombiePin     float64 `json:"zombie_pin"`
	Replay        float64 `json:"replay"`
	QuotaShare    float64 `json:"quota_share"`
}

// DefaultMix is the spec §5 mix: 60/15/15/10 endings and a 5% heartbeat
// skip. The other shares are the harness's seeded readings of "a seeded
// share".
func DefaultMix() Mix {
	return Mix{
		Complete: 0.60, Release: 0.15, FailRetryable: 0.15, FailTerminal: 0.10,
		HeartbeatSkip: 0.05, LongHold: 0.02, Pin: 0.10, PinOutlive: 0.02,
		ZombiePin: 0.50, Replay: 0.10, QuotaShare: 0.10,
	}
}

// Options are harness knobs outside §2. Zero values take the defaults
// noted on each field.
type Options struct {
	// CollectorKind is the main backlog kind (default terraform_state).
	CollectorKind scope.CollectorKind
	// QuotaKind carries the quota workload (default package_registry).
	QuotaKind scope.CollectorKind
	// SampleInterval is the sampler period (default 1s, spec §6).
	SampleInterval time.Duration
	// CensusEvery and FencingEvery run the heavier checks every N samples
	// (default 10 each); both also run at the end.
	CensusEvery  int
	FencingEvery int
	// SamplerDisabled turns the periodic sampler off. Only the overhead
	// measurement uses it; such a run is never a proof run.
	SamplerDisabled bool
	IdlePoll        time.Duration // default 25ms
	RequeueDelay    time.Duration // default 5s, workflow defaults
	ReapLimit       int           // default 100, workflow defaults
	CallTimeout     time.Duration // default 30s
	DrainTimeout    time.Duration // default wall-clock cap minus window
	EnqueueBatch    int           // default 250 (claim plane body limit 1 MiB)
	DeleteBatch     int           // default 1000
	// StatsWait bounds the passive wait after seeding for autovacuum to
	// analyze workflow_work_items (default 5m). A freshly bulk-loaded table
	// has no planner statistics, and the claim statement then runs for
	// seconds per call; production tables carry statistics, so the window
	// starts from that state. The wait and its outcome are reported.
	StatsWait time.Duration
	// AbortFile, when set, is polled each sample; its existence aborts the
	// run (the live CPU/memory watch writes it).
	AbortFile          string
	AdvisoryGoroutines int           // default 25, spec §5 step 6
	AdvisoryItems      int           // default 4 targets per round
	AdvisoryInterval   time.Duration // default window/6
	ReplayTimeout      time.Duration // injected first-call timeout, default 1ms
	MaxSamples         int           // samples kept in the report, default 600
	Mix                Mix
	// CoreResidue counts core rows carrying the prefix (third zero). Nil
	// records NOT_CHECKED on live and NOT_APPLICABLE on local.
	CoreResidue func(context.Context) (int, error)
	// Logf receives progress lines; nil discards them.
	Logf func(format string, args ...any)
}

func (o Options) withDefaults(spec Spec) Options {
	if o.CollectorKind == "" {
		o.CollectorKind = "terraform_state"
	}
	if o.QuotaKind == "" {
		o.QuotaKind = "package_registry"
	}
	setDuration(&o.SampleInterval, time.Second)
	setInt(&o.CensusEvery, 10)
	setInt(&o.FencingEvery, 10)
	setDuration(&o.IdlePoll, 25*time.Millisecond)
	setDuration(&o.RequeueDelay, 5*time.Second)
	setInt(&o.ReapLimit, 100)
	setDuration(&o.CallTimeout, 30*time.Second)
	setDuration(&o.DrainTimeout, spec.WallClockCap-spec.Window)
	setInt(&o.EnqueueBatch, 250)
	setInt(&o.DeleteBatch, 1000)
	setDuration(&o.StatsWait, 5*time.Minute)
	setInt(&o.AdvisoryGoroutines, 25)
	setInt(&o.AdvisoryItems, 4)
	setDuration(&o.AdvisoryInterval, spec.Window/6)
	setDuration(&o.ReplayTimeout, time.Millisecond)
	setInt(&o.MaxSamples, 600)
	if o.Mix == (Mix{}) {
		o.Mix = DefaultMix()
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	return o
}

func setDuration(d *time.Duration, fallback time.Duration) {
	if *d <= 0 {
		*d = fallback
	}
}

func setInt(n *int, fallback int) {
	if *n <= 0 {
		*n = fallback
	}
}
