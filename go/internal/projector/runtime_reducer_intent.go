// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func buildReducerIntent(fact facts.Envelope) (ReducerIntent, bool) {
	domainValue, ok := payloadString(fact.Payload, "reducer_domain")
	if !ok {
		domainValue, ok = payloadString(fact.Payload, "shared_domain")
		if !ok {
			return ReducerIntent{}, false
		}
	}
	domain, err := reducer.ParseDomain(domainValue)
	if err != nil {
		return ReducerIntent{}, false
	}

	entityKey, _ := payloadString(fact.Payload, "entity_key")
	reason, _ := payloadString(fact.Payload, "reason")

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
