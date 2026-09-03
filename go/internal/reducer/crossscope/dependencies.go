// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	"slices"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// dependencyCatalog is the single source of truth for which consumer reducer
// domains depend, across scopes, on which producer domains. The durable
// completion fanout derives its SQL inputs from this catalog, so registration
// and runtime scheduling cannot drift.
//
// ci_cd_run_correlation reads container_image_identity output to anchor a run to
// its image; container_image_identity is projected in the OCI/cloud scope while
// the correlation runs in the CI scope, so an early correlation can miss the
// identity producer's latest committed output. supply_chain_impact reads identity output
// directly for repository anchoring and correlation output for its deployment
// context, one hop further along the same chain: its
// intent is triggered by its own vulnerability scope's facts
// (projector/supplychainimpact/impact_intents.go), and matchingSupplyChainDeployments
// rejects a correlation that has not yet resolved its artifact identity, so a
// finding classified before the CI producer commits
// keeps an empty environments list until producer completion schedules the
// canonical consumer again. Identity remains in deferred maintenance because
// its raw OCI producer has no reducer ACK; its successful replay starts this
// reducer-owned completion chain.
func dependencyCatalog() map[reducercontract.Domain]reducercontract.CrossScopeDependency {
	return map[reducercontract.Domain]reducercontract.CrossScopeDependency{
		reducercontract.DomainCICDRunCorrelation: {
			ProducerDomains: []reducercontract.Domain{reducercontract.DomainContainerImageIdentity},
		},
		reducercontract.DomainSupplyChainImpact: {
			ProducerDomains: []reducercontract.Domain{
				reducercontract.DomainContainerImageIdentity,
				reducercontract.DomainCICDRunCorrelation,
			},
		},
	}
}

// ConsumerDomains returns every reducer domain that the dependency catalog
// declares as a CONSUMER of another domain's canonical output, sorted so the
// result does not depend on map iteration order.
//
// The durable completion runner consumes the corresponding deterministic edge
// list. This accessor remains useful to registration and contract tests that
// reason about the consumer set as a whole.
//
// DefaultDomainDefinitions is deliberately not the source for this: the additive
// domains that carry the dependency are wired only by
// implementedDefaultDomainDefinitions with real handlers, so the default
// definitions expose zero CrossScopeDependencies and would report an empty
// consumer set.
func ConsumerDomains() []reducercontract.Domain {
	catalog := dependencyCatalog()
	consumers := make([]reducercontract.Domain, 0, len(catalog))
	for consumer := range catalog {
		consumers = append(consumers, consumer)
	}
	slices.Sort(consumers)

	return consumers
}

// CompletionEdge is one producer-to-consumer fanout edge derived from the
// cross-scope dependency catalog.
type CompletionEdge struct {
	Producer reducercontract.Domain
	Consumer reducercontract.Domain
}

// CompletionEdges returns every producer-to-consumer completion edge in
// deterministic producer, then consumer, order.
func CompletionEdges() []CompletionEdge {
	catalog := dependencyCatalog()
	edges := make([]CompletionEdge, 0, len(catalog)*2)
	for consumer, dependency := range catalog {
		for _, producer := range dependency.ProducerDomains {
			edges = append(edges, CompletionEdge{
				Producer: producer,
				Consumer: consumer,
			})
		}
	}
	slices.SortFunc(edges, func(left, right CompletionEdge) int {
		if byProducer := string(left.Producer) < string(right.Producer); byProducer {
			return -1
		}
		if left.Producer != right.Producer {
			return 1
		}
		if left.Consumer < right.Consumer {
			return -1
		}
		if left.Consumer > right.Consumer {
			return 1
		}
		return 0
	})
	return edges
}

// DependenciesForRegistration returns the DomainDefinition
// CrossScopeDependencies a consumer domain should register, populated from the
// single-source catalog. A domain with no catalog entry registers nil. Domain
// definition constructors call this so the registered DomainDefinition and the
// completion fanout share one declaration rather than parallel hard-coded
// dependency lists.
func DependenciesForRegistration(domain reducercontract.Domain) []reducercontract.CrossScopeDependency {
	dependency, ok := dependencyCatalog()[domain]
	if !ok {
		return nil
	}
	return []reducercontract.CrossScopeDependency{dependency}
}
