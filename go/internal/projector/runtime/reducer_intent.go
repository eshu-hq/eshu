// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/decode"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildReducerIntent converts a fact carrying a reducer routing payload into the
// intent the reducer queue consumes, accepting either reducer_domain or the
// shared_domain spelling. The second result is false when the fact names no
// domain or names one the reducer does not recognize, so an unroutable fact is
// skipped rather than enqueued against a wrong domain.
func BuildReducerIntent(fact facts.Envelope) (ReducerIntent, bool) {
	domainValue, ok := decode.PayloadString(fact.Payload, "reducer_domain")
	if !ok {
		domainValue, ok = decode.PayloadString(fact.Payload, "shared_domain")
		if !ok {
			return ReducerIntent{}, false
		}
	}
	domain, err := reducer.ParseDomain(domainValue)
	if err != nil {
		return ReducerIntent{}, false
	}

	entityKey, _ := decode.PayloadString(fact.Payload, "entity_key")
	reason, _ := decode.PayloadString(fact.Payload, "reason")

	return ReducerIntent{
		ScopeID:      fact.ScopeID,
		GenerationID: fact.GenerationID,
		Domain:       domain,
		EntityKey:    entityKey,
		Reason:       reason,
		FactID:       fact.FactID,
		SourceSystem: fact.SourceRef.SourceSystem,
	}, true
}
