// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// G1-category constants consolidated from constants_appmesh.go (#6696).
const (
	// ServiceAppMesh identifies the regional AWS App Mesh metadata-only scan
	// slice. The scanner covers meshes, virtual services, virtual nodes,
	// virtual routers, routes, virtual gateways, and gateway routes. It never
	// calls App Mesh mutation APIs and never persists client TLS certificate
	// bodies or sensitive HTTP header match values.
	ServiceAppMesh = "appmesh"
)

const (
	// ResourceTypeAppMeshMesh identifies an App Mesh service mesh metadata
	// resource. The fact carries mesh identity (ARN, name), the mesh owner and
	// resource owner account IDs App Mesh reports, the egress filter type, and
	// the IP preference. App Mesh-internal children join to this resource by its
	// ARN.
	ResourceTypeAppMeshMesh = "aws_appmesh_mesh"

	// ResourceTypeAppMeshVirtualService identifies an App Mesh virtual service
	// metadata resource. The fact carries the virtual service identity (ARN,
	// name), parent mesh name, and the provider kind (virtual node or virtual
	// router) the virtual service routes to.
	ResourceTypeAppMeshVirtualService = "aws_appmesh_virtual_service"

	// ResourceTypeAppMeshVirtualNode identifies an App Mesh virtual node
	// metadata resource. The fact carries node identity (ARN, name), parent mesh
	// name, the service discovery shape (DNS hostname or Cloud Map
	// namespace/service), and the backend virtual service names. Client TLS
	// validation references record ACM Private CA certificate authority ARNs
	// only; the scanner never persists a literal certificate body.
	ResourceTypeAppMeshVirtualNode = "aws_appmesh_virtual_node"

	// ResourceTypeAppMeshVirtualRouter identifies an App Mesh virtual router
	// metadata resource. The fact carries router identity (ARN, name), parent
	// mesh name, and listener port/protocol metadata.
	ResourceTypeAppMeshVirtualRouter = "aws_appmesh_virtual_router"

	// ResourceTypeAppMeshRoute identifies an App Mesh route metadata resource.
	// The fact carries route identity (ARN, name), parent mesh and virtual
	// router names, the route protocol kind, priority, and the route match
	// shape (path prefix, method, header names). Sensitive header match values
	// (Authorization, Cookie, X-Api-Key shaped) are redacted through the shared
	// redact library; the header name is always preserved.
	ResourceTypeAppMeshRoute = "aws_appmesh_route"

	// ResourceTypeAppMeshVirtualGateway identifies an App Mesh virtual gateway
	// metadata resource. The fact carries gateway identity (ARN, name), parent
	// mesh name, and listener port/protocol metadata.
	ResourceTypeAppMeshVirtualGateway = "aws_appmesh_virtual_gateway"

	// ResourceTypeAppMeshGatewayRoute identifies an App Mesh gateway route
	// metadata resource. The fact carries gateway route identity (ARN, name),
	// parent mesh and virtual gateway names, the route protocol kind, and the
	// target virtual service name.
	ResourceTypeAppMeshGatewayRoute = "aws_appmesh_gateway_route"
)

