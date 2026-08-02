// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"errors"
	"fmt"
	"slices"
)

// CrossScopeDependency declares that a consumer reducer domain reads canonical
// facts a producer domain writes in a DIFFERENT ingestion scope. The consumer's
// cross-scope active-fact load can run before the producer has committed its
// latest output, so producer completion must schedule the canonical consumer
// again.
//
// The durable producer-completion runner consumes this declaration to schedule
// current-generation consumers after a producer ACK. It does not gate reducer
// admission: cross-scope producer readiness is not available when these intents
// are first enqueued, so convergence comes from ordered replay instead.
type CrossScopeDependency struct {
	// ProducerDomains are the reducer domains whose canonical output this
	// consumer domain reads across scopes. Each successful producer ACK schedules
	// the consumer again; this is convergence ordering, not an admission gate.
	ProducerDomains []Domain
}

// Validate reports whether the declared dependency names at least one producer
// and references only registered producer domains. An empty or unregistered
// declaration is a registration-time error, not a silent no-op, so a typo in the
// catalog fails the build rather than disabling completion-driven replay.
func (d CrossScopeDependency) Validate() error {
	if len(d.ProducerDomains) == 0 {
		return errors.New("cross-scope dependency must name at least one producer domain")
	}
	for _, producer := range d.ProducerDomains {
		if err := producer.Validate(); err != nil {
			return fmt.Errorf("cross-scope dependency producer %q: %w", producer, err)
		}
	}

	return nil
}

// crossScopeDependencyCatalog is the single source of truth for which consumer
// reducer domains depend, across scopes, on which producer domains. The durable
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
// (projector/supply_chain_impact_intents.go), and matchingSupplyChainDeployments
// rejects a correlation that has not yet resolved its artifact identity, so a
// finding classified before the CI producer commits
// keeps an empty environments list until producer completion schedules the
// canonical consumer again. Identity remains in deferred maintenance because
// its raw OCI producer has no reducer ACK; its successful replay starts this
// reducer-owned completion chain.
func crossScopeDependencyCatalog() map[Domain]CrossScopeDependency {
	return map[Domain]CrossScopeDependency{
		DomainCICDRunCorrelation: {
			ProducerDomains: []Domain{DomainContainerImageIdentity},
		},
		DomainSupplyChainImpact: {
			ProducerDomains: []Domain{
				DomainContainerImageIdentity,
				DomainCICDRunCorrelation,
			},
		},
	}
}

// CrossScopeConsumerDomains returns every reducer domain that crossScopeDependencyCatalog
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
func CrossScopeConsumerDomains() []Domain {
	catalog := crossScopeDependencyCatalog()
	consumers := make([]Domain, 0, len(catalog))
	for consumer := range catalog {
		consumers = append(consumers, consumer)
	}
	slices.Sort(consumers)

	return consumers
}

// CrossScopeCompletionEdge is one producer-to-consumer fanout edge derived
// from the cross-scope dependency catalog.
type CrossScopeCompletionEdge struct {
	Producer Domain
	Consumer Domain
}

// CrossScopeCompletionEdges returns every producer-to-consumer completion edge
// in deterministic producer, then consumer, order.
func CrossScopeCompletionEdges() []CrossScopeCompletionEdge {
	catalog := crossScopeDependencyCatalog()
	edges := make([]CrossScopeCompletionEdge, 0, len(catalog)*2)
	for consumer, dependency := range catalog {
		for _, producer := range dependency.ProducerDomains {
			edges = append(edges, CrossScopeCompletionEdge{
				Producer: producer,
				Consumer: consumer,
			})
		}
	}
	slices.SortFunc(edges, func(left, right CrossScopeCompletionEdge) int {
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

// crossScopeDependenciesForRegistration returns the DomainDefinition
// CrossScopeDependencies a consumer domain should register, populated from the
// single-source catalog. A domain with no catalog entry registers nil. Domain
// definition constructors call this so the registered DomainDefinition and the
// completion fanout share one declaration rather than parallel hard-coded
// dependency lists.
func crossScopeDependenciesForRegistration(domain Domain) []CrossScopeDependency {
	dependency, ok := crossScopeDependencyCatalog()[domain]
	if !ok {
		return nil
	}
	return []CrossScopeDependency{dependency}
}
