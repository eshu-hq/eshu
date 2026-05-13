package ec2

import "strings"

func cidrBlockAssociationMaps(associations []CIDRBlockAssociation) []map[string]any {
	if len(associations) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(associations))
	for _, association := range associations {
		output = append(output, map[string]any{
			"association_id": strings.TrimSpace(association.AssociationID),
			"cidr_block":     strings.TrimSpace(association.CIDRBlock),
			"state":          strings.TrimSpace(association.State),
		})
	}
	return output
}

func ipv6CIDRBlockAssociationMaps(associations []IPv6CIDRBlockAssociation) []map[string]any {
	if len(associations) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(associations))
	for _, association := range associations {
		output = append(output, map[string]any{
			"association_id":       strings.TrimSpace(association.AssociationID),
			"cidr_block":           strings.TrimSpace(association.CIDRBlock),
			"ipv6_pool":            strings.TrimSpace(association.IPv6Pool),
			"network_border_group": strings.TrimSpace(association.NetworkBorderGroup),
			"state":                strings.TrimSpace(association.State),
		})
	}
	return output
}

func referencedSecurityGroupMap(group *ReferencedSecurityGroup) map[string]any {
	if group == nil {
		return nil
	}
	return map[string]any{
		"group_id":                  strings.TrimSpace(group.GroupID),
		"peering_status":            strings.TrimSpace(group.PeeringStatus),
		"user_id":                   strings.TrimSpace(group.UserID),
		"vpc_id":                    strings.TrimSpace(group.VPCID),
		"vpc_peering_connection_id": strings.TrimSpace(group.VPCPeeringConnectionID),
	}
}

func attachmentMap(attachment *NetworkInterfaceAttachment) map[string]any {
	if attachment == nil {
		return nil
	}
	return map[string]any{
		"attached_resource_arn":  strings.TrimSpace(attachment.AttachedResourceARN),
		"attached_resource_type": strings.TrimSpace(attachment.AttachedResourceType),
		"attach_time":            timeOrNil(attachment.AttachTime),
		"attachment_id":          strings.TrimSpace(attachment.ID),
		"delete_on_termination":  attachment.DeleteOnTermination,
		"device_index":           attachment.DeviceIndex,
		"instance_id":            strings.TrimSpace(attachment.InstanceID),
		"instance_owner_id":      strings.TrimSpace(attachment.InstanceOwnerID),
		"network_card_index":     attachment.NetworkCardIndex,
		"status":                 strings.TrimSpace(attachment.Status),
	}
}

func securityGroupRefMaps(groups []SecurityGroupRef) []map[string]string {
	if len(groups) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(groups))
	for _, group := range groups {
		output = append(output, map[string]string{
			"group_id":   strings.TrimSpace(group.ID),
			"group_name": strings.TrimSpace(group.Name),
		})
	}
	return output
}

func privateIPAddressMaps(addresses []PrivateIPAddress) []map[string]any {
	if len(addresses) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		output = append(output, map[string]any{
			"private_dns_name": strings.TrimSpace(address.PrivateDNSName),
			"private_ip":       strings.TrimSpace(address.Address),
			"primary":          address.Primary,
		})
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}