const (
	// RelationshipAppMeshVirtualServiceInMesh records that a virtual service
	// belongs to a service mesh. The target is the parent mesh resource keyed
	// by its App Mesh ARN.
	RelationshipAppMeshVirtualServiceInMesh = "appmesh_virtual_service_in_mesh"

	// RelationshipAppMeshVirtualNodeBackendVirtualService records an outbound
	// backend reference from a virtual node to a virtual service it sends
	// traffic to. The target is the backend virtual service resource keyed by
	// its App Mesh ARN.
	RelationshipAppMeshVirtualNodeBackendVirtualService = "appmesh_virtual_node_backend_virtual_service"

	// RelationshipAppMeshRouteInVirtualRouter records that a route belongs to a
	// virtual router. The target is the parent virtual router resource keyed by
	// its App Mesh ARN.
	RelationshipAppMeshRouteInVirtualRouter = "appmesh_route_in_virtual_router"

	// RelationshipAppMeshVirtualGatewayInMesh records that a virtual gateway
	// belongs to a service mesh. The target is the parent mesh resource keyed by
	// its App Mesh ARN.
	RelationshipAppMeshVirtualGatewayInMesh = "appmesh_virtual_gateway_in_mesh"

	// RelationshipAppMeshVirtualNodeTrustsCertificateAuthority records that a
	// virtual node client TLS policy validates peers against an ACM Private CA
	// certificate authority. App Mesh reports these trust anchors as ACM Private
	// CA ARNs (arn:{partition}:acm-pca:{region}:{account}:certificate-authority/
	// {id}), not public ACM certificate ARNs, so the target keys on the
	// certificate authority ARN. The literal certificate body is never read.
	RelationshipAppMeshVirtualNodeTrustsCertificateAuthority = "appmesh_virtual_node_trusts_certificate_authority"

	// RelationshipAppMeshVirtualNodeUsesCloudMapService records that a virtual
	// node discovers endpoints through an AWS Cloud Map namespace and service.
	// The target is keyed by the Cloud Map namespace/service identity App Mesh
	// reports.
	RelationshipAppMeshVirtualNodeUsesCloudMapService = "appmesh_virtual_node_uses_cloud_map_service"

	// RelationshipAppMeshVirtualNodeUsesDNSHostname records that a virtual node
	// discovers endpoints through a DNS hostname. The target is keyed by the
	// reported hostname.
	RelationshipAppMeshVirtualNodeUsesDNSHostname = "appmesh_virtual_node_uses_dns_hostname"
)

const (
	// ResourceTypeACMPCACertificateAuthority identifies an AWS Certificate
	// Manager Private CA certificate authority. App Mesh client TLS trust ARNs
	// point at ACM Private CA (acm-pca) certificate authorities, not public ACM
	// certificates, so the virtual-node trust edge targets this type. The
	// acm-pca scanner (catalog.ServiceACMPCA) publishes certificate authority resources
	// keyed by the certificate authority ARN, which is the resource_id this
	// constant joins against, so the trust edge resolves.
	ResourceTypeACMPCACertificateAuthority = "aws_acmpca_certificate_authority"

	// TargetTypeCloudMapService is the relationship target_type for an AWS Cloud
	// Map service discovered through an App Mesh virtual node. Cloud Map has no
	// Eshu scanner yet, so the target keys on the namespace/service identity App
	// Mesh reports rather than a Cloud Map ARN.
	TargetTypeCloudMapService = "aws_cloud_map_service"

	// TargetTypeDNSHostname is the relationship target_type for a DNS hostname
	// an App Mesh virtual node uses for service discovery. The hostname is an
	// external name, not an AWS ARN-addressable resource.
	TargetTypeDNSHostname = "dns_hostname"
)

// G1-category constants consolidated from constants_appstream.go (#6696).
const (
	// ServiceAppStream identifies the regional Amazon AppStream 2.0 metadata-only
	// scan slice. The scanner reads fleet, stack, image builder, and image
	// control-plane metadata through the AppStream describe/list management APIs
	// (DescribeFleets, DescribeStacks, DescribeImageBuilders, DescribeImages,
	// ListAssociatedStacks, ListAssociatedFleets, ListTagsForResource) and never
	// reads streaming sessions, user data, or session scripts, and never mutates
	// AppStream state.
	ServiceAppStream = "appstream"
)

