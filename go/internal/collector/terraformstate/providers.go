package terraformstate

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type providerBinding struct {
	ResourceAddress       string
	ProviderAddress       string
	ProviderSourceAddress string
	ProviderHostname      string
	ProviderNamespace     string
	ProviderType          string
	ProviderAlias         string
}

func (p *stateParser) emitProviderBinding(resourceAddress string, providerAddress string) error {
	resourceAddress = strings.TrimSpace(resourceAddress)
	providerAddress = strings.TrimSpace(providerAddress)
	if resourceAddress == "" || providerAddress == "" {
		return nil
	}
	binding := parseProviderBinding(resourceAddress, providerAddress)
	providerHash := providerAddressHash(binding.ProviderAddress)
	payload := map[string]any{
		"resource_address": binding.ResourceAddress,
		"provider_address": binding.ProviderAddress,
	}
	if binding.ProviderSourceAddress != "" {
		payload["provider_source_address"] = binding.ProviderSourceAddress
	}
	if binding.ProviderHostname != "" {
		payload["provider_hostname"] = binding.ProviderHostname
	}
	if binding.ProviderNamespace != "" {
		payload["provider_namespace"] = binding.ProviderNamespace
	}
	if binding.ProviderType != "" {
		payload["provider_type"] = binding.ProviderType
	}
	if binding.ProviderAlias != "" {
		payload["provider_alias"] = binding.ProviderAlias
	}
	stableKey := "provider_binding:" + binding.ResourceAddress + ":" + providerHash
	sourceRecordID := binding.ResourceAddress + ":provider:" + providerHash
	return p.emitBodyFact(p.envelope(facts.TerraformStateProviderBindingFactKind, stableKey, payload, sourceRecordID))
}

func parseProviderBinding(resourceAddress string, providerAddress string) providerBinding {
	binding := providerBinding{
		ResourceAddress: resourceAddress,
		ProviderAddress: providerAddress,
	}
	sourceAddress, alias := parseProviderConfigAddress(providerAddress)
	binding.ProviderSourceAddress = sourceAddress
	binding.ProviderAlias = alias
	parts := strings.Split(sourceAddress, "/")
	if len(parts) == 3 {
		binding.ProviderHostname = parts[0]
		binding.ProviderNamespace = parts[1]
		binding.ProviderType = parts[2]
	}
	return binding
}

func parseProviderConfigAddress(providerAddress string) (string, string) {
	const prefix = `provider["`
	providerAddress = strings.TrimSpace(providerAddress)
	remainder, ok := strings.CutPrefix(providerAddress, prefix)
	if !ok {
		return "", ""
	}
	end := strings.Index(remainder, `"]`)
	if end < 0 {
		return "", ""
	}
	sourceAddress := strings.TrimSpace(remainder[:end])
	alias := ""
	if suffix := strings.TrimSpace(remainder[end+2:]); strings.HasPrefix(suffix, ".") {
		alias = strings.TrimSpace(strings.TrimPrefix(suffix, "."))
	}
	return sourceAddress, alias
}

func providerAddressHash(providerAddress string) string {
	return facts.StableID("TerraformStateProviderAddress", map[string]any{
		"provider_address": providerAddress,
	})
}
