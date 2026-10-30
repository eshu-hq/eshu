// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import (
	"time"
)

// SSMHostTuple is the EC2 managed-node observation boundary one SSM host
// inventory assessment covers: the AWS partition, account, region, and managed
// node instance identifier. Every row in one assessed stream must agree on the
// full tuple; a narrower identity (an identifier prefix alone) is insufficient
// and cross-tuple rows never merge (eshu-hq/eshu#9302).
type SSMHostTuple struct {
	Partition  string
	AccountID  string
	Region     string
	InstanceID string
}

// SSMHostApplicationViewRow is one admitted aws_ssm_host_application
// observation reduced to the assessment inputs: its host tuple, the inventory
// capture time, and the canonical allowlisted-row hash that deduplicates
// identical rows within one capture.
type SSMHostApplicationViewRow struct {
	Host        SSMHostTuple
	CaptureTime string
	RowHash     string
	Tombstone   bool
}

// SSMHostPlatformViewRow is one admitted aws_ssm_host_platform observation
// reduced to the assessment inputs: its host tuple, the evidence provenance,
// and the capture or observation time that governs its freshness.
type SSMHostPlatformViewRow struct {
	Host            SSMHostTuple
	SourceType      string
	CaptureTime     string
	ObservationTime string
	Tombstone       bool
}

// Closed stream verdict reasons. Reasons are static strings: capture times,
// tokens, and host identities never flow into operator-visible text through
// the verdict path.
const (
	ssmStreamReasonComplete       = "complete coherent capture within budgets"
	ssmStreamReasonIncoherent     = "incoherent stream identity or capture time"
	ssmStreamReasonMissingCapture = "missing or invalid capture time"
	ssmStreamReasonNoRows         = "no observations read"
	ssmStreamReasonWithdrawn      = "observations withdrawn"
	ssmStreamReasonStale          = "capture past the stale-age bound"
	ssmStreamReasonStaleDII       = "platform observation past the observation-age bound"
)

// SSMHostStreamAssessment is the verdict for one host and inventory type:
// whether the read is a complete coherent capture, whether it reads stale,
// the capture time when one governs, the deduplicated row count, and the
// static reason. Only a fresh complete capture may supersede the prior
// generation; every other verdict preserves it.
type SSMHostStreamAssessment struct {
	// Complete reports a fully coherent, within-budget read.
	Complete bool
	// Stale reports a coherent-but-aged read past the applicable bound.
	Stale bool
	// CaptureTime is the governing capture or observation time, when one
	// parsed. It is the zero time when no time governs the verdict.
	CaptureTime time.Time
	// RowCount is the deduplicated live observation count.
	RowCount int
	// Reason is a closed static string naming the verdict class.
	Reason string
}

// Absent reports whether the verdict carries no live signal for its stream:
// no observations read, or every observation withdrawn. An absent stream
// contributes nothing to the host status; an incoherent stream (Complete and
// Stale both false with live rows behind a non-empty reason) is a fresh
// negative signal, never absence.
func (a SSMHostStreamAssessment) Absent() bool {
	return !a.Complete && !a.Stale && a.RowCount == 0 &&
		(a.Reason == ssmStreamReasonNoRows || a.Reason == ssmStreamReasonWithdrawn)
}