const (
	// ResourceTypeAppStreamFleet identifies an Amazon AppStream 2.0 fleet
	// metadata resource. The scanner emits identity, fleet type, instance type,
	// lifecycle state, platform, stream view, internet access, and capacity
	// limits only.
	ResourceTypeAppStreamFleet = "aws_appstream_fleet"
	// ResourceTypeAppStreamStack identifies an Amazon AppStream 2.0 stack
	// metadata resource. The scanner emits identity, display name, persistent
	// application-settings enablement, and the S3 bucket names AppStream reports
	// for app-settings and storage connectors only.
	ResourceTypeAppStreamStack = "aws_appstream_stack"
	// ResourceTypeAppStreamImageBuilder identifies an Amazon AppStream 2.0 image
	// builder metadata resource. The scanner emits identity, instance type,
	// lifecycle state, platform, and the base image ARN only.
	ResourceTypeAppStreamImageBuilder = "aws_appstream_image_builder"
	// ResourceTypeAppStreamImage identifies an Amazon AppStream 2.0 image
	// metadata resource. The scanner emits identity, state, visibility, image
	// type, platform, and the base image ARN only; installed applications and
	// image-permission grants stay outside the contract.
	ResourceTypeAppStreamImage = "aws_appstream_image"
)

const (
	// RelationshipAppStreamFleetUsesSubnet records an AppStream fleet's reported
	// VPC subnet dependency. The target is keyed by the bare subnet id
	// (subnet-...), matching how the EC2 scanner publishes its subnet resource_id.
	RelationshipAppStreamFleetUsesSubnet = "appstream_fleet_uses_subnet"
	// RelationshipAppStreamFleetUsesSecurityGroup records an AppStream fleet's
	// reported VPC security group dependency. The target is keyed by the bare
	// security group id (sg-...), matching the EC2 scanner's published id.
	RelationshipAppStreamFleetUsesSecurityGroup = "appstream_fleet_uses_security_group"
	// RelationshipAppStreamFleetUsesIAMRole records an AppStream fleet's applied
	// IAM role. AWS reports a role ARN, which matches how the IAM scanner
	// publishes its role resource_id.
	RelationshipAppStreamFleetUsesIAMRole = "appstream_fleet_uses_iam_role"
	// RelationshipAppStreamFleetUsesImage records an AppStream fleet's source
	// image. AWS reports the image ARN, which matches how this scanner publishes
	// its image node resource_id.
	RelationshipAppStreamFleetUsesImage = "appstream_fleet_uses_image"
	// RelationshipAppStreamFleetAssociatedWithStack records an AppStream
	// fleet-to-stack association reported by ListAssociatedStacks. The target is
	// keyed by the stack node resource_id (stack ARN when available, else name).
	RelationshipAppStreamFleetAssociatedWithStack = "appstream_fleet_associated_with_stack"
	// RelationshipAppStreamImageBuilderUsesSubnet records an AppStream image
	// builder's reported VPC subnet dependency, keyed by the bare subnet id.
	RelationshipAppStreamImageBuilderUsesSubnet = "appstream_image_builder_uses_subnet"
	// RelationshipAppStreamImageBuilderUsesSecurityGroup records an AppStream
	// image builder's reported VPC security group dependency, keyed by the bare
	// security group id.
	RelationshipAppStreamImageBuilderUsesSecurityGroup = "appstream_image_builder_uses_security_group"
	// RelationshipAppStreamImageBuilderUsesIAMRole records an AppStream image
	// builder's applied IAM role, keyed by the reported role ARN.
	RelationshipAppStreamImageBuilderUsesIAMRole = "appstream_image_builder_uses_iam_role"
	// RelationshipAppStreamImageBuilderUsesImage records an AppStream image
	// builder's base image, keyed by the reported image ARN.
	RelationshipAppStreamImageBuilderUsesImage = "appstream_image_builder_uses_image"
	// RelationshipAppStreamStackUsesS3Bucket records an AppStream stack's reported
	// S3 bucket dependency for persistent application settings or a home-folders
	// storage connector. AppStream reports a bucket NAME, so the scanner
	// synthesizes the partition-aware bucket ARN (arn:<partition>:s3:::<bucket>)
	// to match the S3 scanner's published bucket resource_id.
	RelationshipAppStreamStackUsesS3Bucket = "appstream_stack_uses_s3_bucket"
)

// G1-category constants consolidated from constants_elasticbeanstalk.go (#6696).
const (
	// ServiceElasticBeanstalk identifies the regional AWS Elastic Beanstalk
	// service scan slice.
	ServiceElasticBeanstalk = "elasticbeanstalk"
)

