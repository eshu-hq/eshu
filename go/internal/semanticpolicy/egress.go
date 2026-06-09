package semanticpolicy

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

const (
	// EgressModeRestricted requires an explicit provider-profile allow rule.
	EgressModeRestricted = "restricted"
	// EgressModeBroad allows provider egress after the source policy allows it.
	EgressModeBroad = "broad"
)

const (
	// EgressDecisionAllow permits matching provider egress.
	EgressDecisionAllow = "allow"
	// EgressDecisionDeny blocks matching provider egress.
	EgressDecisionDeny = "deny"
)

const (
	// ReasonEgressPolicyMissing marks missing provider egress policy.
	ReasonEgressPolicyMissing = "egress_policy_missing"
	// ReasonEgressProviderDenied marks a provider or source class blocked by egress policy.
	ReasonEgressProviderDenied = "egress_provider_denied"
)

// EgressPolicy captures outbound semantic provider egress posture.
type EgressPolicy struct {
	Mode              string               `json:"mode,omitempty"`
	SemanticProviders []EgressProviderRule `json:"semantic_providers,omitempty"`
}

// EgressProviderRule gates one semantic provider profile and source class set.
type EgressProviderRule struct {
	ProviderProfileID string   `json:"provider_profile_id"`
	SourceClasses     []string `json:"source_classes"`
	Decision          string   `json:"decision"`
}

func normalizeEgress(policy EgressPolicy) (EgressPolicy, error) {
	out := EgressPolicy{
		Mode: strings.TrimSpace(policy.Mode),
	}
	if out.Mode == "" && len(policy.SemanticProviders) > 0 {
		out.Mode = EgressModeRestricted
	}
	if out.Mode != "" && !slices.Contains([]string{EgressModeRestricted, EgressModeBroad}, out.Mode) {
		return EgressPolicy{}, fmt.Errorf("egress.mode %q is unsupported", out.Mode)
	}
	if out.Mode == EgressModeBroad && len(policy.SemanticProviders) > 0 {
		return EgressPolicy{}, fmt.Errorf("egress.mode %q must not include semantic_providers rules", out.Mode)
	}
	seen := make(map[string]struct{}, len(policy.SemanticProviders))
	for i, rule := range policy.SemanticProviders {
		normalized, err := normalizeEgressProviderRule(rule)
		if err != nil {
			return EgressPolicy{}, fmt.Errorf("egress.semantic_providers[%d]: %w", i, err)
		}
		key := normalized.ProviderProfileID + ":" + strings.Join(normalized.SourceClasses, ",")
		if _, ok := seen[key]; ok {
			return EgressPolicy{}, fmt.Errorf("egress.semantic_providers[%d] duplicates provider/source-class rule", i)
		}
		seen[key] = struct{}{}
		out.SemanticProviders = append(out.SemanticProviders, normalized)
	}
	return out, nil
}

func normalizeEgressProviderRule(rule EgressProviderRule) (EgressProviderRule, error) {
	out := EgressProviderRule{
		ProviderProfileID: strings.TrimSpace(rule.ProviderProfileID),
		Decision:          strings.TrimSpace(rule.Decision),
	}
	if out.ProviderProfileID == "" {
		return EgressProviderRule{}, fmt.Errorf("provider_profile_id is required")
	}
	if !slices.Contains([]string{EgressDecisionAllow, EgressDecisionDeny}, out.Decision) {
		return EgressProviderRule{}, fmt.Errorf("decision %q is unsupported", out.Decision)
	}
	sourceClasses, err := normalizeSourceClasses(rule.SourceClasses)
	if err != nil {
		return EgressProviderRule{}, fmt.Errorf("source_classes: %w", err)
	}
	if len(sourceClasses) == 0 {
		return EgressProviderRule{}, fmt.Errorf("source_classes must include at least one source class")
	}
	out.SourceClasses = sourceClasses
	return out, nil
}

func egressAllowsRequest(policy EgressPolicy, request Request) (bool, string, string) {
	if policy.Mode == "" && len(policy.SemanticProviders) == 0 {
		return false, ReasonEgressPolicyMissing, "semantic provider egress policy is missing"
	}
	if policy.Mode == EgressModeBroad {
		return true, ReasonAllowed, ""
	}
	var allowMatched bool
	for _, rule := range policy.SemanticProviders {
		if rule.ProviderProfileID != request.ProviderProfileID {
			continue
		}
		if !slices.Contains(rule.SourceClasses, request.SourceClass) {
			continue
		}
		if rule.Decision == EgressDecisionAllow {
			allowMatched = true
			continue
		}
		return false, ReasonEgressProviderDenied, "semantic provider egress is denied"
	}
	if allowMatched {
		return true, ReasonAllowed, ""
	}
	return false, ReasonEgressProviderDenied, "semantic provider egress is not allowlisted"
}

func egressAllowsProfileSourceClass(policy EgressPolicy, providerProfileID, sourceClass string) bool {
	if policy.Mode == EgressModeBroad {
		return true
	}
	var allowMatched bool
	for _, rule := range policy.SemanticProviders {
		if rule.ProviderProfileID != strings.TrimSpace(providerProfileID) {
			continue
		}
		if !slices.Contains(rule.SourceClasses, strings.TrimSpace(sourceClass)) {
			continue
		}
		if rule.Decision == EgressDecisionDeny {
			return false
		}
		allowMatched = true
	}
	return allowMatched
}

func sortEgressProviderRules(rules []EgressProviderRule) {
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].ProviderProfileID == rules[j].ProviderProfileID {
			return strings.Join(rules[i].SourceClasses, ",") < strings.Join(rules[j].SourceClasses, ",")
		}
		return rules[i].ProviderProfileID < rules[j].ProviderProfileID
	})
}
