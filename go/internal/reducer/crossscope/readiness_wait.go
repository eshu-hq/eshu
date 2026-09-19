// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// Readiness-wait outcome labels for eshu_dp_reducer_readiness_waits_total. The
// set is closed.
const (
	// ReadinessWaitDeferred counts an evaluation that returned the
	// non-counting not-ready error, after committing any ready edges.
	ReadinessWaitDeferred = "deferred"
	// ReadinessWaitAbandoned counts the evaluation that settled a missing set
	// because the elapsed-time bound since the first defer expired. It fires
	// once per (scope, domain, missing set).
	ReadinessWaitAbandoned = "abandoned"
	// ReadinessWaitSettledMissing counts an evaluation that committed at once
	// because its missing set equals one that already settled, so it neither
	// deferred nor polled.
	ReadinessWaitSettledMissing = "settled_missing"
)

// ReadinessWaitMaxKeys caps the missing keys a ledger row stores. The
// fingerprint always covers the full set. A row holding fewer keys than
// MissingCount is not poll-eligible, so its polls fall back to the full
// evaluation.
const ReadinessWaitMaxKeys = 500

// ReadinessWait is one (scope, domain) row of the readiness-wait ledger
// (#6785). The ledger outlives the per-generation queue row, so a wait that
// spans generations keeps its first-defer anchor when a newer generation
// supersedes the deferred row.
type ReadinessWait struct {
	ScopeID string
	Domain  reducercontract.Domain
	// FirstDeferredAt is the bound's anchor. It survives supersession and is
	// reset only when a settled or cleared wait sees a new missing set.
	FirstDeferredAt time.Time
	// AnchorEpoch counts anchor resets, settles, and clears. A write carries
	// the epoch its evaluation read (plus one when it resets the anchor,
	// settles, or clears the row), and the store drops a write whose epoch is
	// below the stored one, so a lease-expired straggler cannot restore an
	// anchor that a reset or clear replaced (review P3-1) or un-settle a
	// settled wait (review P3-a).
	AnchorEpoch int64
	// MissingKeys is the sorted missing set, capped at ReadinessWaitMaxKeys.
	MissingKeys []string
	// MissingCount is the size of the full missing set.
	MissingCount int
	// MissingFingerprint is a SHA-256 over the full sorted missing set.
	MissingFingerprint string
	// CommittedGenerationID, CommittedCycleStartedAt, and CommittedFingerprint
	// mark the last partial commit. An evaluation whose generation, queue
	// cycle, and missing fingerprint all match has nothing new to write.
	CommittedGenerationID   string
	CommittedCycleStartedAt time.Time
	CommittedFingerprint    string
	// SettledAt is set once the bound expired for MissingFingerprint.
	SettledAt time.Time
	// ClearedAt is set when the missing set emptied. A cleared row holds no
	// missing set; it stays as a tombstone so its AnchorEpoch still fences
	// stragglers that read the wait before the clear.
	ClearedAt time.Time
	UpdatedAt time.Time
}

// Settled reports whether this wait's bound already expired.
func (w ReadinessWait) Settled() bool { return !w.SettledAt.IsZero() }

// Cleared reports whether this row is a clear tombstone with no missing set.
func (w ReadinessWait) Cleared() bool { return !w.ClearedAt.IsZero() }

// CommittedInGeneration reports whether the ledger records an earlier commit
// of generationID for this (scope, domain). A re-commit in the same
// generation must retract before it rewrites: the resolved edge set can
// shrink between evaluations (a CAN_PERFORM target scope activating a newer
// generation without a bucket), so the first-generation retract skip is safe
// only for the generation's first commit (review P3-2).
func CommittedInGeneration(existing ReadinessWait, found bool, generationID string) bool {
	return found && generationID != "" && existing.CommittedGenerationID == generationID
}

// KeysComplete reports whether MissingKeys holds the whole missing set.
func (w ReadinessWait) KeysComplete() bool { return len(w.MissingKeys) == w.MissingCount }

// ReadinessWaitLedger persists readiness waits keyed by (scope_id, domain).
// The reducer claim fence is (scope, domain), so at most one live worker
// writes a key at a time; the only concurrent writer is a lease-expired
// straggler, which AnchorEpoch fences. Implementations must return an error,
// never a missing row, when they cannot answer.
type ReadinessWaitLedger interface {
	// GetReadinessWait returns the row and whether it exists. A cleared
	// tombstone is returned as found with Cleared() true.
	GetReadinessWait(ctx context.Context, scopeID string, domain reducercontract.Domain) (ReadinessWait, bool, error)
	// UpsertReadinessWait inserts or updates the row. A write whose
	// AnchorEpoch is below the stored epoch is dropped. A higher epoch
	// replaces FirstDeferredAt; an equal epoch keeps the earlier anchor.
	UpsertReadinessWait(ctx context.Context, wait ReadinessWait) error
	// ClearReadinessWait turns the row into a tombstone with the next epoch,
	// only when the stored epoch still equals wait.AnchorEpoch. Clearing an
	// absent or already-advanced row is a no-op, not an error.
	ClearReadinessWait(ctx context.Context, wait ReadinessWait) error
}