const (
	// ResourceTypeElasticBeanstalkApplication identifies an Elastic Beanstalk
	// application.
	ResourceTypeElasticBeanstalkApplication = "aws_elasticbeanstalk_application"
	// ResourceTypeElasticBeanstalkEnvironment identifies an Elastic Beanstalk
	// environment.
	ResourceTypeElasticBeanstalkEnvironment = "aws_elasticbeanstalk_environment"
	// ResourceTypeElasticBeanstalkApplicationVersion identifies an Elastic
	// Beanstalk application version.
	ResourceTypeElasticBeanstalkApplicationVersion = "aws_elasticbeanstalk_application_version"
)

const (
	// RelationshipElasticBeanstalkEnvironmentBelongsToApplication records the
	// application an environment belongs to.
	RelationshipElasticBeanstalkEnvironmentBelongsToApplication = "elasticbeanstalk_environment_belongs_to_application"
	// RelationshipElasticBeanstalkEnvironmentUsesVPC records the VPC an
	// environment is placed in, derived from the aws:ec2:vpc/VPCId option
	// setting. The target is the EC2-owned aws_ec2_vpc identity.
	RelationshipElasticBeanstalkEnvironmentUsesVPC = "elasticbeanstalk_environment_uses_vpc"
	// RelationshipElasticBeanstalkEnvironmentUsesInstanceProfile records the
	// EC2 instance profile an environment's instances assume, derived from the
	// aws:autoscaling:launchconfiguration/IamInstanceProfile option setting.
	RelationshipElasticBeanstalkEnvironmentUsesInstanceProfile = "elasticbeanstalk_environment_uses_instance_profile"
	// RelationshipElasticBeanstalkEnvironmentUsesServiceRole records the
	// Elastic Beanstalk service role an environment uses, derived from the
	// aws:elasticbeanstalk:environment/ServiceRole option setting.
	RelationshipElasticBeanstalkEnvironmentUsesServiceRole = "elasticbeanstalk_environment_uses_service_role"
	// RelationshipElasticBeanstalkEnvironmentUsesLoadBalancer records a load
	// balancer an environment routes through, reported by
	// DescribeEnvironmentResources. The target is the ELBv2-owned
	// aws_elbv2_load_balancer identity.
	RelationshipElasticBeanstalkEnvironmentUsesLoadBalancer = "elasticbeanstalk_environment_uses_load_balancer"
	// RelationshipElasticBeanstalkEnvironmentUsesAutoScalingGroup records an
	// Auto Scaling group an environment runs on, reported by
	// DescribeEnvironmentResources.
	RelationshipElasticBeanstalkEnvironmentUsesAutoScalingGroup = "elasticbeanstalk_environment_uses_auto_scaling_group"
	// RelationshipElasticBeanstalkEnvironmentUsesLaunchTemplate records an EC2
	// launch template an environment uses, reported by
	// DescribeEnvironmentResources.
	RelationshipElasticBeanstalkEnvironmentUsesLaunchTemplate = "elasticbeanstalk_environment_uses_launch_template"
	// RelationshipElasticBeanstalkEnvironmentRunsVersion records the
	// application version currently deployed in an environment.
	RelationshipElasticBeanstalkEnvironmentRunsVersion = "elasticbeanstalk_environment_runs_version"
)

// G1-category constants consolidated from constants_workspaces.go (#6696).
const (
	// ServiceWorkSpaces identifies the regional Amazon WorkSpaces metadata-only
	// scan slice. The scanner reads control-plane describe APIs
	// (DescribeWorkspaces, DescribeWorkspaceDirectories, DescribeWorkspaceBundles,
	// DescribeIpGroups, DescribeTags) and never reads desktop session contents,
	// user credentials, registration codes, or connection state, and never
	// creates, modifies, reboots, rebuilds, starts, stops, or terminates a
	// WorkSpace or any WorkSpaces resource.
	ServiceWorkSpaces = "workspaces"
)

