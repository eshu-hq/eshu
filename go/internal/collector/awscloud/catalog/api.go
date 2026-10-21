// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

const (
	// ServiceAPIGateway identifies the regional Amazon API Gateway metadata scan
	// slice covering REST, HTTP, and WebSocket APIs.
	ServiceAPIGateway = "apigateway"
)

const (
	// ResourceTypeAPIGatewayRESTAPI identifies a REST API Gateway API.
	ResourceTypeAPIGatewayRESTAPI = "aws_apigateway_rest_api"
	// ResourceTypeAPIGatewayV2API identifies an HTTP or WebSocket API Gateway
	// API.
	ResourceTypeAPIGatewayV2API = "aws_apigatewayv2_api"
	// ResourceTypeAPIGatewayStage identifies an API Gateway stage.
	ResourceTypeAPIGatewayStage = "aws_apigateway_stage"
	// ResourceTypeAPIGatewayDomainName identifies an API Gateway custom domain.
	ResourceTypeAPIGatewayDomainName = "aws_apigateway_domain_name"
)

const (
	// RelationshipAPIGatewayAPIHasStage records stage membership on an API
	// Gateway API.
	RelationshipAPIGatewayAPIHasStage = "apigateway_api_has_stage"
	// RelationshipAPIGatewayDomainMapsToAPI records custom-domain mapping
	// evidence to an API Gateway API.
	RelationshipAPIGatewayDomainMapsToAPI = "apigateway_domain_maps_to_api"
	// RelationshipAPIGatewayDomainUsesACMCertificate records custom-domain TLS
	// certificate dependencies.
	RelationshipAPIGatewayDomainUsesACMCertificate = "apigateway_domain_uses_acm_certificate"
	// RelationshipAPIGatewayStageLogsToResource records stage access-log
	// destinations when AWS reports an ARN.
	RelationshipAPIGatewayStageLogsToResource = "apigateway_stage_logs_to_resource"
	// RelationshipAPIGatewayAPIIntegratesWithResource records API integration
	// targets when AWS reports an ARN-addressable backend.
	RelationshipAPIGatewayAPIIntegratesWithResource = "apigateway_api_integrates_with_resource"
)

const (
	// ServiceAPIGatewayV2 identifies the regional Amazon API Gateway v2 metadata
	// scan slice covering HTTP and WebSocket APIs, their stages, routes,
	// integrations, authorizers, custom domains, and VPC links. The classic REST
	// (v1) surface is owned by the separate ServiceAPIGateway scan slice.
	ServiceAPIGatewayV2 = "apigatewayv2"
)

const (
	// ResourceTypeAPIGatewayV2Route identifies an API Gateway v2 route. The fact
	// carries the route key, route id, target reference, and authorization
	// summary only. Request models, request parameter mappings, and selection
	// expressions are never persisted because they describe request transformation
	// behavior, not topology.
	ResourceTypeAPIGatewayV2Route = "aws_apigatewayv2_route"
	// ResourceTypeAPIGatewayV2Integration identifies an API Gateway v2 integration.
	// The fact carries the integration id, type, backend URI/target, and
	// connection metadata only. Request templates, request/response parameter
	// mappings, and credential ARNs are never persisted.
	ResourceTypeAPIGatewayV2Integration = "aws_apigatewayv2_integration"
	// ResourceTypeAPIGatewayV2Authorizer identifies an API Gateway v2 authorizer.
	// The fact carries the authorizer name, type, and identity sources only. The
	// JWT issuer is surfaced for the Cognito user pool join. Lambda authorizer
	// invocation URIs, authorizer credential ARNs, and identity validation
	// expressions are never persisted.
	ResourceTypeAPIGatewayV2Authorizer = "aws_apigatewayv2_authorizer"
	// ResourceTypeAPIGatewayV2VPCLink identifies an API Gateway v2 VPC link used by
	// private integrations. The fact carries the link id, name, status, subnet
	// ids, and security group ids.
	ResourceTypeAPIGatewayV2VPCLink = "aws_apigatewayv2_vpc_link"
)