// AssessSSMHostApplicationStream verdicts one host's application observations
// against the stale-age bound at read time now (eshu-hq/eshu#9302). Identical
// rows in one capture deduplicate on their canonical hash; mixed capture
// times are incoherent (never resolved by picking a winner); a missing or
// invalid capture time emits no superseding truth; a fully withdrawn stream
// is unknown, never an empty host.
func AssessSSMHostApplicationStream(rows []SSMHostApplicationViewRow, now time.Time, maxAge time.Duration) SSMHostStreamAssessment {
	preserve := func(reason string) SSMHostStreamAssessment {
		return SSMHostStreamAssessment{Reason: reason}
	}
	if len(rows) == 0 {
		return preserve(ssmStreamReasonNoRows)
	}
	first := rows[0]
	captureTime, err := time.Parse(time.RFC3339, first.CaptureTime)
	if first.CaptureTime == "" || err != nil {
		return preserve(ssmStreamReasonMissingCapture)
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.Host != first.Host {
			return preserve(ssmStreamReasonIncoherent)
		}
		if row.CaptureTime != first.CaptureTime {
			return preserve(ssmStreamReasonIncoherent)
		}
		if row.Tombstone {
			continue
		}
		seen[row.RowHash] = struct{}{}
	}
	if len(seen) == 0 {
		return preserve(ssmStreamReasonWithdrawn)
	}
	assessment := SSMHostStreamAssessment{
		Complete:    true,
		CaptureTime: captureTime,
		RowCount:    len(seen),
		Reason:      ssmStreamReasonComplete,
	}
	if now.Sub(captureTime) > maxAge {
		assessment.Complete = false
		assessment.Stale = true
		assessment.Reason = ssmStreamReasonStale
	}
	return assessment
}

// AssessSSMHostPlatformStream verdicts one host's platform observations at
// read time now (eshu-hq/eshu#9302). Inventory-sourced evidence is governed by
// its inventory capture time under maxInventoryAge; DescribeInstanceInformation
// evidence carries no capture time and is governed by its observation time
// under the separate maxObservationAge rule. A capture time on
// DescribeInstanceInformation evidence is a contract violation the collector
// validator rejects; here it is incoherent, never silently preferred.
func AssessSSMHostPlatformStream(rows []SSMHostPlatformViewRow, now time.Time, maxInventoryAge, maxObservationAge time.Duration) SSMHostStreamAssessment {
	preserve := func(reason string) SSMHostStreamAssessment {
		return SSMHostStreamAssessment{Reason: reason}
	}
	if len(rows) == 0 {
		return preserve(ssmStreamReasonNoRows)
	}
	first := rows[0]
	live := 0
	for _, row := range rows {
		if row.Host != first.Host || row.SourceType != first.SourceType {
			return preserve(ssmStreamReasonIncoherent)
		}
		if !row.Tombstone {
			live++
		}
	}
	if live == 0 {
		return preserve(ssmStreamReasonWithdrawn)
	}
	if first.SourceType == "describe_instance_information" {
		if first.CaptureTime != "" {
			return preserve(ssmStreamReasonIncoherent)
		}
		observed, err := time.Parse(time.RFC3339, first.ObservationTime)
		if first.ObservationTime == "" || err != nil {
			return preserve(ssmStreamReasonMissingCapture)
		}
		assessment := SSMHostStreamAssessment{
			Complete:    true,
			CaptureTime: observed,
			RowCount:    live,
			Reason:      ssmStreamReasonComplete,
		}
		if now.Sub(observed) > maxObservationAge {
			assessment.Complete = false
			assessment.Stale = true
			assessment.Reason = ssmStreamReasonStaleDII
		}
		return assessment
	}
	captureTime, err := time.Parse(time.RFC3339, first.CaptureTime)
	if first.CaptureTime == "" || err != nil {
		return preserve(ssmStreamReasonMissingCapture)
	}
	for _, row := range rows[1:] {
		if row.CaptureTime != first.CaptureTime {
			return preserve(ssmStreamReasonIncoherent)
		}
	}
	assessment := SSMHostStreamAssessment{
		Complete:    true,
		CaptureTime: captureTime,
		RowCount:    live,
		Reason:      ssmStreamReasonComplete,
	}
	if now.Sub(captureTime) > maxInventoryAge {
		assessment.Complete = false
		assessment.Stale = true
		assessment.Reason = ssmStreamReasonStale
	}
	return assessment
}

// SSMSkewPairs reports whether the application capture time and the platform
// evidence time admitted as one host view sit within the cross-type skew
// bound. AWS does not promise equal capture times across inventory types, so
// paired evidence overlaps in time rather than matching exactly.
func SSMSkewPairs(applicationCapture, platformTime time.Time, maxSkew time.Duration) bool {
	skew := applicationCapture.Sub(platformTime)
	if skew < 0 {
		skew = -skew
	}
	return skew <= maxSkew
}
