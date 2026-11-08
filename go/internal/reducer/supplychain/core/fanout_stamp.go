// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// FanOutStampCVEKey is the fanout_origin stamp key carrying the sorted,
// de-duplicated CVE ids the committing pass changed (#9740). A stamped pass
// compares the CVEs it derived with this set after its writer commit and
// before its acknowledgement: disjoint means the flagged change cannot affect
// what it wrote, so it clears its replay flag instead of rerunning.
const FanOutStampCVEKey = "cves"

// FanOutStampSentinel stands in for the whole changed set when it exceeds
// FanOutOriginCVEIDCap. A sentinel stamp always reruns: no pass may prove
// itself disjoint from an unbounded change.
const FanOutStampSentinel = "*"

// FanOutOriginCVEIDCap bounds the CVE ids one stamp carries, so a widely
// shared CVE does not bloat every reopened item's payload. It matches the
// holder-read chunk size, so one capped stamp still names at most one read
// chunk's worth of CVEs.
const FanOutOriginCVEIDCap = fanOutCVEChunk

// FanOutOrigin names the pass that caused a reopen.
type FanOutOrigin struct {
	SourceScopeID      string
	SourceGenerationID string
	SourceIntentID     string
	// CVEIDs are the sorted, de-duplicated, capped CVE ids the committing
	// pass changed (#9740). Empty on stamps written before the set existed
	// and on enqueued first passes, which always run full. Above
	// FanOutOriginCVEIDCap the stamp carries only FanOutStampSentinel.
	CVEIDs []string
}

// CapFanOutStampCVEIDs sorts and de-duplicates ids for a stamp, replacing an
// over-cap set with the sentinel alone. The empty set stays empty: a reopen
// that names no CVE ids runs a full pass, it does not clear anything. The
// store applies it at the write boundary too, so every stamp writer shares
// the one canonical shape.
func CapFanOutStampCVEIDs(ids []string) []string {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		set[id] = struct{}{}
	}
	if len(set) > FanOutOriginCVEIDCap {
		return []string{FanOutStampSentinel}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// fanOutStampCVEIDs parses the stamped CVE set out of an intent's payload.
// The queue stores the stamp as map[string]any with a []any id list; a stamp
// without the key, or with any other shape, parses as no set, never an error,
// so a corrupt stamp fails closed to a full pass and a rerun.
func fanOutStampCVEIDs(intent reducercontract.Intent) []string {
	stamp, ok := intent.Payload[PayloadKeyFanOutOrigin].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := stamp[FanOutStampCVEKey].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		if s, ok := id.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// fanOutStampSentinel reports whether the stamped set is the unbounded marker.
func fanOutStampSentinel(cveIDs []string) bool {
	for _, id := range cveIDs {
		if id == FanOutStampSentinel {
			return true
		}
	}
	return false
}

// FanOutStampJSON marshals a reopen stamp. It is the one stamp shape every
// writer of fanout_origin uses, so the store and the queue-side reader can
// never disagree about the keys. The cves key is present only when the origin
// names CVE ids: an enqueued first pass stamps no set and always runs full,
// and a reopen that names no ids does the same.
func FanOutStampJSON(origin FanOutOrigin) ([]byte, error) {
	stamp := map[string]any{
		"scope_id":      origin.SourceScopeID,
		"generation_id": origin.SourceGenerationID,
		"intent_id":     origin.SourceIntentID,
	}
	if len(origin.CVEIDs) > 0 {
		stamp[FanOutStampCVEKey] = origin.CVEIDs
	}
	return json.Marshal(stamp)
}

// clearReplayFlag clears the pass's replay flag when it proved its rewrite
// clear of the change that flagged it (#9740). A scoped pass (#9779) proved
// it by construction: it re-derived exactly the stamped set under current
// evidence. Any other stamped pass proves it by disjointness: none of the
// CVE ids it derived (every row it wrote plus retracted copies) is in the
// stamp. A stamp without a set, the sentinel, a truncated load, or any
// continuation debt rides to the next claim instead: those fail closed to a
// rerun, because a partial load may have missed the flagged change.
func (h SupplyChainImpactHandler) clearReplayFlag(
	ctx context.Context,
	intent reducercontract.Intent,
	write SupplyChainImpactWriteResult,
	stampCVEs []string,
	seen map[string]any,
) (bool, error) {
	if h.FanOut == nil || seen == nil || len(stampCVEs) == 0 || fanOutStampSentinel(stampCVEs) {
		return false, nil
	}
	if !write.EvidenceComplete {
		return false, nil
	}
	if len(write.ScopedCVEIDs) == 0 {
		derived := unionSortedStrings(write.DerivedCVEIDs, write.SuppressionCVEIDs)
		if overlapsStrings(derived, stampCVEs) {
			return false, nil
		}
	}
	cleared, err := h.FanOut.ClearReplayFlagIfStampSeen(ctx, intent, seen)
	if err != nil {
		return false, fanOutError{fmt.Errorf("clear supply chain impact replay flag: %w", err)}
	}
	return cleared, nil
}