// WaitInput is one evaluation's view for DecideWait.
type WaitInput struct {
	// Existing is the ledger row read before the evaluation; Found reports
	// whether one existed.
	Existing ReadinessWait
	Found    bool
	ScopeID  string
	Domain   reducercontract.Domain
	// GenerationID and CycleStartedAt identify the claimed queue row.
	// CycleStartedAt changes on a reopen or a graph rebuild re-drive, which
	// forces a re-commit because the prior commit's edges may be gone.
	GenerationID   string
	CycleStartedAt time.Time
	// Missing is the evaluation's missing set in any order, duplicates allowed.
	Missing []string
	Now     time.Time
	// MaxWait is the bound; zero or negative means ProducerReadinessMaxWait.
	MaxWait time.Duration
}

// WaitDecision tells a handler what to do, in this order: commit (the
// scope-wide retract and rewrite), then write the ledger (Clear, or Upsert
// Row), then return the not-ready error when Defer is set, or
// succeed otherwise. Writing the ledger after the graph commit means a crash
// between them causes one idempotent re-commit, never a missed one.
type WaitDecision struct {
	Commit bool
	Defer  bool
	Clear  bool
	Upsert bool
	// ResetAnchor reports that Row starts a new bound; Row.AnchorEpoch is then
	// one above the epoch the evaluation read.
	ResetAnchor bool
	// Row is the row to upsert, or for Clear the identity, read epoch, and
	// commit marker of the tombstone.
	Row ReadinessWait
	// Outcome is "" when nothing is missing, else one of the ReadinessWait*
	// labels.
	Outcome string
	// Elapsed is the time since FirstDeferredAt, for logs.
	Elapsed time.Duration
}

// DecideWait is the shared commit-first readiness decision for cross-scope
// edge handlers (#6785). It is pure: the caller reads the ledger, evaluates
// the missing set, and applies the decision.
//
// Ready edges are committed at the first evaluation of every generation, so a
// missing endpoint never holds back other edges or the retraction of a revoked
// one. The wait only governs when a late endpoint is added: the row polls
// until the missing set empties or the bound since FirstDeferredAt expires,
// and a settled set later commits at once without polling.
func DecideWait(in WaitInput) WaitDecision {
	missing := sortedDistinctKeys(in.Missing)
	if len(missing) == 0 {
		if !in.Found || in.Existing.Cleared() {
			return WaitDecision{Commit: true}
		}
		return WaitDecision{Commit: true, Clear: true, Row: ReadinessWait{
			ScopeID: in.ScopeID, Domain: in.Domain, AnchorEpoch: in.Existing.AnchorEpoch,
			CommittedGenerationID: in.GenerationID, CommittedCycleStartedAt: in.CycleStartedAt,
			ClearedAt: in.Now, UpdatedAt: in.Now,
		}}
	}
	fingerprint := missingFingerprint(missing)
	if in.Found && in.Existing.Settled() && in.Existing.MissingFingerprint == fingerprint {
		return WaitDecision{
			Commit: true, Row: in.Existing, Outcome: ReadinessWaitSettledMissing,
			Elapsed: in.Now.Sub(in.Existing.FirstDeferredAt),
		}
	}

	decision := WaitDecision{}
	row := ReadinessWait{
		ScopeID:            in.ScopeID,
		Domain:             in.Domain,
		FirstDeferredAt:    in.Now,
		MissingKeys:        capKeys(missing),
		MissingCount:       len(missing),
		MissingFingerprint: fingerprint,
		UpdatedAt:          in.Now,
	}
	if in.Found {
		row.CommittedGenerationID = in.Existing.CommittedGenerationID
		row.CommittedCycleStartedAt = in.Existing.CommittedCycleStartedAt
		row.CommittedFingerprint = in.Existing.CommittedFingerprint
		row.AnchorEpoch = in.Existing.AnchorEpoch
		if in.Existing.Settled() || in.Existing.Cleared() {
			// A settled or cleared wait that sees a new missing set starts a
			// new bound, under the next epoch.
			decision.ResetAnchor = true
			row.AnchorEpoch++
		} else {
			row.FirstDeferredAt = in.Existing.FirstDeferredAt
		}
	}

	committedCurrent := row.CommittedGenerationID == in.GenerationID &&
		sameInstant(row.CommittedCycleStartedAt, in.CycleStartedAt) &&
		row.CommittedFingerprint == fingerprint
	decision.Commit = !committedCurrent
	if decision.Commit {
		row.CommittedGenerationID = in.GenerationID
		row.CommittedCycleStartedAt = in.CycleStartedAt
		row.CommittedFingerprint = fingerprint
	}

	maxWait := in.MaxWait
	if maxWait <= 0 {
		maxWait = ProducerReadinessMaxWait
	}
	decision.Elapsed = in.Now.Sub(row.FirstDeferredAt)
	settledNow := decision.Elapsed >= maxWait
	if settledNow {
		// Settling moves to the next epoch, so a straggler that read the
		// unsettled row cannot write settled_at back to NULL and make the next
		// evaluation settle (and count abandoned) a second time (review P3-a).
		row.SettledAt = in.Now
		row.AnchorEpoch++
		decision.Outcome = ReadinessWaitAbandoned
	} else {
		decision.Defer = true
		decision.Outcome = ReadinessWaitDeferred
	}
	decision.Upsert = !in.Found || decision.Commit || settledNow || decision.ResetAnchor ||
		in.Existing.MissingFingerprint != fingerprint
	if !decision.Upsert {
		row.UpdatedAt = in.Existing.UpdatedAt
	}
	decision.Row = row
	return decision
}