const (
	// RelationshipAPIGatewayV2APIHasStage records stage membership on an API
	// Gateway v2 API.
	RelationshipAPIGatewayV2APIHasStage = "apigatewayv2_api_has_stage"
	// RelationshipAPIGatewayV2APIHasRoute records route membership on an API
	// Gateway v2 API.
	RelationshipAPIGatewayV2APIHasRoute = "apigatewayv2_api_has_route"
	// RelationshipAPIGatewayV2RouteUsesIntegration records the integration a route
	// dispatches to, derived from the route target reference.
	RelationshipAPIGatewayV2RouteUsesIntegration = "apigatewayv2_route_uses_integration"
	// RelationshipAPIGatewayV2IntegrationTargetsLambda records the Lambda function
	// an AWS_PROXY integration invokes, joined by the function ARN the Lambda
	// scanner publishes as its resource_id.
	RelationshipAPIGatewayV2IntegrationTargetsLambda = "apigatewayv2_integration_targets_lambda"
	// RelationshipAPIGatewayV2IntegrationTargetsHTTP records the external HTTP
	// endpoint an HTTP_PROXY integration forwards to. The target is a URL, not an
	// AWS resource ARN.
	RelationshipAPIGatewayV2IntegrationTargetsHTTP = "apigatewayv2_integration_targets_http"
	// RelationshipAPIGatewayV2IntegrationUsesVPCLink records the VPC link a private
	// integration routes through.
	RelationshipAPIGatewayV2IntegrationUsesVPCLink = "apigatewayv2_integration_uses_vpc_link"
	// RelationshipAPIGatewayV2APIUsesUserPool records the Cognito user pool a JWT
	// authorizer trusts, joined by the bare user pool id parsed from the JWT
	// issuer URL. This matches the resource_id the Cognito scanner publishes for
	// the user pool node.
	RelationshipAPIGatewayV2APIUsesUserPool = "apigatewayv2_api_uses_user_pool"
	// RelationshipAPIGatewayV2APIUsesJWTIssuer records a JWT authorizer issuer that
	// is not an AWS Cognito user pool. The issuer is an external URL, not an AWS
	// resource ARN.
	RelationshipAPIGatewayV2APIUsesJWTIssuer = "apigatewayv2_api_uses_jwt_issuer"
	// RelationshipAPIGatewayV2DomainUsesACMCertificate records custom-domain TLS
	// certificate dependencies, joined by the certificate ARN the ACM scanner
	// publishes as its resource_id.
	RelationshipAPIGatewayV2DomainUsesACMCertificate = "apigatewayv2_domain_uses_acm_certificate"
	// RelationshipAPIGatewayV2DomainMapsToAPI records custom-domain API mapping
	// evidence to an API Gateway v2 API.
	RelationshipAPIGatewayV2DomainMapsToAPI = "apigatewayv2_domain_maps_to_api"
	// RelationshipAPIGatewayV2VPCLinkUsesSubnet records a subnet an API Gateway v2
	// VPC link spans, joined by the bare subnet id the EC2 scanner publishes.
	RelationshipAPIGatewayV2VPCLinkUsesSubnet = "apigatewayv2_vpc_link_uses_subnet"
	// RelationshipAPIGatewayV2VPCLinkUsesSecurityGroup records a security group an
	// API Gateway v2 VPC link applies, joined by the bare group id the EC2 scanner
	// publishes.
	RelationshipAPIGatewayV2VPCLinkUsesSecurityGroup = "apigatewayv2_vpc_link_uses_security_group"
)

const (
	// APIGatewayV2JWTIssuerTargetType labels a JWT authorizer issuer that is not a
	// Cognito user pool. The issuer is an external OpenID Connect URL, not an AWS
	// resource ARN.
	APIGatewayV2JWTIssuerTargetType = "jwt_issuer"
	// APIGatewayV2HTTPEndpointTargetType labels an HTTP_PROXY integration target.
	// The target is an external endpoint URL, not an AWS resource ARN.
	APIGatewayV2HTTPEndpointTargetType = "http_endpoint"
)

