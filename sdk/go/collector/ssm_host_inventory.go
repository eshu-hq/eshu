// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"time"
)

// Arbiter ceilings for bounded SSM host inventory collection (eshu-hq/eshu
// #9302, arbiter ACCEPT 2026-10-29, measured on the 2-node captured cohort:
// target A 1310 entries over 27 pages at ~459 B/entry and ~22.3 KB/page).
// Implementation limits must not exceed these ceilings without a new
// measurement; a capture exceeding any ceiling is partial/unknown, never
// truncated-and-superseding.
const (
	// SSMHostInventoryMaxEntriesPerHostPerCapture caps the application
	// entries admitted for one host in one capture (~3.8x the observed
	// 1310-entry maximum).
	SSMHostInventoryMaxEntriesPerHostPerCapture = 5000

	// SSMHostInventoryMaxPagesPerHostPerTypePerPoll caps the
	// ListInventoryEntries pages read for one host and inventory type in
	// one poll (~3.7x the observed 27-page maximum).
	SSMHostInventoryMaxPagesPerHostPerTypePerPoll = 100

	// SSMHostInventoryMaxEntryBytes is the per-entry planning budget (~2.2x
	// headroom over the observed ~459 B/entry).
	SSMHostInventoryMaxEntryBytes = 1024

	// SSMHostInventoryMaxPageBytes is the per-page planning budget (~2x
	// headroom over the observed ~22-23 KB/page).
	SSMHostInventoryMaxPageBytes = 50 * 1024
)

// SSMHostInventoryLimits is the explicit deployment configuration for
// bounded SSM host inventory collection (eshu-hq/eshu#9556). Every field is
// required: missing limits fail configuration closed (Validate), and bounded
// resource fields must sit within the arbiter ceilings above. The host set
// is an explicit configured target list with no unlimited default.
type SSMHostInventoryLimits struct {
	// MaxHostsPerPoll caps the managed nodes polled in one collection pass.
	// Required positive.
	MaxHostsPerPoll int

	// MaxPagesPerHostPerTypePerPoll caps ListInventoryEntries pages read
	// for one host and inventory type per poll. Required positive, at most
	// SSMHostInventoryMaxPagesPerHostPerTypePerPoll.
	MaxPagesPerHostPerTypePerPoll int

	// MaxEntriesPerHostPerCapture caps the application entries admitted for
	// one host in one capture. Required positive, at most
	// SSMHostInventoryMaxEntriesPerHostPerCapture.
	MaxEntriesPerHostPerCapture int

	// MaxEntryBytes caps the planning bytes of one inventory entry.
	// Required positive, at most SSMHostInventoryMaxEntryBytes.
	MaxEntryBytes int

	// MaxPageBytes caps the planning bytes of one inventory page.
	// Required positive, at most SSMHostInventoryMaxPageBytes.
	MaxPageBytes int

	// CaptureInterval is the configured Eshu polling cadence for SSM
	// inventory. Required positive: freshness bounds below are tied to it.
	CaptureInterval time.Duration

	// MaxCaptureAge is the stale-age bound: a capture older than this at
	// read time displays stale and cannot supersede the prior generation.
	// Required, at least CaptureInterval (a bound shorter than one poll is
	// incoherent). The recommended tie is two capture intervals.
	MaxCaptureAge time.Duration

	// MaxCrossTypeSkew bounds the allowed capture-time difference between
	// the platform and application evidence admitted as one host view: AWS
	// does not promise equal capture times across types. Required
	// positive, at most MaxCaptureAge (a skew wider than the stale bound
	// admits pairs no single capture could be fresh for).
	MaxCrossTypeSkew time.Duration

	// MaxPlatformObservationAge bounds the age of a
	// DescribeInstanceInformation platform observation: that source has no
	// inventory capture time, so this separate evidence rule governs it.
	// Required positive.
	MaxPlatformObservationAge time.Duration

	// RetentionDays bounds how long admitted SSM host observations are
	// retained. Required positive.
	RetentionDays int

	// Targets is the explicit configured host set (association targets,
	// tag selectors, or instance lists, per deployment). Required
	// non-empty: there is no unlimited default.
	Targets []string
}

