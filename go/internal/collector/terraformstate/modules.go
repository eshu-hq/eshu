// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	tfstatev1 "github.com/eshu-hq/eshu/sdk/go/factschema/terraformstate/v1"
)

func (p *stateParser) emitModuleObservation(moduleAddress string, resourceAddress string) error {
	moduleAddress = strings.TrimSpace(moduleAddress)
	if moduleAddress == "" {
		return nil
	}
	payload := map[string]any{
		"module_address": moduleAddress,
		"resource_count": int64(1),
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeTerraformStateModule(tfstatev1.Module{
			ModuleAddress: moduleAddress,
			ResourceCount: int64Ptr(1),
		})
	}); err != nil {
		return err
	}
	stableKey := "module:" + moduleAddress + ":resource:" + resourceAddress
	sourceRecordID := moduleAddress + ":resource:" + resourceAddress
	if err := p.emitBodyFact(p.envelope(facts.TerraformStateModuleFactKind, stableKey, payload, sourceRecordID)); err != nil {
		return err
	}
	p.moduleFacts++
	return nil
}
