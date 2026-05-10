package terraformstate

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type moduleObservation struct {
	ResourceCount int64
}

func (p *stateParser) recordModuleObservation(moduleAddress string) {
	moduleAddress = strings.TrimSpace(moduleAddress)
	if moduleAddress == "" {
		return
	}
	if p.modules == nil {
		p.modules = map[string]moduleObservation{}
	}
	observation := p.modules[moduleAddress]
	observation.ResourceCount++
	p.modules[moduleAddress] = observation
}

func (p *stateParser) emitModules() {
	if len(p.modules) == 0 {
		return
	}
	moduleAddresses := make([]string, 0, len(p.modules))
	for moduleAddress := range p.modules {
		moduleAddresses = append(moduleAddresses, moduleAddress)
	}
	sort.Strings(moduleAddresses)

	for _, moduleAddress := range moduleAddresses {
		observation := p.modules[moduleAddress]
		payload := map[string]any{
			"module_address": moduleAddress,
			"resource_count": observation.ResourceCount,
		}
		p.facts = append(p.facts, p.envelope(facts.TerraformStateModuleFactKind, "module:"+moduleAddress, payload, moduleAddress))
	}
}
