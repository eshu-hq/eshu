// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import "time"

// Where the active-work part of a snapshot (queue, stage, backlog, blockage,
// and latest failure) came from (#7009).
const (
	// ActiveWorkSourceModel is a stored summary row that passed every fence.
	ActiveWorkSourceModel = "model"
	// ActiveWorkSourceLive is the live statement, run because the reader of
	// the stored summary is off.
	ActiveWorkSourceLive = "live"
	// ActiveWorkSourceLiveFallback is the live statement, run because the
	// stored row could not be served; ActiveWorkSource.Reason says why.
	ActiveWorkSourceLiveFallback = "live_fallback"
)

// The closed set of reasons behind an ActiveWorkSource.
const (
	ActiveWorkReasonFresh        = "fresh"
	ActiveWorkReasonFlagOff      = "flag_off"
	ActiveWorkReasonMissing      = "missing"
	ActiveWorkReasonNotInstalled = "not_installed"
	ActiveWorkReasonVersion      = "version"
	ActiveWorkReasonRowCount     = "row_count"
	ActiveWorkReasonStale        = "stale"
	ActiveWorkReasonDecode       = "decode"
)

// ActiveWorkSource says where the active-work sections of a snapshot came
// from and how old they are. A stored row is counted at its as_of, so the
// counts are true at AsOf while the ages are advanced to the read; the live
// statement is true at the snapshot's own clock. The other snapshot sections
// are always live, so one report can mix a stored summary with live sections;
// this marker is how a reader sees that.
type ActiveWorkSource struct {
	// Source is ActiveWorkSourceModel, ActiveWorkSourceLive, or
	// ActiveWorkSourceLiveFallback. Empty means the reader reports no source.
	Source string
	// Reason explains Source with one of the ActiveWorkReason values.
	Reason string
	// AsOf is the time the active-work counts are true at: the stored row's
	// as_of, or the live statement's clock.
	AsOf time.Time
	// Age is how old the served active-work data was at the read; zero for a
	// live read.
	Age time.Duration
	// Stale reports that the served active-work data is older than the
	// configured limit. A stored row that is too old is never served: the live
	// statement answers and Reason says "stale", so Stale is false on every
	// route that reads through the stored-summary reader. It is reserved for a
	// consumer that must serve its last decoded row (the runtime /metrics
	// scrape, #7009 PR-D).
	Stale bool
}

// ActiveWorkSourceJSON is the wire shape of ActiveWorkSource.
type ActiveWorkSourceJSON struct {
	Source     string  `json:"source"`
	Reason     string  `json:"reason"`
	AsOf       string  `json:"as_of"`
	AgeSeconds float64 `json:"age_seconds"`
	Stale      bool    `json:"stale"`
}

// JSON returns the wire shape, or nil when the reader reported no source so a
// caller adds no key for it.
func (s ActiveWorkSource) JSON() *ActiveWorkSourceJSON {
	if s.Source == "" {
		return nil
	}
	return &ActiveWorkSourceJSON{
		Source:     s.Source,
		Reason:     s.Reason,
		AsOf:       s.AsOf.UTC().Format(time.RFC3339),
		AgeSeconds: s.Age.Seconds(),
		Stale:      s.Stale,
	}
}
