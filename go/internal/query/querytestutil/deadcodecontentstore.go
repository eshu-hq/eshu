// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// FakeDeadCodeContentStore is the dead-code content-read double.
//
// It embeds FakePortContentStore so it satisfies the whole ContentStore surface,
// and overrides the two reads the dead-code scanner actually drives. A symbol
// declared in a _test.go file is not part of the importable package, so a
// handler family that moves out of package query for #6060 could not otherwise
// reach this double and would have to re-declare its own copy. A re-declared
// double that drifts from the real port keeps passing while guarding nothing.
//
// The fields are exported because an unexported field cannot be filled in from
// another package. Package query keeps an unexported adapter under the field
// names its existing tests already use, and that adapter delegates here.
//
// The zero value is usable: an unset map answers with no entity and no incoming
// edge rather than panicking.
type FakeDeadCodeContentStore struct {
	FakePortContentStore

	// Entities answers GetEntityContent, keyed by entity ID.
	Entities map[string]querycontract.EntityContent
	// IncomingEntityIDs marks which entity IDs have an incoming edge.
	IncomingEntityIDs map[string]bool
}

// GetEntityContent returns a COPY of the stored entity, or a nil entity with a
// nil error when the ID is unknown.
//
// The copy matters: callers receive a pointer, and handing back the address of
// the map's value would let one test's mutation reach another's fixture.
func (f FakeDeadCodeContentStore) GetEntityContent(
	_ context.Context,
	entityID string,
) (*querycontract.EntityContent, error) {
	entity, ok := f.Entities[entityID]
	if !ok {
		return nil, nil
	}
	cloned := entity
	return &cloned, nil
}

// DeadCodeIncomingEntityIDs reports the strongest incoming edge for each
// requested entity ID, omitting IDs with no incoming edge.
//
// Omission rather than a zero-valued entry is the contract the scanner reads:
// a present key means reachable, so answering every requested ID would make
// every candidate look reachable.
func (f FakeDeadCodeContentStore) DeadCodeIncomingEntityIDs(
	_ context.Context,
	_ string,
	entityIDs []string,
) (map[string]querycontract.DeadCodeIncomingEdge, error) {
	incoming := make(map[string]querycontract.DeadCodeIncomingEdge)
	for _, entityID := range entityIDs {
		if f.IncomingEntityIDs[entityID] {
			incoming[entityID] = querycontract.DeadCodeIncomingEdge{
				MaxConfidence: codeprovenance.LegacyConfidence,
				Method:        codeprovenance.MethodUnspecified,
			}
		}
	}
	return incoming, nil
}
