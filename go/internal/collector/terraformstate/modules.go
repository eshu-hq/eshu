package terraformstate

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
	stableKey := "module:" + moduleAddress + ":resource:" + resourceAddress
	sourceRecordID := moduleAddress + ":resource:" + resourceAddress
	return p.emitBodyFact(p.envelope(facts.TerraformStateModuleFactKind, stableKey, payload, sourceRecordID))
}
