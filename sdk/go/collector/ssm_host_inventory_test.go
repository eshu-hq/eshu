// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// exampleSSMHostInventoryLimits returns explicitly configured limits within
// the arbiter ceilings from the accepted #9302 record: 5000 entries per host
// per capture, 100 pages per host per type per poll, 1 KiB per entry, 50 KiB
// per page, and an explicit target list. Freshness is tied to the capture
// interval: the stale-age bound covers two polls, the cross-type skew bound
// sits well inside it, and the DescribeInstanceInformation platform rule runs
// on observation age, never an invented capture time.
func exampleSSMHostInventoryLimits() SSMHostInventoryLimits {
	return SSMHostInventoryLimits{
		MaxHostsPerPoll:               100,
		MaxPagesPerHostPerTypePerPoll: 100,
		MaxEntriesPerHostPerCapture:   5000,
		MaxEntryBytes:                 1024,
		MaxPageBytes:                  50 * 1024,
		CaptureInterval:               24 * time.Hour,
		MaxCaptureAge:                 48 * time.Hour,
		MaxCrossTypeSkew:              15 * time.Minute,
		MaxPlatformObservationAge:     48 * time.Hour,
		RetentionDays:                 90,
		Targets:                       []string{"tag:inventory-cohort"},
		AllowedSourceTypes:            []string{"AWS:Application"},
	}
}

func TestSSMHostInventoryLimitsAcceptsExplicitInCeilingConfig(t *testing.T) {
	t.Parallel()

	if err := exampleSSMHostInventoryLimits().Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestSSMHostInventoryLimitsMissingFailsClosed(t *testing.T) {
	t.Parallel()

	base := exampleSSMHostInventoryLimits()
	cases := []struct {
		name  string
		blank func(*SSMHostInventoryLimits)
	}{
		{"hosts", func(l *SSMHostInventoryLimits) { l.MaxHostsPerPoll = 0 }},
		{"pages", func(l *SSMHostInventoryLimits) { l.MaxPagesPerHostPerTypePerPoll = 0 }},
		{"entries", func(l *SSMHostInventoryLimits) { l.MaxEntriesPerHostPerCapture = 0 }},
		{"entry_bytes", func(l *SSMHostInventoryLimits) { l.MaxEntryBytes = 0 }},
		{"page_bytes", func(l *SSMHostInventoryLimits) { l.MaxPageBytes = 0 }},
		{"capture_interval", func(l *SSMHostInventoryLimits) { l.CaptureInterval = 0 }},
		{"capture_age", func(l *SSMHostInventoryLimits) { l.MaxCaptureAge = 0 }},
		{"skew", func(l *SSMHostInventoryLimits) { l.MaxCrossTypeSkew = 0 }},
		{"platform_age", func(l *SSMHostInventoryLimits) { l.MaxPlatformObservationAge = 0 }},
		{"retention", func(l *SSMHostInventoryLimits) { l.RetentionDays = 0 }},
		{"targets", func(l *SSMHostInventoryLimits) { l.Targets = nil }},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limits := base
			tt.blank(&limits)
			if err := limits.Validate(); err == nil {
				t.Fatalf("Validate() with missing %s = nil, want fail-closed error", tt.name)
			}
		})
	}
}

func TestSSMHostInventoryLimitsAboveCeilingFails(t *testing.T) {
	t.Parallel()

	base := exampleSSMHostInventoryLimits()
	cases := []struct {
		name   string
		breach func(*SSMHostInventoryLimits)
	}{
		{"entries", func(l *SSMHostInventoryLimits) {
			l.MaxEntriesPerHostPerCapture = SSMHostInventoryMaxEntriesPerHostPerCapture + 1
		}},
		{"pages", func(l *SSMHostInventoryLimits) {
			l.MaxPagesPerHostPerTypePerPoll = SSMHostInventoryMaxPagesPerHostPerTypePerPoll + 1
		}},
		{"entry_bytes", func(l *SSMHostInventoryLimits) { l.MaxEntryBytes = SSMHostInventoryMaxEntryBytes + 1 }},
		{"page_bytes", func(l *SSMHostInventoryLimits) { l.MaxPageBytes = SSMHostInventoryMaxPageBytes + 1 }},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limits := base
			tt.breach(&limits)
			if err := limits.Validate(); err == nil {
				t.Fatalf("Validate() above %s ceiling = nil, want error", tt.name)
			}
		})
	}
}

func TestSSMHostInventoryLimitsFreshnessCoherence(t *testing.T) {
	t.Parallel()

	t.Run("stale_age_below_interval_fails", func(t *testing.T) {
		t.Parallel()
		limits := exampleSSMHostInventoryLimits()
		limits.MaxCaptureAge = 23 * time.Hour
		if err := limits.Validate(); err == nil {
			t.Fatal("Validate() with stale age below capture interval = nil, want error")
		}
	})

	t.Run("skew_above_stale_age_fails", func(t *testing.T) {
		t.Parallel()
		limits := exampleSSMHostInventoryLimits()
		limits.MaxCrossTypeSkew = 49 * time.Hour
		if err := limits.Validate(); err == nil {
			t.Fatal("Validate() with skew above stale age = nil, want error")
		}
	})
}

