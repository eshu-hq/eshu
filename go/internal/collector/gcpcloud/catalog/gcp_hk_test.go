// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

// TestHKConstantsMatchGCPCloudValues pins every HK-category constant (compute
// lane H: address, backend service, instance, network, disk, firewall,
// forwarding rule, health check, interconnect attachment, network endpoint
// group, route, router, security policy, SSL certificate, subnetwork, target
// HTTPS proxy, URL map, VPN gateway, VPN tunnel; security lane K: API key,
// Certificate Manager certificate, IAM role, Identity Platform config, KMS
// crypto key, KMS key ring, org policy, reCAPTCHA key, Secret Manager secret,
// secret version, service account key, workload identity pool (+ provider),
// API gateway) to the exact value it carried in package gcpcloud before the
// #6696 catalog consolidation. The consolidation moves declarations only: any
// value drift here is a behavior change, not a move.
//
// One source declaration folds rather than minting a new identifier: the
// function-local `versionsMarker` in extractor_secret_version.go carries the
// identical "/versions/" value as the package-level
// `secretVersionVersionsMarker`, so its uses bind to
// SecretVersionVersionsMarker after the move.
func TestHKConstantsMatchGCPCloudValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "AssetTypeComputeAddress", got: AssetTypeComputeAddress, want: "compute.googleapis.com/Address"},
		{name: "AssetTypeComputeGlobalAddress", got: AssetTypeComputeGlobalAddress, want: "compute.googleapis.com/GlobalAddress"},
		{name: "AssetTypeComputeForwardingRule", got: AssetTypeComputeForwardingRule, want: "compute.googleapis.com/ForwardingRule"},
		{name: "AssetTypeComputeGlobalForwardingRule", got: AssetTypeComputeGlobalForwardingRule, want: "compute.googleapis.com/GlobalForwardingRule"},
		{name: "RelationshipTypeAddressInNetwork", got: RelationshipTypeAddressInNetwork, want: "address_in_network"},
		{name: "RelationshipTypeAddressInSubnetwork", got: RelationshipTypeAddressInSubnetwork, want: "address_in_subnetwork"},
		{name: "RelationshipTypeAddressUsedByForwardingRule", got: RelationshipTypeAddressUsedByForwardingRule, want: "address_used_by_forwarding_rule"},
		{name: "RelationshipTypeAddressUsedByInstance", got: RelationshipTypeAddressUsedByInstance, want: "address_used_by_instance"},
		{name: "AssetTypeComputeRegionBackendService", got: AssetTypeComputeRegionBackendService, want: "compute.googleapis.com/RegionBackendService"},
		{name: "AssetTypeComputeInstanceGroup", got: AssetTypeComputeInstanceGroup, want: "compute.googleapis.com/InstanceGroup"},
		{name: "AssetTypeComputeNetworkEndpointGroup", got: AssetTypeComputeNetworkEndpointGroup, want: "compute.googleapis.com/NetworkEndpointGroup"},
		{name: "AssetTypeComputeHealthCheck", got: AssetTypeComputeHealthCheck, want: "compute.googleapis.com/HealthCheck"},
		{name: "AssetTypeComputeSecurityPolicy", got: AssetTypeComputeSecurityPolicy, want: "compute.googleapis.com/SecurityPolicy"},
		{name: "RelationshipTypeBackendServiceHasBackend", got: RelationshipTypeBackendServiceHasBackend, want: "backend_service_has_backend"},
		{name: "RelationshipTypeBackendServiceUsesHealthCheck", got: RelationshipTypeBackendServiceUsesHealthCheck, want: "backend_service_uses_health_check"},
		{name: "RelationshipTypeBackendServiceUsesSecurityPolicy", got: RelationshipTypeBackendServiceUsesSecurityPolicy, want: "backend_service_uses_security_policy"},
		{name: "RelationshipTypeBackendServiceUsesEdgeSecurityPolicy", got: RelationshipTypeBackendServiceUsesEdgeSecurityPolicy, want: "backend_service_uses_edge_security_policy"},
		{name: "RelationshipTypeInstanceUsesDisk", got: RelationshipTypeInstanceUsesDisk, want: "instance_uses_disk"},
		{name: "RelationshipTypeInstanceInNetwork", got: RelationshipTypeInstanceInNetwork, want: "instance_in_network"},
		{name: "RelationshipTypeInstanceInSubnetwork", got: RelationshipTypeInstanceInSubnetwork, want: "instance_in_subnetwork"},
		{name: "RelationshipTypeNetworkContainsSubnetwork", got: RelationshipTypeNetworkContainsSubnetwork, want: "vpc_network_contains_subnetwork"},
		{name: "RelationshipTypeNetworkPeersWithNetwork", got: RelationshipTypeNetworkPeersWithNetwork, want: "vpc_network_peers_with_network"},
		{name: "PeeringStateActive", got: PeeringStateActive, want: "ACTIVE"},
		{name: "AssetTypeComputeDisk", got: AssetTypeComputeDisk, want: "compute.googleapis.com/Disk"},
		{name: "AssetTypeComputeInstance", got: AssetTypeComputeInstance, want: "compute.googleapis.com/Instance"},
		{name: "AssetTypeComputeImage", got: AssetTypeComputeImage, want: "compute.googleapis.com/Image"},
		{name: "AssetTypeComputeSnapshot", got: AssetTypeComputeSnapshot, want: "compute.googleapis.com/Snapshot"},
		{name: "RelationshipTypeDiskAttachedToInstance", got: RelationshipTypeDiskAttachedToInstance, want: "disk_attached_to_instance"},
		{name: "RelationshipTypeDiskCreatedFromImage", got: RelationshipTypeDiskCreatedFromImage, want: "disk_created_from_image"},
		{name: "RelationshipTypeDiskCreatedFromSnapshot", got: RelationshipTypeDiskCreatedFromSnapshot, want: "disk_created_from_snapshot"},
		{name: "RelationshipTypeDiskEncryptedByKey", got: RelationshipTypeDiskEncryptedByKey, want: "disk_encrypted_by_key"},
		{name: "AssetTypeComputeFirewall", got: AssetTypeComputeFirewall, want: "compute.googleapis.com/Firewall"},
		{name: "RelationshipTypeFirewallAppliesToNetwork", got: RelationshipTypeFirewallAppliesToNetwork, want: "firewall_applies_to_network"},
		{name: "AssetTypeComputeBackendService", got: AssetTypeComputeBackendService, want: "compute.googleapis.com/BackendService"},
		{name: "AssetTypeComputeTargetPool", got: AssetTypeComputeTargetPool, want: "compute.googleapis.com/TargetPool"},
		{name: "AssetTypeComputeTargetHTTPProxy", got: AssetTypeComputeTargetHTTPProxy, want: "compute.googleapis.com/TargetHttpProxy"},
		{name: "AssetTypeComputeTargetHTTPSProxy", got: AssetTypeComputeTargetHTTPSProxy, want: "compute.googleapis.com/TargetHttpsProxy"},
		{name: "AssetTypeComputeTargetTCPProxy", got: AssetTypeComputeTargetTCPProxy, want: "compute.googleapis.com/TargetTcpProxy"},
		{name: "AssetTypeComputeTargetSSLProxy", got: AssetTypeComputeTargetSSLProxy, want: "compute.googleapis.com/TargetSslProxy"},
		{name: "AssetTypeComputeTargetGRPCProxy", got: AssetTypeComputeTargetGRPCProxy, want: "compute.googleapis.com/TargetGrpcProxy"},
		{name: "AssetTypeComputeTargetInstance", got: AssetTypeComputeTargetInstance, want: "compute.googleapis.com/TargetInstance"},
		{name: "AssetTypeComputeTargetVPNGateway", got: AssetTypeComputeTargetVPNGateway, want: "compute.googleapis.com/TargetVpnGateway"},
		{name: "AssetTypeComputeServiceAttachment", got: AssetTypeComputeServiceAttachment, want: "compute.googleapis.com/ServiceAttachment"},
		{name: "RelationshipTypeForwardingRuleTargetsBackendService", got: RelationshipTypeForwardingRuleTargetsBackendService, want: "forwarding_rule_targets_backend_service"},
		{name: "RelationshipTypeForwardingRuleTargetsTargetPool", got: RelationshipTypeForwardingRuleTargetsTargetPool, want: "forwarding_rule_targets_target_pool"},
		{name: "RelationshipTypeForwardingRuleTargetsTargetProxy", got: RelationshipTypeForwardingRuleTargetsTargetProxy, want: "forwarding_rule_targets_target_proxy"},
		{name: "RelationshipTypeForwardingRuleTargetsTargetInstance", got: RelationshipTypeForwardingRuleTargetsTargetInstance, want: "forwarding_rule_targets_target_instance"},
		{name: "RelationshipTypeForwardingRuleTargetsTargetVPNGateway", got: RelationshipTypeForwardingRuleTargetsTargetVPNGateway, want: "forwarding_rule_targets_target_vpn_gateway"},
		{name: "RelationshipTypeForwardingRuleTargetsServiceAttachment", got: RelationshipTypeForwardingRuleTargetsServiceAttachment, want: "forwarding_rule_targets_service_attachment"},
		{name: "RelationshipTypeForwardingRuleInNetwork", got: RelationshipTypeForwardingRuleInNetwork, want: "forwarding_rule_in_network"},
		{name: "RelationshipTypeForwardingRuleInSubnetwork", got: RelationshipTypeForwardingRuleInSubnetwork, want: "forwarding_rule_in_subnetwork"},
		{name: "AssetTypeComputeInterconnect", got: AssetTypeComputeInterconnect, want: "compute.googleapis.com/Interconnect"},
		{name: "RelationshipTypeInterconnectAttachmentUsesRouter", got: RelationshipTypeInterconnectAttachmentUsesRouter, want: "interconnect_attachment_uses_router"},
		{name: "RelationshipTypeInterconnectAttachmentUsesInterconnect", got: RelationshipTypeInterconnectAttachmentUsesInterconnect, want: "interconnect_attachment_uses_interconnect"},
		{name: "RelationshipTypeInterconnectAttachmentUsesNetwork", got: RelationshipTypeInterconnectAttachmentUsesNetwork, want: "interconnect_attachment_uses_network"},
		{name: "RelationshipTypeNetworkEndpointGroupInNetwork", got: RelationshipTypeNetworkEndpointGroupInNetwork, want: "network_endpoint_group_in_network"},
		{name: "RelationshipTypeNetworkEndpointGroupInSubnetwork", got: RelationshipTypeNetworkEndpointGroupInSubnetwork, want: "network_endpoint_group_in_subnetwork"},
		{name: "RelationshipTypeNetworkEndpointGroupTargetsServerlessService", got: RelationshipTypeNetworkEndpointGroupTargetsServerlessService, want: "network_endpoint_group_targets_serverless_service"},
		{name: "RelationshipTypeNetworkEndpointGroupTargetsServiceAttachment", got: RelationshipTypeNetworkEndpointGroupTargetsServiceAttachment, want: "network_endpoint_group_targets_service_attachment"},
		{name: "NetworkEndpointGroupServerlessCloudRun", got: NetworkEndpointGroupServerlessCloudRun, want: "cloud_run"},
		{name: "NetworkEndpointGroupServerlessAppEngine", got: NetworkEndpointGroupServerlessAppEngine, want: "app_engine"},
		{name: "NetworkEndpointGroupServerlessCloudFunction", got: NetworkEndpointGroupServerlessCloudFunction, want: "cloud_function"},
		{name: "AssetTypeComputeRoute", got: AssetTypeComputeRoute, want: "compute.googleapis.com/Route"},
		{name: "AssetTypeComputeVpnTunnel", got: AssetTypeComputeVpnTunnel, want: "compute.googleapis.com/VpnTunnel"},
		{name: "RelationshipTypeRouteInNetwork", got: RelationshipTypeRouteInNetwork, want: "route_in_network"},
		{name: "RelationshipTypeRouteNextHopInstance", got: RelationshipTypeRouteNextHopInstance, want: "route_next_hop_instance"},
		{name: "RelationshipTypeRouteNextHopVpnTunnel", got: RelationshipTypeRouteNextHopVpnTunnel, want: "route_next_hop_vpn_tunnel"},
		{name: "RelationshipTypeRouteNextHopIlb", got: RelationshipTypeRouteNextHopIlb, want: "route_next_hop_ilb"},
		{name: "AssetTypeComputeRouter", got: AssetTypeComputeRouter, want: "compute.googleapis.com/Router"},
		{name: "AssetTypeComputeInterconnectAttachment", got: AssetTypeComputeInterconnectAttachment, want: "compute.googleapis.com/InterconnectAttachment"},
		{name: "RelationshipTypeRouterInNetwork", got: RelationshipTypeRouterInNetwork, want: "router_in_network"},
		{name: "RelationshipTypeRouterInterfaceLinkedVpnTunnel", got: RelationshipTypeRouterInterfaceLinkedVpnTunnel, want: "router_interface_linked_vpn_tunnel"},
		{name: "RelationshipTypeRouterInterfaceLinkedInterconnectAttachment", got: RelationshipTypeRouterInterfaceLinkedInterconnectAttachment, want: "router_interface_linked_interconnect_attachment"},
		{name: "RelationshipTypeRouterInterfaceSubnetwork", got: RelationshipTypeRouterInterfaceSubnetwork, want: "router_interface_subnetwork"},
		{name: "AssetTypeComputeSSLCertificate", got: AssetTypeComputeSSLCertificate, want: "compute.googleapis.com/SslCertificate"},
		{name: "SslCertificateTypeSelfManaged", got: SslCertificateTypeSelfManaged, want: "SELF_MANAGED"},
		{name: "RelationshipTypeSubnetworkInNetwork", got: RelationshipTypeSubnetworkInNetwork, want: "subnetwork_in_network"},
		{name: "AssetTypeComputeSSLPolicy", got: AssetTypeComputeSSLPolicy, want: "compute.googleapis.com/SslPolicy"},
		{name: "AssetTypeCertificateManagerCertificateMap", got: AssetTypeCertificateManagerCertificateMap, want: "certificatemanager.googleapis.com/CertificateMap"},
		{name: "AssetTypeCertificateManagerCertificate", got: AssetTypeCertificateManagerCertificate, want: "certificatemanager.googleapis.com/Certificate"},
		{name: "CertificateManagerFullNamePrefix", got: CertificateManagerFullNamePrefix, want: "//certificatemanager.googleapis.com/"},
		{name: "RelationshipTypeTargetHTTPSProxyUsesURLMap", got: RelationshipTypeTargetHTTPSProxyUsesURLMap, want: "target_https_proxy_uses_url_map"},
		{name: "RelationshipTypeTargetHTTPSProxyUsesSSLCertificate", got: RelationshipTypeTargetHTTPSProxyUsesSSLCertificate, want: "target_https_proxy_uses_ssl_certificate"},
		{name: "RelationshipTypeTargetHTTPSProxyUsesSSLPolicy", got: RelationshipTypeTargetHTTPSProxyUsesSSLPolicy, want: "target_https_proxy_uses_ssl_policy"},
		{name: "RelationshipTypeTargetHTTPSProxyUsesCertificateMap", got: RelationshipTypeTargetHTTPSProxyUsesCertificateMap, want: "target_https_proxy_uses_certificate_map"},
		{name: "RelationshipTypeTargetHTTPSProxyUsesCertManagerCertificate", got: RelationshipTypeTargetHTTPSProxyUsesCertManagerCertificate, want: "target_https_proxy_uses_certificate_manager_certificate"},
		{name: "AssetTypeComputeUrlMap", got: AssetTypeComputeUrlMap, want: "compute.googleapis.com/UrlMap"},
		{name: "AssetTypeComputeBackendBucket", got: AssetTypeComputeBackendBucket, want: "compute.googleapis.com/BackendBucket"},
		{name: "RelationshipTypeURLMapDefaultService", got: RelationshipTypeURLMapDefaultService, want: "url_map_default_service"},
		{name: "RelationshipTypeURLMapPathMatcherService", got: RelationshipTypeURLMapPathMatcherService, want: "url_map_path_matcher_default_service"},
		{name: "RelationshipTypeURLMapPathRuleService", got: RelationshipTypeURLMapPathRuleService, want: "url_map_path_rule_service"},
		{name: "RelationshipTypeURLMapRouteRuleService", got: RelationshipTypeURLMapRouteRuleService, want: "url_map_route_rule_service"},
		{name: "RelationshipTypeURLMapRouteRuleWeightedService", got: RelationshipTypeURLMapRouteRuleWeightedService, want: "url_map_route_rule_weighted_service"},
		{name: "AssetTypeComputeVPNGateway", got: AssetTypeComputeVPNGateway, want: "compute.googleapis.com/VpnGateway"},
		{name: "RelationshipTypeVPNGatewayInNetwork", got: RelationshipTypeVPNGatewayInNetwork, want: "vpn_gateway_in_network"},
		{name: "AssetTypeComputeExternalVPNGateway", got: AssetTypeComputeExternalVPNGateway, want: "compute.googleapis.com/ExternalVpnGateway"},
		{name: "RelationshipTypeVpnTunnelUsesVpnGateway", got: RelationshipTypeVpnTunnelUsesVpnGateway, want: "vpn_tunnel_uses_vpn_gateway"},
		{name: "RelationshipTypeVpnTunnelUsesTargetVpnGateway", got: RelationshipTypeVpnTunnelUsesTargetVpnGateway, want: "vpn_tunnel_uses_target_vpn_gateway"},
		{name: "RelationshipTypeVpnTunnelPeersWithVpnGateway", got: RelationshipTypeVpnTunnelPeersWithVpnGateway, want: "vpn_tunnel_peers_with_vpn_gateway"},
		{name: "RelationshipTypeVpnTunnelUsesRouter", got: RelationshipTypeVpnTunnelUsesRouter, want: "vpn_tunnel_uses_router"},
		{name: "AssetTypeAPIGatewayGateway", got: AssetTypeAPIGatewayGateway, want: "apigateway.googleapis.com/Gateway"},
		{name: "AssetTypeAPIGatewayAPIConfig", got: AssetTypeAPIGatewayAPIConfig, want: "apigateway.googleapis.com/ApiConfig"},
		{name: "RelationshipTypeAPIGatewayUsesAPIConfig", got: RelationshipTypeAPIGatewayUsesAPIConfig, want: "api_gateway_uses_api_config"},
		{name: "ApiGatewayAPIConfigFullResourceNamePrefix", got: ApiGatewayAPIConfigFullResourceNamePrefix, want: "//apigateway.googleapis.com/"},
		{name: "ApiGatewayAPIConfigRelativeMarker", got: ApiGatewayAPIConfigRelativeMarker, want: "/apis/"},
		{name: "ApiGatewayLocationsMarker", got: ApiGatewayLocationsMarker, want: "/locations/"},
		{name: "ApiKeyAssetType", got: ApiKeyAssetType, want: "apikeys.googleapis.com/Key"},
		{name: "AssetTypeCertificateManagerDNSAuthorization", got: AssetTypeCertificateManagerDNSAuthorization, want: "certificatemanager.googleapis.com/DnsAuthorization"},
		{name: "AssetTypeCertificateManagerCertificateIssuanceConfig", got: AssetTypeCertificateManagerCertificateIssuanceConfig, want: "certificatemanager.googleapis.com/CertificateIssuanceConfig"},
		{name: "AssetTypeCertificateManagerCertificateMapEntry", got: AssetTypeCertificateManagerCertificateMapEntry, want: "certificatemanager.googleapis.com/CertificateMapEntry"},
		{name: "CertManagerCertificateTypeManaged", got: CertManagerCertificateTypeManaged, want: "MANAGED"},
		{name: "CertManagerCertificateTypeSelfManaged", got: CertManagerCertificateTypeSelfManaged, want: "SELF_MANAGED"},
		{name: "CertManagerCertificateTypeManagedIdentity", got: CertManagerCertificateTypeManagedIdentity, want: "MANAGED_IDENTITY"},
		{name: "RelationshipTypeCertManagerCertificateUsesDNSAuthorization", got: RelationshipTypeCertManagerCertificateUsesDNSAuthorization, want: "certificate_manager_certificate_uses_dns_authorization"},
		{name: "RelationshipTypeCertManagerCertificateUsesIssuanceConfig", got: RelationshipTypeCertManagerCertificateUsesIssuanceConfig, want: "certificate_manager_certificate_uses_issuance_config"},
		{name: "RelationshipTypeCertManagerCertificateUsedByCertificateMapEntry", got: RelationshipTypeCertManagerCertificateUsedByCertificateMapEntry, want: "certificate_manager_certificate_used_by_certificate_map_entry"},
		{name: "RelationshipTypeCertManagerCertificateUsedByTargetHTTPSProxy", got: RelationshipTypeCertManagerCertificateUsedByTargetHTTPSProxy, want: "certificate_manager_certificate_used_by_target_https_proxy"},
		{name: "IamRoleAssetType", got: IamRoleAssetType, want: "iam.googleapis.com/Role"},
		{name: "IdentityConfigAssetType", got: IdentityConfigAssetType, want: "identitytoolkit.googleapis.com/Config"},
		{name: "AssetTypeKMSKeyRing", got: AssetTypeKMSKeyRing, want: "cloudkms.googleapis.com/KeyRing"},
		{name: "RelationshipTypeCryptoKeyInKeyRing", got: RelationshipTypeCryptoKeyInKeyRing, want: "kms_crypto_key_in_key_ring"},
		{name: "KmsCryptoKeyPurposeEncryptDecrypt", got: KmsCryptoKeyPurposeEncryptDecrypt, want: "ENCRYPT_DECRYPT"},
		{name: "KmsKeyRingLocationsSegment", got: KmsKeyRingLocationsSegment, want: "/locations/"},
		{name: "KmsKeyRingKeyRingsSegment", got: KmsKeyRingKeyRingsSegment, want: "/keyRings/"},
		{name: "OrgPolicyAssetType", got: OrgPolicyAssetType, want: "orgpolicy.googleapis.com/Policy"},
		{name: "AssetTypeCloudResourceManagerOrganization", got: AssetTypeCloudResourceManagerOrganization, want: "cloudresourcemanager.googleapis.com/Organization"},
		{name: "AssetTypeCloudResourceManagerFolder", got: AssetTypeCloudResourceManagerFolder, want: "cloudresourcemanager.googleapis.com/Folder"},
		{name: "RelationshipTypeOrgPolicyAppliesToResource", got: RelationshipTypeOrgPolicyAppliesToResource, want: "org_policy_applies_to_resource"},
		{name: "OrgPolicyFullResourcePoliciesMarker", got: OrgPolicyFullResourcePoliciesMarker, want: "/policies/"},
		{name: "OrgPolicyFullResourceNamePrefix", got: OrgPolicyFullResourceNamePrefix, want: "//orgpolicy.googleapis.com/"},
		{name: "RecaptchaKeyAssetType", got: RecaptchaKeyAssetType, want: "recaptchaenterprise.googleapis.com/Key"},
		{name: "RelationshipTypeSecretEncryptedByKMSKey", got: RelationshipTypeSecretEncryptedByKMSKey, want: "secret_encrypted_by_kms_key"},
		{name: "RelationshipTypeSecretNotifiesTopic", got: RelationshipTypeSecretNotifiesTopic, want: "secret_notifies_topic"},
		{name: "SecretVersionAssetType", got: SecretVersionAssetType, want: "secretmanager.googleapis.com/SecretVersion"},
		{name: "AssetTypeKMSCryptoKeyVersion", got: AssetTypeKMSCryptoKeyVersion, want: "cloudkms.googleapis.com/CryptoKeyVersion"},
		{name: "RelationshipTypeSecretVersionOfSecret", got: RelationshipTypeSecretVersionOfSecret, want: "secret_version_of_secret"},
		{name: "RelationshipTypeSecretVersionEncryptedByKMSKeyVersion", got: RelationshipTypeSecretVersionEncryptedByKMSKeyVersion, want: "secret_version_encrypted_by_kms_key_version"},
		{name: "SecretVersionVersionsMarker", got: SecretVersionVersionsMarker, want: "/versions/"},
		{name: "ServiceAccountKeyAssetType", got: ServiceAccountKeyAssetType, want: "iam.googleapis.com/ServiceAccountKey"},
		{name: "RelationshipTypeServiceAccountKeyOf", got: RelationshipTypeServiceAccountKeyOf, want: "service_account_key_of"},
		{name: "ServiceAccountKeyKeysMarker", got: ServiceAccountKeyKeysMarker, want: "/keys/"},
		{name: "WorkloadIdentityPoolAssetType", got: WorkloadIdentityPoolAssetType, want: "iam.googleapis.com/WorkloadIdentityPool"},
		{name: "WorkloadIdentityPoolProviderAssetType", got: WorkloadIdentityPoolProviderAssetType, want: "iam.googleapis.com/WorkloadIdentityPoolProvider"},
		{name: "RelationshipTypeWIFProviderOfPool", got: RelationshipTypeWIFProviderOfPool, want: "workload_identity_provider_of_pool"},
		{name: "WifProvidersMarker", got: WifProvidersMarker, want: "/providers/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}

	intTests := []struct {
		name string
		got  int
		want int
	}{
		{name: "AwsDefaultAttributeMappingKeyCount", got: AwsDefaultAttributeMappingKeyCount, want: 2},
	}
	for _, tt := range intTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}
