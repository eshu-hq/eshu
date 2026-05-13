package eks

import (
	"strings"
	"time"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}

func vpcConfigMap(config VPCConfig) map[string]any {
	if config.VPCID == "" && len(config.SubnetIDs) == 0 &&
		len(config.SecurityGroupIDs) == 0 && config.ClusterSecurityGroupID == "" {
		return nil
	}
	return map[string]any{
		"cluster_security_group_id": strings.TrimSpace(config.ClusterSecurityGroupID),
		"endpoint_private_access":   config.EndpointPrivateAccess,
		"endpoint_public_access":    config.EndpointPublicAccess,
		"public_access_cidrs":       cloneStrings(config.PublicAccessCIDRs),
		"security_group_ids":        cloneStrings(config.SecurityGroupIDs),
		"subnet_ids":                cloneStrings(config.SubnetIDs),
		"vpc_id":                    strings.TrimSpace(config.VPCID),
	}
}

func scalingConfigMap(config ScalingConfig) map[string]any {
	return map[string]any{
		"desired_size": config.DesiredSize,
		"max_size":     config.MaxSize,
		"min_size":     config.MinSize,
	}
}