const (
	// ServiceAppSync identifies the regional AWS AppSync metadata scan slice
	// covering GraphQL APIs, data sources, resolvers, functions, schema
	// metadata, and API key metadata.
	ServiceAppSync = "appsync"
)

const (
	// ResourceTypeAppSyncGraphQLAPI identifies an AppSync GraphQL API.
	ResourceTypeAppSyncGraphQLAPI = "aws_appsync_graphql_api"
	// ResourceTypeAppSyncDataSource identifies an AppSync data source.
	ResourceTypeAppSyncDataSource = "aws_appsync_datasource"
	// ResourceTypeAppSyncResolver identifies an AppSync resolver attached to a
	// GraphQL type field.
	ResourceTypeAppSyncResolver = "aws_appsync_resolver"
	// ResourceTypeAppSyncFunction identifies an AppSync pipeline function.
	ResourceTypeAppSyncFunction = "aws_appsync_function"
	// ResourceTypeAppSyncSchema identifies AppSync schema metadata for one API.
	ResourceTypeAppSyncSchema = "aws_appsync_schema"
	// Per-key AppSync API key resources are intentionally absent: key IDs
	// must not become graph identities. The scanner emits only key counts
	// and lifecycle timestamps on the owning API; historical
	// aws_appsync_api_key rows are matched by literal in read paths.
)

const (
	// RelationshipAppSyncAPIHasDataSource records data-source membership on an
	// AppSync GraphQL API.
	RelationshipAppSyncAPIHasDataSource = "appsync_api_has_data_source"
	// RelationshipAppSyncResolverUsesDataSource records the data source a
	// resolver invokes.
	RelationshipAppSyncResolverUsesDataSource = "appsync_resolver_uses_data_source"
	// RelationshipAppSyncFunctionUsesDataSource records the data source a
	// pipeline function invokes.
	RelationshipAppSyncFunctionUsesDataSource = "appsync_function_uses_data_source"
	// RelationshipAppSyncDataSourceTargetsResource records the backing AWS
	// resource a data source connects to (Lambda, DynamoDB table, OpenSearch
	// domain, HTTP endpoint, or RDS cluster).
	RelationshipAppSyncDataSourceTargetsResource = "appsync_data_source_targets_resource"
	// RelationshipAppSyncAPIUsesUserPool records the Cognito user pool a GraphQL
	// API authenticates against.
	RelationshipAppSyncAPIUsesUserPool = "appsync_api_uses_user_pool"
	// RelationshipAppSyncAPIUsesOIDCIssuer records the OpenID Connect issuer a
	// GraphQL API authenticates against.
	RelationshipAppSyncAPIUsesOIDCIssuer = "appsync_api_uses_oidc_issuer"
)

const (
	// AppSyncDataSourceTargetTypeOpenSearch labels an AppSync OpenSearch data
	// source target. AppSync reports an endpoint URL rather than a domain ARN,
	// and no OpenSearch scanner publishes a canonical node, so the edge carries
	// the endpoint as join evidence under this target type.
	AppSyncDataSourceTargetTypeOpenSearch = "aws_opensearch_domain"
	// AppSyncDataSourceTargetTypeHTTPEndpoint labels an AppSync HTTP data source
	// target. The target is an external endpoint URL, not an AWS resource ARN.
	AppSyncDataSourceTargetTypeHTTPEndpoint = "http_endpoint"
	// AppSyncOIDCIssuerTargetType labels the OpenID Connect issuer an AppSync API
	// trusts. The issuer is an external URL, not an AWS resource ARN.
	AppSyncOIDCIssuerTargetType = "oidc_issuer"
)

