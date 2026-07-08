// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// decodeReducerPackageOwnershipCorrelation decodes one persisted
// reducer_package_ownership_correlation envelope through the contracts seam.
func decodeReducerPackageOwnershipCorrelation(
	env facts.Envelope,
) (reducerderivedv1.PackageOwnershipCorrelation, error) {
	correlation, err := factschema.DecodeReducerPackageOwnershipCorrelation(factschemaEnvelope(env))
	if err != nil {
		return reducerderivedv1.PackageOwnershipCorrelation{},
			newFactDecodeError(factschema.FactKindReducerPackageOwnershipCorrelation, err)
	}
	return correlation, nil
}

// decodeReducerPackageConsumptionCorrelation decodes one persisted
// reducer_package_consumption_correlation envelope through the contracts seam.
func decodeReducerPackageConsumptionCorrelation(
	env facts.Envelope,
) (reducerderivedv1.PackageConsumptionCorrelation, error) {
	correlation, err := factschema.DecodeReducerPackageConsumptionCorrelation(factschemaEnvelope(env))
	if err != nil {
		return reducerderivedv1.PackageConsumptionCorrelation{},
			newFactDecodeError(factschema.FactKindReducerPackageConsumptionCorrelation, err)
	}
	return correlation, nil
}

// decodeReducerPackagePublicationCorrelation decodes one persisted
// reducer_package_publication_correlation envelope through the contracts seam.
func decodeReducerPackagePublicationCorrelation(
	env facts.Envelope,
) (reducerderivedv1.PackagePublicationCorrelation, error) {
	correlation, err := factschema.DecodeReducerPackagePublicationCorrelation(factschemaEnvelope(env))
	if err != nil {
		return reducerderivedv1.PackagePublicationCorrelation{},
			newFactDecodeError(factschema.FactKindReducerPackagePublicationCorrelation, err)
	}
	return correlation, nil
}
