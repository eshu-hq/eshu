// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// DecodeReducerPackageOwnershipCorrelation decodes one persisted
// reducer_package_ownership_correlation envelope through the contracts seam.
func DecodeReducerPackageOwnershipCorrelation(
	env facts.Envelope,
) (reducerderivedv1.PackageOwnershipCorrelation, error) {
	correlation, err := factschema.DecodeReducerPackageOwnershipCorrelation(FactschemaEnvelope(env))
	if err != nil {
		return reducerderivedv1.PackageOwnershipCorrelation{},
			factdecode.NewFactDecodeError(factschema.FactKindReducerPackageOwnershipCorrelation, err)
	}
	return correlation, nil
}

// DecodeReducerPackageConsumptionCorrelation decodes one persisted
// reducer_package_consumption_correlation envelope through the contracts seam.
func DecodeReducerPackageConsumptionCorrelation(
	env facts.Envelope,
) (reducerderivedv1.PackageConsumptionCorrelation, error) {
	correlation, err := factschema.DecodeReducerPackageConsumptionCorrelation(FactschemaEnvelope(env))
	if err != nil {
		return reducerderivedv1.PackageConsumptionCorrelation{},
			factdecode.NewFactDecodeError(factschema.FactKindReducerPackageConsumptionCorrelation, err)
	}
	return correlation, nil
}

// DecodeReducerPackagePublicationCorrelation decodes one persisted
// reducer_package_publication_correlation envelope through the contracts seam.
func DecodeReducerPackagePublicationCorrelation(
	env facts.Envelope,
) (reducerderivedv1.PackagePublicationCorrelation, error) {
	correlation, err := factschema.DecodeReducerPackagePublicationCorrelation(FactschemaEnvelope(env))
	if err != nil {
		return reducerderivedv1.PackagePublicationCorrelation{},
			factdecode.NewFactDecodeError(factschema.FactKindReducerPackagePublicationCorrelation, err)
	}
	return correlation, nil
}