const (
	// ServiceAppConfig identifies the regional AWS AppConfig metadata-only scan
	// slice. The scanner reads AppConfig control-plane identity and deployment
	// metadata through ListApplications, ListEnvironments,
	// ListConfigurationProfiles, ListDeploymentStrategies, and
	// ListTagsForResource. It never reads or persists configuration content,
	// hosted configuration version bodies, or freeform/feature-flag values, and
	// never mutates AppConfig state or starts deployments.
	ServiceAppConfig = "appconfig"
)

const (
	// ResourceTypeAppConfigApplication identifies an AWS AppConfig application
	// metadata resource. The scanner emits identity (id, synthesized ARN, name)
	// and description only; it never reads any configuration the application
	// distributes.
	ResourceTypeAppConfigApplication = "aws_appconfig_application"
	// ResourceTypeAppConfigEnvironment identifies an AWS AppConfig environment
	// metadata resource. The scanner emits identity, owning application id,
	// lifecycle state, and the CloudWatch alarm monitor references only.
	ResourceTypeAppConfigEnvironment = "aws_appconfig_environment"
	// ResourceTypeAppConfigConfigurationProfile identifies an AWS AppConfig
	// configuration profile metadata resource. The scanner emits identity,
	// owning application id, profile type, validator-type kinds, and the
	// configuration source location URI reference only; it never reads the
	// configuration content the profile points at.
	ResourceTypeAppConfigConfigurationProfile = "aws_appconfig_configuration_profile"
	// ResourceTypeAppConfigDeploymentStrategy identifies an AWS AppConfig
	// deployment strategy metadata resource. The scanner emits identity and the
	// rollout-shape knobs (duration, growth factor/type, final bake time,
	// replicate-to target) only.
	ResourceTypeAppConfigDeploymentStrategy = "aws_appconfig_deployment_strategy"
)

const (
	// RelationshipAppConfigEnvironmentInApplication records an AppConfig
	// environment's membership in its owning application. The target is keyed by
	// the application ARN the application node publishes so the edge joins the
	// application node.
	RelationshipAppConfigEnvironmentInApplication = "appconfig_environment_in_application"
	// RelationshipAppConfigProfileInApplication records an AppConfig
	// configuration profile's membership in its owning application. The target is
	// keyed by the application ARN the application node publishes.
	RelationshipAppConfigProfileInApplication = "appconfig_profile_in_application"
	// RelationshipAppConfigEnvironmentMonitorsAlarm records an AppConfig
	// environment's CloudWatch alarm monitor. AppConfig reports the alarm ARN,
	// which matches how the CloudWatch scanner publishes its alarm resource_id,
	// so the edge joins the alarm node.
	RelationshipAppConfigEnvironmentMonitorsAlarm = "appconfig_environment_monitors_alarm"
	// RelationshipAppConfigEnvironmentUsesMonitorRole records the IAM role
	// AppConfig assumes to read a monitored CloudWatch alarm. The target is keyed
	// by the role ARN the IAM scanner publishes.
	RelationshipAppConfigEnvironmentUsesMonitorRole = "appconfig_environment_uses_monitor_role"
)

const (
	// ServiceAppRunner identifies the regional AWS App Runner service scan
	// slice.
	ServiceAppRunner = "apprunner"
)