const (
	// ResourceTypeWorkSpacesWorkspace identifies an Amazon WorkSpaces virtual
	// desktop metadata resource. The scanner emits identity, the parent
	// directory and bundle identifiers, the operational state, the volume
	// encryption configuration, and the (identity-metadata) user name only.
	// Desktop session contents and connection state stay outside the contract.
	ResourceTypeWorkSpacesWorkspace = "aws_workspaces"
	// ResourceTypeWorkSpacesDirectory identifies an Amazon WorkSpaces registered
	// directory metadata resource. This is the WorkSpaces-side registration of a
	// Directory Service directory; the scanner emits identity, registration
	// state, directory type, tenancy, and the network placement references
	// (subnets, security group, IAM role, IP access control groups). The
	// registration code is intentionally excluded.
	ResourceTypeWorkSpacesDirectory = "aws_workspaces_directory"
	// ResourceTypeWorkSpacesBundle identifies an Amazon WorkSpaces bundle
	// metadata resource. The scanner emits identity, the owner, the bundle type,
	// the compute type, the root/user volume sizes, and the backing image
	// identifier only.
	ResourceTypeWorkSpacesBundle = "aws_workspaces_bundle"
	// ResourceTypeWorkSpacesIPGroup identifies an Amazon WorkSpaces IP access
	// control group metadata resource. The scanner emits identity, the group
	// description, and the CIDR access rules (network configuration, not secret
	// material) only.
	ResourceTypeWorkSpacesIPGroup = "aws_workspaces_ip_group"
)

const (
	// RelationshipWorkSpacesWorkspaceInDirectory records a WorkSpace's membership
	// in its parent WorkSpaces directory. The target is keyed by the directory
	// node's published resource_id so the edge joins the directory node exactly.
	RelationshipWorkSpacesWorkspaceInDirectory = "workspaces_workspace_in_directory"
	// RelationshipWorkSpacesWorkspaceUsesBundle records a WorkSpace's dependency
	// on the bundle it was created from. The target is the WorkSpaces bundle
	// node keyed by the bundle node's published resource_id.
	RelationshipWorkSpacesWorkspaceUsesBundle = "workspaces_workspace_uses_bundle"
	// RelationshipWorkSpacesWorkspaceUsesKMSKey records a WorkSpace's reported
	// volume encryption KMS key dependency.
	RelationshipWorkSpacesWorkspaceUsesKMSKey = "workspaces_workspace_uses_kms_key"
	// RelationshipWorkSpacesDirectoryUsesDSDirectory records the WorkSpaces
	// directory's link to the underlying AWS Directory Service directory. The
	// target is the bare Directory Service directory id (for example
	// "d-1234567890") the ds scanner publishes as its resource_id.
	RelationshipWorkSpacesDirectoryUsesDSDirectory = "workspaces_directory_uses_ds_directory"
	// RelationshipWorkSpacesDirectoryInSubnet records a WorkSpaces directory's
	// placement in a VPC subnet. The target is the bare subnet id the ec2
	// scanner publishes.
	RelationshipWorkSpacesDirectoryInSubnet = "workspaces_directory_in_subnet"
	// RelationshipWorkSpacesDirectoryUsesSecurityGroup records the WorkSpaces
	// security group assigned to new WorkSpaces in the directory. The target is
	// the bare security group id the ec2 scanner publishes.
	RelationshipWorkSpacesDirectoryUsesSecurityGroup = "workspaces_directory_uses_security_group"
	// RelationshipWorkSpacesDirectoryUsesIAMRole records the IAM role WorkSpaces
	// assumes to call other services on the account's behalf. The target is the
	// IAM role ARN the iam scanner publishes as its role resource_id.
	RelationshipWorkSpacesDirectoryUsesIAMRole = "workspaces_directory_uses_iam_role"
	// RelationshipWorkSpacesDirectoryUsesIPGroup records a WorkSpaces directory's
	// association with an IP access control group. The target is the WorkSpaces
	// IP access control group node keyed by its published resource_id.
	RelationshipWorkSpacesDirectoryUsesIPGroup = "workspaces_directory_uses_ip_group"
)
