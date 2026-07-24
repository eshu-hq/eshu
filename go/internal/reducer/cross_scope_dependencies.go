// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"errors"
	"fmt"
)

// CrossScopeDependency declares that a consumer reducer domain reads canonical
// facts a producer domain writes in a DIFFERENT ingestion scope. The consumer's
// cross-scope active-fact load only resolves once the producer scope's
// generation is active, so the two are ordered by generation activation rather
// than by being enqueued together.
//
// This is a declarative contract only. It records the dependency and validates
// it; the readiness-defer (a consumer whose producer scope is not yet
// quiescent-active returns a non-counting retry instead of writing an empty-join
// decision) and the activation-driven re-enqueue that consume this declaration
// land in follow-up slices of #5709. Until then every domain still relies on the
// bootstrap maintenance reopen, so adding a declaration here changes no runtime
// behavior.
type CrossScopeDependency struct {
	// ProducerDomains are the reducer domains whose canonical output this
	// consumer domain reads across scopes. A consumer resolves only after every
	// listed producer's generation for the relevant scope is active.
	ProducerDomains []Domain
}

// Validate reports whether the declared dependency names at least one producer
// and references only registered producer domains. An empty or unregistered
// declaration is a registration-time error, not a silent no-op, so a typo in the
// catalog fails the build rather than disabling the future readiness gate.
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
// reducer domains depend, across scopes, on which producer domains. It exists so
// the readiness-defer and activation re-enqueue slices of #5709 derive the same
// producer set the consumer handler and the queue claim path both need, with no
// drift between them.
//
// ci_cd_run_correlation reads container_image_identity output to anchor a run to
// its image; container_image_identity is projected in the OCI/cloud scope while
// the correlation runs in the CI scope, so the correlation cannot resolve until
// the identity generation is active. supply_chain_impact reads the correlation
// output for its deployment context, one hop further along the same chain.
func crossScopeDependencyCatalog() map[Domain]CrossScopeDependency {
	return map[Domain]CrossScopeDependency{
		DomainCICDRunCorrelation: {
			ProducerDomains: []Domain{DomainContainerImageIdentity},
		},
	}
}

// crossScopeDependenciesForRegistration returns the DomainDefinition
// CrossScopeDependencies a consumer domain should register, populated from the
// single-source catalog. A domain with no catalog entry registers nil. Domain
// definition constructors call this so the registered DomainDefinition carries
// its declared producers — the readiness/re-enqueue slices read the dependency
// off the registry, not the standalone catalog, so an unwired constructor would
// silently permit the early empty-join execution this contract prevents.
func crossScopeDependenciesForRegistration(domain Domain) []CrossScopeDependency {
	dependency, ok := crossScopeDependencyCatalog()[domain]
	if !ok {
		return nil
	}
	return []CrossScopeDependency{dependency}
}
