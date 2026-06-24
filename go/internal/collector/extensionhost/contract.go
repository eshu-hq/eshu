// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"errors"

	"github.com/eshu-hq/eshu/go/internal/component"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func sdkContract(manifest component.Manifest) (sdkcollector.Contract, error) {
	contract := sdkcollector.Contract{
		ProtocolVersion: manifest.Spec.Runtime.SDKProtocol,
		Facts:           make([]sdkcollector.FactDeclaration, 0, len(manifest.Spec.EmittedFacts)),
	}
	for _, fact := range manifest.Spec.EmittedFacts {
		declaration := sdkcollector.FactDeclaration{
			Kind:             fact.Kind,
			SchemaVersions:   append([]string(nil), fact.SchemaVersions...),
			SourceConfidence: make([]sdkcollector.SourceConfidence, 0, len(fact.SourceConfidence)),
		}
		for _, confidence := range fact.SourceConfidence {
			declaration.SourceConfidence = append(
				declaration.SourceConfidence,
				sdkcollector.SourceConfidence(confidence),
			)
		}
		contract.Facts = append(contract.Facts, declaration)
	}
	if len(contract.Facts) == 0 {
		return sdkcollector.Contract{}, errors.New("component must declare at least one emitted fact family")
	}
	return contract, nil
}
