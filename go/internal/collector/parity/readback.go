// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// admissionStatus is the result of offering one committed fact to the readback
// model.
type admissionStatus string

const (
	admissionAdmitted   admissionStatus = "admitted"
	admissionIdempotent admissionStatus = "idempotent"
	admissionSuperseded admissionStatus = "superseded"
	admissionWithheld   admissionStatus = "withheld"
)

// admittedFact is one readable row keyed by stable fact key.
type admittedFact struct {
	factKind     string
	fencingToken int64
}

// readbackStore is an in-memory model of the reducer readback contract shared by
// all claim-driven collectors. It is intentionally domain-agnostic: it enforces
// only the universal guarantees that hosted readback must hold so the harness can
// fail when fixture facts cannot reach them.
//
//   - Stable-key idempotency: the same stable key at the same fencing token is a
//     no-op replay, not a duplicate row (mirrors facts.Envelope.StableFactKey
//     first-writer-wins reducer admission).
//   - Fencing supersede: a lower fencing token for an existing key is rejected as
//     stale; a higher token replaces the row (mirrors facts.Envelope.FencingToken
//     generation fencing).
//   - Non-admissible withholding: permission-hidden and unsupported facts are
//     recorded as evidence but never become readable.
type readbackStore struct {
	admitted map[string]admittedFact // stable key -> readable row
	withheld map[string]int          // class -> count
}

func newReadbackStore() *readbackStore {
	return &readbackStore{
		admitted: map[string]admittedFact{},
		withheld: map[string]int{},
	}
}

// offer applies one committed fact and its class to the readback model.
func (r *readbackStore) offer(envelope facts.Envelope, class FactClass) admissionStatus {
	if class != FactAdmissible {
		r.withheld[string(class)]++
		return admissionWithheld
	}
	key := readbackIdentity(envelope)
	existing, ok := r.admitted[key]
	if ok {
		switch {
		case envelope.FencingToken < existing.fencingToken:
			return admissionSuperseded
		case envelope.FencingToken == existing.fencingToken:
			return admissionIdempotent
		}
	}
	r.admitted[key] = admittedFact{factKind: envelope.FactKind, fencingToken: envelope.FencingToken}
	return admissionAdmitted
}

// readbackIdentity scopes the admission key by source identity. The fact
// envelope contract only guarantees StableFactKey uniqueness within one
// collector kind and scope, so admission must key on (collector kind, scope id,
// stable key) — otherwise the same fixture key emitted by two scopes or
// families would collapse into one readable row.
func readbackIdentity(envelope facts.Envelope) string {
	key := envelope.StableFactKey
	if key == "" {
		key = envelope.FactID
	}
	return envelope.CollectorKind + "\x00" + envelope.ScopeID + "\x00" + key
}

// readableFactKinds returns the sorted distinct fact kinds currently readable.
func (r *readbackStore) readableFactKinds() []string {
	seen := map[string]struct{}{}
	for _, fact := range r.admitted {
		if fact.factKind == "" {
			continue
		}
		seen[fact.factKind] = struct{}{}
	}
	kinds := make([]string, 0, len(seen))
	for kind := range seen {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// readableCount returns the number of distinct readable rows.
func (r *readbackStore) readableCount() int {
	return len(r.admitted)
}
