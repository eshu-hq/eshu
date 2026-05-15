package reducer

import (
	"fmt"
	"strings"
)

var knownDomains = map[Domain]struct{}{
	DomainWorkloadIdentity:               {},
	DomainDeployableUnitCorrelation:      {},
	DomainCloudAssetResolution:           {},
	DomainDeploymentMapping:              {},
	DomainDataLineage:                    {},
	DomainOwnership:                      {},
	DomainGovernance:                     {},
	DomainWorkloadMaterialization:        {},
	DomainCodeCallMaterialization:        {},
	DomainSemanticEntityMaterialization:  {},
	DomainSQLRelationshipMaterialization: {},
	DomainInheritanceMaterialization:     {},
	DomainConfigStateDrift:               {},
	DomainPackageSourceCorrelation:       {},
	DomainContainerImageIdentity:         {},
	DomainAWSCloudRuntimeDrift:           {},
}

// ParseDomain converts one raw string into a known reducer domain.
func ParseDomain(raw string) (Domain, error) {
	domain := Domain(strings.TrimSpace(raw))
	if err := domain.Validate(); err != nil {
		return "", err
	}
	return domain, nil
}

// Validate checks that the reducer domain is explicit and known.
func (domain Domain) Validate() error {
	if strings.TrimSpace(string(domain)) == "" {
		return fmt.Errorf("domain must not be blank")
	}
	if _, ok := knownDomains[domain]; !ok {
		return fmt.Errorf("unknown reducer domain %q", domain)
	}
	return nil
}
