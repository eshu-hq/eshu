// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"strings"
	"testing"
	"time"
)

// localEnv is a complete local-target §2 field set.
func localEnv() map[string]string {
	return map[string]string{
		EnvTarget:             "local",
		EnvDigest:             "sha256:e1b564afa5f72167e3911aea7c881973c74bab123183c03d9d6af5a2d80e47a4",
		EnvIACEnablePR:        NotApplicableLocal,
		EnvIACRevertPR:        NotApplicableLocal,
		EnvAuthority:          NotApplicableLocal,
		EnvWorkerCounts:       "3",
		EnvWorkerMultiple:     "1",
		EnvBacklogItems:       "400",
		EnvBacklogScopes:      "20",
		EnvQuotaBudget:        "5",
		EnvWindowSeconds:      "10",
		EnvWallClockCap:       "5m",
		EnvStarvationBound:    "5",
		EnvLockWaitBoundMS:    "2000",
		EnvPGMaxConnections:   "80",
		EnvPGMaxLagSeconds:    NotApplicableLocal,
		EnvPGMaxCPUMillicores: NotApplicableLocal,
		EnvPGMaxMemoryMi:      NotApplicableLocal,
		EnvPGMaxLockWaiters:   "5",
		EnvBaselineThroughput: "892.8 in-tree / 894.5 satellite ops/s",
		EnvSeed:               "1",
	}
}

func lookup(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

func TestLoadSpecAcceptsCompleteLocalFields(t *testing.T) {
	spec, err := LoadSpec(lookup(localEnv()))
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if spec.Workers() != 3 {
		t.Fatalf("workers = %d, want 3", spec.Workers())
	}
	if spec.LeaseTTL != 60*time.Second || spec.HeartbeatInterval != 20*time.Second || spec.ReapInterval != 20*time.Second {
		t.Fatalf("defaults = %s/%s/%s, want 60s/20s/20s", spec.LeaseTTL, spec.HeartbeatInterval, spec.ReapInterval)
	}
	if spec.FullSize() {
		t.Fatalf("400 items over 20 scopes must not report full size")
	}
}

// TestLoadSpecFailsClosedOnEveryMissingField removes each required §2 field
// in turn: a blank field stops the run (spec §2), and the error names it.
func TestLoadSpecFailsClosedOnEveryMissingField(t *testing.T) {
	for key := range localEnv() {
		env := localEnv()
		delete(env, key)
		_, err := LoadSpec(lookup(env))
		if err == nil {
			t.Fatalf("missing %s: LoadSpec accepted the field set", key)
		}
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("missing %s: error %q does not name the field", key, err)
		}
	}
}

func TestLoadSpecRejectsLocalSentinelsOnLiveTarget(t *testing.T) {
	env := localEnv()
	env[EnvTarget] = "live"
	_, err := LoadSpec(lookup(env))
	if err == nil {
		t.Fatalf("live target accepted %q sentinels", NotApplicableLocal)
	}
	for _, key := range []string{EnvIACEnablePR, EnvIACRevertPR, EnvAuthority, EnvPGMaxLagSeconds, EnvPGMaxCPUMillicores, EnvPGMaxMemoryMi} {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("error %q does not name %s", err, key)
		}
	}
}

func TestLoadSpecLiveTargetNeedsGrantAndPRNumbers(t *testing.T) {
	env := localEnv()
	env[EnvTarget] = "live"
	env[EnvIACEnablePR] = "#510"
	env[EnvIACRevertPR] = "#511"
	env[EnvAuthority] = "https://github.com/eshu-hq/eshu/issues/8875#issuecomment-6579288965"
	env[EnvPGMaxLagSeconds] = "30"
	env[EnvPGMaxCPUMillicores] = "10000"
	env[EnvPGMaxMemoryMi] = "63488"
	if _, err := LoadSpec(lookup(env)); err == nil || !strings.Contains(err.Error(), EnvLiveGrant) {
		t.Fatalf("live target without %s: err=%v", EnvLiveGrant, err)
	}
	env[EnvLiveGrant] = LiveGrantComment
	if _, err := LoadSpec(lookup(env)); err != nil {
		t.Fatalf("complete live field set: %v", err)
	}
	env[EnvIACRevertPR] = "revert"
	if _, err := LoadSpec(lookup(env)); err == nil || !strings.Contains(err.Error(), EnvIACRevertPR) {
		t.Fatalf("non-numeric revert PR accepted: %v", err)
	}
}

func TestLoadSpecRejectsInvalidBounds(t *testing.T) {
	cases := map[string]string{
		EnvWorkerMultiple:  "3",
		EnvBacklogItems:    "0",
		EnvBacklogScopes:   "0",
		EnvWindowSeconds:   "-1",
		EnvWallClockCap:    "3h",
		EnvStarvationBound: "zero",
		EnvDigest:          "latest",
	}
	for key, value := range cases {
		env := localEnv()
		env[key] = value
		if _, err := LoadSpec(lookup(env)); err == nil {
			t.Fatalf("%s=%q accepted", key, value)
		}
	}
	env := localEnv()
	env[EnvHeartbeatInterval] = "90s"
	if _, err := LoadSpec(lookup(env)); err == nil {
		t.Fatalf("heartbeat interval above the 60s lease accepted")
	}
}

func TestLoadSpecQuotaNotApplicable(t *testing.T) {
	env := localEnv()
	env[EnvQuotaBudget] = NotApplicable
	spec, err := LoadSpec(lookup(env))
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if spec.QuotaBudget != 0 {
		t.Fatalf("quota budget = %d, want 0 for NOT_APPLICABLE", spec.QuotaBudget)
	}
}