const (
	// ResourceTypeAppRunnerService identifies an App Runner service. The
	// resource_id is the service ARN, matching the in-use-by / associated-
	// resource ARN that the ACM and WAFv2 scanners emit as their App Runner
	// edge target so those dangling edges resolve to this resource.
	ResourceTypeAppRunnerService = "aws_apprunner_service"
	// ResourceTypeAppRunnerConnection identifies an App Runner connection used
	// to reach a source code repository provider (for example GitHub or
	// Bitbucket). The resource_id is the connection ARN.
	ResourceTypeAppRunnerConnection = "aws_apprunner_connection"
	// ResourceTypeAppRunnerAutoScalingConfiguration identifies an App Runner
	// automatic scaling configuration revision. The resource_id is the
	// configuration ARN.
	ResourceTypeAppRunnerAutoScalingConfiguration = "aws_apprunner_autoscaling_configuration"
	// ResourceTypeAppRunnerObservabilityConfiguration identifies an App Runner
	// observability configuration revision. The resource_id is the
	// configuration ARN.
	ResourceTypeAppRunnerObservabilityConfiguration = "aws_apprunner_observability_configuration"
	// ResourceTypeAppRunnerVpcConnector identifies an App Runner VPC connector.
	// The resource_id is the VPC connector ARN.
	ResourceTypeAppRunnerVpcConnector = "aws_apprunner_vpc_connector"
	// ResourceTypeAppRunnerVpcIngressConnection identifies an App Runner VPC
	// ingress connection. The resource_id is the VPC ingress connection ARN.
	ResourceTypeAppRunnerVpcIngressConnection = "aws_apprunner_vpc_ingress_connection"
)

const (
	// RelationshipAppRunnerServiceUsesImage records a container image URI a
	// service deploys from a source image repository. The target keys on the
	// container image identifier (container_image), matching the ECS and Batch
	// container-image join.
	RelationshipAppRunnerServiceUsesImage = "apprunner_service_uses_image"
	// RelationshipAppRunnerServiceUsesConnection records the App Runner
	// connection a source-code service uses to reach its repository provider.
	RelationshipAppRunnerServiceUsesConnection = "apprunner_service_uses_connection"
	// RelationshipAppRunnerServiceUsesIAMRole records the instance role or the
	// ECR access role a service uses. The target keys on the IAM role ARN.
	RelationshipAppRunnerServiceUsesIAMRole = "apprunner_service_uses_iam_role"
	// RelationshipAppRunnerServiceUsesKMSKey records the customer-managed KMS
	// key a service uses to encrypt logs and the source-repository copy.
	RelationshipAppRunnerServiceUsesKMSKey = "apprunner_service_uses_kms_key"
	// RelationshipAppRunnerServiceUsesVpcConnector records the VPC connector a
	// service uses for outbound (egress) VPC traffic.
	RelationshipAppRunnerServiceUsesVpcConnector = "apprunner_service_uses_vpc_connector"
	// RelationshipAppRunnerServiceUsesAutoScalingConfiguration records the
	// automatic scaling configuration revision associated with a service.
	RelationshipAppRunnerServiceUsesAutoScalingConfiguration = "apprunner_service_uses_autoscaling_configuration"
	// RelationshipAppRunnerServiceUsesObservabilityConfiguration records the
	// observability configuration revision associated with a service.
	RelationshipAppRunnerServiceUsesObservabilityConfiguration = "apprunner_service_uses_observability_configuration"
	// RelationshipAppRunnerServiceReferencesSecret records a Secrets Manager or
	// SSM Parameter Store ARN referenced by a service's runtime environment
	// secrets. Only the reference ARN is recorded; the resolved value is never
	// read or persisted.
	RelationshipAppRunnerServiceReferencesSecret = "apprunner_service_references_secret" // #nosec G101 -- relationship-type identifier recording a secret ARN reference, not a credential value
	// RelationshipAppRunnerVpcConnectorUsesSubnet records a subnet a VPC
	// connector places network interfaces in. The target keys on the bare
	// subnet ID.
	RelationshipAppRunnerVpcConnectorUsesSubnet = "apprunner_vpc_connector_uses_subnet"
	// RelationshipAppRunnerVpcConnectorUsesSecurityGroup records a security
	// group a VPC connector attaches. The target keys on the bare security
	// group ID.
	RelationshipAppRunnerVpcConnectorUsesSecurityGroup = "apprunner_vpc_connector_uses_security_group"
	// RelationshipAppRunnerVpcIngressConnectionTargetsService records the
	// service a VPC ingress connection routes inbound traffic to.
	RelationshipAppRunnerVpcIngressConnectionTargetsService = "apprunner_vpc_ingress_connection_targets_service"
)