// Validate fails closed unless every limit is explicitly configured,
// freshness bounds cohere with the capture interval, and bounded resource
// fields sit within the arbiter ceilings.
func (l SSMHostInventoryLimits) Validate() error {
	if l.MaxHostsPerPoll <= 0 {
		return fmt.Errorf("ssm host inventory: max hosts per poll must be positive")
	}
	if l.MaxPagesPerHostPerTypePerPoll <= 0 {
		return fmt.Errorf("ssm host inventory: max pages per host per type per poll must be positive")
	}
	if l.MaxPagesPerHostPerTypePerPoll > SSMHostInventoryMaxPagesPerHostPerTypePerPoll {
		return fmt.Errorf("ssm host inventory: max pages per host per type per poll %d exceeds ceiling %d",
			l.MaxPagesPerHostPerTypePerPoll, SSMHostInventoryMaxPagesPerHostPerTypePerPoll)
	}
	if l.MaxEntriesPerHostPerCapture <= 0 {
		return fmt.Errorf("ssm host inventory: max entries per host per capture must be positive")
	}
	if l.MaxEntriesPerHostPerCapture > SSMHostInventoryMaxEntriesPerHostPerCapture {
		return fmt.Errorf("ssm host inventory: max entries per host per capture %d exceeds ceiling %d",
			l.MaxEntriesPerHostPerCapture, SSMHostInventoryMaxEntriesPerHostPerCapture)
	}
	if l.MaxEntryBytes <= 0 {
		return fmt.Errorf("ssm host inventory: max entry bytes must be positive")
	}
	if l.MaxEntryBytes > SSMHostInventoryMaxEntryBytes {
		return fmt.Errorf("ssm host inventory: max entry bytes %d exceeds ceiling %d",
			l.MaxEntryBytes, SSMHostInventoryMaxEntryBytes)
	}
	if l.MaxPageBytes <= 0 {
		return fmt.Errorf("ssm host inventory: max page bytes must be positive")
	}
	if l.MaxPageBytes > SSMHostInventoryMaxPageBytes {
		return fmt.Errorf("ssm host inventory: max page bytes %d exceeds ceiling %d",
			l.MaxPageBytes, SSMHostInventoryMaxPageBytes)
	}
	if l.CaptureInterval <= 0 {
		return fmt.Errorf("ssm host inventory: capture interval must be positive")
	}
	if l.MaxCaptureAge <= 0 {
		return fmt.Errorf("ssm host inventory: max capture age must be positive")
	}
	if l.MaxCaptureAge < l.CaptureInterval {
		return fmt.Errorf("ssm host inventory: max capture age %v is below capture interval %v",
			l.MaxCaptureAge, l.CaptureInterval)
	}
	if l.MaxCrossTypeSkew <= 0 {
		return fmt.Errorf("ssm host inventory: max cross-type skew must be positive")
	}
	if l.MaxCrossTypeSkew > l.MaxCaptureAge {
		return fmt.Errorf("ssm host inventory: max cross-type skew %v exceeds max capture age %v",
			l.MaxCrossTypeSkew, l.MaxCaptureAge)
	}
	if l.MaxPlatformObservationAge <= 0 {
		return fmt.Errorf("ssm host inventory: max platform observation age must be positive")
	}
	if l.RetentionDays <= 0 {
		return fmt.Errorf("ssm host inventory: retention days must be positive")
	}
	if len(l.Targets) == 0 {
		return fmt.Errorf("ssm host inventory: explicit target list is required (no unlimited default)")
	}
	return nil
}

// CaptureIsStale reports whether a capture taken at captureTime reads stale
// at read time now under the configured stale-age bound.
func (l SSMHostInventoryLimits) CaptureIsStale(captureTime, now time.Time) bool {
	return now.Sub(captureTime) > l.MaxCaptureAge
}

// CrossTypeSkewOK reports whether the application and platform capture times
// admitted as one host view sit within the configured cross-type skew
// bound.
func (l SSMHostInventoryLimits) CrossTypeSkewOK(applicationCapture, platformCapture time.Time) bool {
	skew := applicationCapture.Sub(platformCapture)
	if skew < 0 {
		skew = -skew
	}
	return skew <= l.MaxCrossTypeSkew
}