func TestSSMHostInventoryFreshnessRules(t *testing.T) {
	t.Parallel()

	limits := exampleSSMHostInventoryLimits()
	now := time.Date(2026, 10, 29, 22, 35, 0, 0, time.UTC)
	freshCapture := time.Date(2026, 10, 29, 22, 31, 19, 0, time.UTC)

	if limits.CaptureIsStale(freshCapture, now) {
		t.Fatal("CaptureIsStale(4-minute-old capture) = true, want false")
	}
	if !limits.CaptureIsStale(freshCapture.Add(-49*time.Hour), now) {
		t.Fatal("CaptureIsStale(49-hour-old capture) = false, want true")
	}

	if !limits.CrossTypeSkewOK(freshCapture, freshCapture.Add(2*time.Minute)) {
		t.Fatal("CrossTypeSkewOK(2-minute skew) = false, want true")
	}
	if limits.CrossTypeSkewOK(freshCapture, freshCapture.Add(16*time.Minute)) {
		t.Fatal("CrossTypeSkewOK(16-minute skew) = true, want false")
	}

	if !limits.PlatformObservationIsFresh(now.Add(-time.Hour), now) {
		t.Fatal("PlatformObservationIsFresh(1-hour-old DII read) = false, want true")
	}
	if limits.PlatformObservationIsFresh(now.Add(-49*time.Hour), now) {
		t.Fatal("PlatformObservationIsFresh(49-hour-old DII read) = true, want false")
	}
}

func TestSSMHostInventoryOutcomeMapsToResultState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		outcome SSMHostInventoryOutcome
		want    ResultState
	}{
		{SSMHostInventorySuccessful, ResultComplete},
		{SSMHostInventoryPartial, ResultPartial},
		{SSMHostInventoryStale, ResultUnchanged},
		{SSMHostInventoryDenied, ResultTerminal},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(string(tt.outcome), func(t *testing.T) {
			t.Parallel()
			if got := tt.outcome.ToResultState(); got != tt.want {
				t.Fatalf("%q.ToResultState() = %q, want %q", tt.outcome, got, tt.want)
			}
		})
	}
}

// TestSSMHostInventoryAggregatesCarryCountsOnly proves the aggregate-only
// telemetry rule from #9556: the log fields carry host, page, entry, and
// byte counts plus the outcome — never package values, hostnames, IPs,
// URLs, or rows. The hostile strings below model everything a careless
// implementation could interpolate; none may survive into the fields.
func TestSSMHostInventoryAggregatesCarryCountsOnly(t *testing.T) {
	t.Parallel()

	hostile := []string{
		"openssl-3.0.8", "i-00000000000000002", "host-01.example.com",
		"203.0.113.10", "https://example.com/packages",
		"ListInventoryEntries row",
	}
	aggregates := SSMHostInventoryAggregates{
		Targets: 24, Managed: 24, Captured: 2, Excluded: 22,
		DeniedHosts: 0, PartialHosts: 0, StaleHosts: 0, SuccessfulHosts: 2,
		Entries: 2605, Pages: 53, ResponseBytes: 1195978,
		Outcome: SSMHostInventorySuccessful,
	}
	fields := aggregates.LogFields()
	if len(fields) == 0 {
		t.Fatal("LogFields() is empty, want aggregate counts")
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("json.Marshal(LogFields()) error = %v", err)
	}
	for _, secret := range hostile {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("aggregate log fields leak %q", secret)
		}
	}
}

// TestSSMHostInventoryLimitsMissingAllowlistFailsClosed proves the poll
// allowlist is required: no explicit source-type set means no stream is
// admissible.
func TestSSMHostInventoryLimitsMissingAllowlistFailsClosed(t *testing.T) {
	t.Parallel()

	limits := exampleSSMHostInventoryLimits()
	limits.AllowedSourceTypes = nil
	if err := limits.Validate(); err == nil {
		t.Fatal("Validate() without allowlist: error = nil, want fail-closed")
	}
}

// TestValidateSSMHostPlatformProvenance proves the P2-1 pairing check for
// eshu-hq/eshu#9571: a DescribeInstanceInformation observation carrying a
// capture time is rejected, while both valid pairings pass.
func TestValidateSSMHostPlatformProvenance(t *testing.T) {
	t.Parallel()

	if err := ValidateSSMHostPlatformProvenance("describe_instance_information", true); err == nil {
		t.Fatal("DII with capture time: error = nil, want rejection")
	}
	if err := ValidateSSMHostPlatformProvenance("describe_instance_information", false); err != nil {
		t.Fatalf("DII without capture time: error = %v, want nil", err)
	}
	if err := ValidateSSMHostPlatformProvenance("ssm_inventory", true); err != nil {
		t.Fatalf("inventory with capture time: error = %v, want nil", err)
	}
}