// PollEligible reports whether an evaluation may take the cheap poll path:
// re-check only the ledger's missing keys, skipping the fact load and
// extraction. It requires an unsettled row whose last commit is this
// generation and queue cycle at the row's own missing set, with every key
// stored.
func PollEligible(existing ReadinessWait, found bool, generationID string, cycleStartedAt time.Time) bool {
	return found && !existing.Settled() && existing.MissingCount > 0 && existing.KeysComplete() &&
		existing.CommittedGenerationID == generationID &&
		sameInstant(existing.CommittedCycleStartedAt, cycleStartedAt) &&
		existing.CommittedFingerprint == existing.MissingFingerprint
}

// SameMissingSet reports whether keys, in any order and with duplicates, is
// exactly the set the ledger row stores.
func SameMissingSet(existing ReadinessWait, keys []string) bool {
	distinct := sortedDistinctKeys(keys)
	return len(distinct) == existing.MissingCount && missingFingerprint(distinct) == existing.MissingFingerprint
}

// ApplyWaitDecision writes the ledger side of decision: Clear, or Upsert the
// row. Call it only after the graph commit the decision asked for succeeded. A
// nil ledger is a no-op (test wiring).
func ApplyWaitDecision(ctx context.Context, ledger ReadinessWaitLedger, decision WaitDecision, scopeID string, domain reducercontract.Domain) error {
	if ledger == nil {
		return nil
	}
	if decision.Clear {
		if err := ledger.ClearReadinessWait(ctx, decision.Row); err != nil {
			return fmt.Errorf("clear %s readiness wait: %w", domain, err)
		}
		return nil
	}
	if decision.Upsert {
		if err := ledger.UpsertReadinessWait(ctx, decision.Row); err != nil {
			return fmt.Errorf("record %s readiness wait: %w", domain, err)
		}
	}
	return nil
}

// ReadWait returns the ledger row for (scopeID, domain). With a nil ledger it
// synthesizes an unsettled, uncommitted row anchored at cycleAnchor, so every
// evaluation commits and the wait is bounded by the queue row's own cycle
// (test wiring only; production wires the Postgres ledger). A zero anchor
// reads as no row.
func ReadWait(ctx context.Context, ledger ReadinessWaitLedger, scopeID string, domain reducercontract.Domain, cycleAnchor time.Time) (ReadinessWait, bool, error) {
	if ledger == nil {
		if cycleAnchor.IsZero() {
			return ReadinessWait{}, false, nil
		}
		return ReadinessWait{ScopeID: scopeID, Domain: domain, FirstDeferredAt: cycleAnchor}, true, nil
	}
	wait, found, err := ledger.GetReadinessWait(ctx, scopeID, domain)
	if err != nil {
		return ReadinessWait{}, false, fmt.Errorf("read %s readiness wait: %w", domain, err)
	}
	return wait, found, nil
}

// MissingSample returns at most limit keys of a missing set, for logs.
func MissingSample(keys []string, limit int) []string {
	if len(keys) > limit {
		keys = keys[:limit]
	}
	return append([]string(nil), keys...)
}

// sameInstant compares timestamps at the microsecond precision Postgres
// timestamptz stores, so a value round-tripped through the ledger still
// matches the claim's CycleStartedAt.
func sameInstant(a, b time.Time) bool {
	return a.Truncate(time.Microsecond).Equal(b.Truncate(time.Microsecond))
}

func sortedDistinctKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func capKeys(keys []string) []string {
	if len(keys) > ReadinessWaitMaxKeys {
		keys = keys[:ReadinessWaitMaxKeys]
	}
	return append([]string(nil), keys...)
}

// missingFingerprint hashes the sorted set with a NUL separator, which no
// ARN or anchor key contains.
func missingFingerprint(sorted []string) string {
	hash := sha256.New()
	for _, key := range sorted {
		hash.Write([]byte(key))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