// PlatformObservationIsFresh reports whether a DescribeInstanceInformation
// platform observation read at observationTime is fresh at read time now
// under the separate platform observation rule.
func (l SSMHostInventoryLimits) PlatformObservationIsFresh(observationTime, now time.Time) bool {
	return now.Sub(observationTime) <= l.MaxPlatformObservationAge
}

// SSMHostInventoryOutcome is the per-host-per-type collection verdict the
// operator sees (eshu-hq/eshu#9556). It distinguishes denied, partial, stale,
// and successful inventory; only successful inventory may supersede the
// prior generation.
type SSMHostInventoryOutcome string

const (
	// SSMHostInventoryDenied marks a host whose inventory read was denied
	// (for example a missing grant). A denial is not a zero inventory
	// result: the prior generation stands.
	SSMHostInventoryDenied SSMHostInventoryOutcome = "denied"

	// SSMHostInventoryPartial marks reachable-but-incomplete evidence:
	// missing pages, an incoherent page pair, or a ceiling breach. Partial
	// inventory preserves the prior generation.
	SSMHostInventoryPartial SSMHostInventoryOutcome = "partial"

	// SSMHostInventoryStale marks a complete-but-aged capture past the
	// stale-age bound. The display marks staleness; the prior generation
	// stands.
	SSMHostInventoryStale SSMHostInventoryOutcome = "stale"

	// SSMHostInventorySuccessful marks a fresh complete capture that may
	// supersede the prior generation for that host and type.
	SSMHostInventorySuccessful SSMHostInventoryOutcome = "successful"
)

// ToResultState maps the inventory outcome onto the collector SDK result
// states the host commits: successful inventory completes the claim, partial
// inventory commits what is reachable, stale inventory commits nothing new
// (the prior generation stands), and denied inventory terminates the claim
// for operator action instead of retrying blindly.
func (o SSMHostInventoryOutcome) ToResultState() ResultState {
	switch o {
	case SSMHostInventorySuccessful:
		return ResultComplete
	case SSMHostInventoryPartial:
		return ResultPartial
	case SSMHostInventoryStale:
		return ResultUnchanged
	case SSMHostInventoryDenied:
		return ResultTerminal
	default:
		return ResultTerminal
	}
}

// SSMHostInventoryAggregates is the aggregate-only operator telemetry for
// one SSM inventory poll (eshu-hq/eshu#9556). It carries counts only —
// targets, managed, captured, and excluded host counts separately (none is
// automatically the denominator for the others), per-outcome host counts,
// and entry/page/byte totals. It has no string field for package values,
// hostnames, IPs, URLs, or rows, so no such value can flow into logs
// through this shape.
type SSMHostInventoryAggregates struct {
	// Targets counts the configured host set.
	Targets int
	// Managed counts the SSM-managed nodes among the targets.
	Managed int
	// Captured counts the hosts with a dated complete capture.
	Captured int
	// Excluded counts the hosts out of cohort scope.
	Excluded int
	// DeniedHosts counts the hosts whose read was denied.
	DeniedHosts int
	// PartialHosts counts the hosts with partial or unknown evidence.
	PartialHosts int
	// StaleHosts counts the hosts whose newest capture reads stale.
	StaleHosts int
	// SuccessfulHosts counts the hosts with fresh complete captures.
	SuccessfulHosts int
	// Entries counts the admitted application entries.
	Entries int
	// Pages counts the inventory pages read.
	Pages int
	// ResponseBytes counts the compact-JSON response bytes read.
	ResponseBytes int64
	// Outcome is the poll-level verdict.
	Outcome SSMHostInventoryOutcome
}

// LogFields renders the aggregates as logger fields: counts plus the
// outcome only. There is no identifier, value, or row content to render by
// construction.
func (a SSMHostInventoryAggregates) LogFields() map[string]any {
	return map[string]any{
		"targets":          a.Targets,
		"managed":          a.Managed,
		"captured":         a.Captured,
		"excluded":         a.Excluded,
		"denied_hosts":     a.DeniedHosts,
		"partial_hosts":    a.PartialHosts,
		"stale_hosts":      a.StaleHosts,
		"successful_hosts": a.SuccessfulHosts,
		"entries":          a.Entries,
		"pages":            a.Pages,
		"response_bytes":   a.ResponseBytes,
		"outcome":          string(a.Outcome),
	}
}
