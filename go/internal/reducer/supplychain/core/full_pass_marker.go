// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// The full-pass marker bounds the #10415 out-of-stamp fallback (#10972,
// arbiter decision D2). Before it, a stamped one-hop rerun ran full whenever
// the holder held any active row outside the stamp, which is the normal state
// of every multi-CVE holder, so nearly every rerun was a full pass long enough
// to be flagged again. Now the fallback fires only when a reap is due: the
// item carries no marker for its generation, or fullPassReapMaxScopedReruns
// scoped reruns ran since the last full pass, or that pass is older than
// fullPassReapMaxAge. A drift row outside the stamp lingers for at most that
// bound instead of being reaped on every reopen.
//
// The marker lives on the item's payload and is written by the pass's own
// lease-fenced fan-out state update (UpdateFanOutState), the statement every
// stamped pass already runs to consume its stamp, so it costs no new
// statement. A full pass whose evidence load was complete resets it; a scoped
// pass increments its counter; a partial full pass leaves it alone, because
// it retracted nothing. A new generation is a new work item, so its first
// stamped rerun finds no marker and reaps in full once.
const (
	// PayloadKeyFullPassMarker is the work-item payload key of the marker.
	PayloadKeyFullPassMarker = "full_pass_marker"
	// FullPassMarkerGenerationKey names the generation the full pass wrote.
	FullPassMarkerGenerationKey = "generation_id"
	// FullPassMarkerScopedSinceFullKey counts scoped reruns since that pass.
	FullPassMarkerScopedSinceFullKey = "scoped_since_full" // #nosec G101 -- payload key name, not a credential
	// FullPassMarkerFullAtKey is when that pass ran, RFC 3339 in UTC.
	FullPassMarkerFullAtKey = "full_at"

	// fullPassReapMaxScopedReruns is N. QA measured about 431 passes/h with
	// one-hop reruns at 57% of handler time, and the worst item reran 11 times
	// in 37 minutes (about 18/h). N = 16 lets that item reap about once an
	// hour instead of on every rerun, and bounds drift on any item to 16
	// reruns.
	fullPassReapMaxScopedReruns = 16
	// fullPassReapMaxAge is T: an item that reruns rarely still reaps drift
	// within a day of its last full pass. 24 hours matches the daily
	// enrichment cadence (arbiter D3/D2 ruling); an hour would find the marker
	// due on every rerun of a holder rerun less than hourly and save nothing.
	// One clock serves both ends: full_at is written from, and the age is
	// read against, the reducer worker's wall clock (time.Now in UTC), never
	// the database clock. Skew between workers shifts the bound by the skew
	// only, which is noise at 24 hours.
	fullPassReapMaxAge = 24 * time.Hour
)

// fullPassMarker is the parsed marker of one item.
type fullPassMarker struct {
	generationID    string
	scopedSinceFull int
	fullAt          time.Time
}

// fullPassMarkerOf parses the marker the intent carries. It reports false
// for a missing or malformed marker, which callers treat as a reap due.
func fullPassMarkerOf(intent reducercontract.Intent) (fullPassMarker, bool) {
	raw, ok := intent.Payload[PayloadKeyFullPassMarker].(map[string]any)
	if !ok {
		return fullPassMarker{}, false
	}
	generation, _ := raw[FullPassMarkerGenerationKey].(string)
	fullAtText, _ := raw[FullPassMarkerFullAtKey].(string)
	fullAt, err := time.Parse(time.RFC3339Nano, fullAtText)
	if generation == "" || err != nil {
		return fullPassMarker{}, false
	}
	count, ok := markerCount(raw[FullPassMarkerScopedSinceFullKey])
	if !ok {
		return fullPassMarker{}, false
	}
	return fullPassMarker{generationID: generation, scopedSinceFull: count, fullAt: fullAt}, true
}

// markerCount reads a non-negative count that arrived as a JSON number
// (float64 from the queue) or as a Go integer (tests and in-process callers).
func markerCount(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		if v < 0 || v != float64(int(v)) {
			return 0, false
		}
		return int(v), true
	case int:
		return v, v >= 0
	case int64:
		return int(v), v >= 0
	}
	return 0, false
}

// fullPassReapDue reports whether a stamped pass with rows outside its stamp
// must run full to reap them. It fails closed: no marker, a marker for
// another generation, the scoped-rerun bound, or the age bound all make a
// reap due.
func fullPassReapDue(intent reducercontract.Intent, now time.Time) bool {
	marker, ok := fullPassMarkerOf(intent)
	if !ok || marker.generationID != intent.GenerationID {
		return true
	}
	return marker.scopedSinceFull >= fullPassReapMaxScopedReruns || now.Sub(marker.fullAt) >= fullPassReapMaxAge
}

// nextFullPassMarker returns the marker this pass leaves on its item, or nil
// to leave the stored one unchanged. A complete full pass resets it; a scoped
// pass counts itself against a valid marker of its generation; anything else
// (a partial full pass, a scoped pass with no valid marker) changes nothing.
// Every verdict that does not re-derive the whole set counts toward N: a
// future inert or seedless-scoped verdict (D3) must take the scoped branch.
func nextFullPassMarker(intent reducercontract.Intent, loaded supplyChainImpactLoadedEvidence, now time.Time) map[string]any {
	if loaded.scoped == nil {
		if loaded.truncation.partial() {
			return nil
		}
		return map[string]any{
			FullPassMarkerGenerationKey:      intent.GenerationID,
			FullPassMarkerScopedSinceFullKey: 0,
			FullPassMarkerFullAtKey:          now.UTC().Format(time.RFC3339Nano),
		}
	}
	marker, ok := fullPassMarkerOf(intent)
	if !ok || marker.generationID != intent.GenerationID {
		return nil
	}
	return map[string]any{
		FullPassMarkerGenerationKey:      marker.generationID,
		FullPassMarkerScopedSinceFullKey: marker.scopedSinceFull + 1,
		FullPassMarkerFullAtKey:          marker.fullAt.UTC().Format(time.RFC3339Nano),
	}
}

// withFullPassMarker returns the write result carrying the marker this pass
// leaves on its item, computed on the worker's wall clock (the one clock the
// marker uses), for the fan-out state update to store.
func withFullPassMarker(write SupplyChainImpactWriteResult, intent reducercontract.Intent, loaded supplyChainImpactLoadedEvidence) SupplyChainImpactWriteResult {
	write.fullPassMarker = nextFullPassMarker(intent, loaded, time.Now())
	return write
}

// newFanOutState starts a findings pass's fan-out state: the stamp to
// consume, when the pass carried one, and the full-pass marker the handler
// computed for this pass (write.fullPassMarker).
func newFanOutState(intent reducercontract.Intent, write SupplyChainImpactWriteResult) FanOutState {
	stamped := fanOutOriginStamped(intent)
	state := FanOutState{ConsumeOrigin: stamped, FullPassMarker: write.fullPassMarker}
	if stamped {
		state.ExpectedOrigin = fanOutOriginOf(intent)
	}
	return state
}
