// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

// TestAPIConstantsMatchAWSCloudValues pins every API-category constant
// to the exact value it carried in package awscloud before the #6696 catalog
// consolidation. The consolidation moves declarations only: any value drift
// here is a behavior change, not a move.
func TestAPIConstantsMatchAWSCloudValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "ServiceAPIGateway", got: ServiceAPIGateway, want: "apigateway"},
		{name: "ResourceTypeAPIGatewayRESTAPI", got: ResourceTypeAPIGatewayRESTAPI, want: "aws_apigateway_rest_api"},
		{name: "ResourceTypeAPIGatewayV2API", got: ResourceTypeAPIGatewayV2API, want: "aws_apigatewayv2_api"},
		{name: "ResourceTypeAPIGatewayStage", got: ResourceTypeAPIGatewayStage, want: "aws_apigateway_stage"},
		{name: "ResourceTypeAPIGatewayDomainName", got: ResourceTypeAPIGatewayDomainName, want: "aws_apigateway_domain_name"},
		{name: "RelationshipAPIGatewayAPIHasStage", got: RelationshipAPIGatewayAPIHasStage, want: "apigateway_api_has_stage"},
		{name: "RelationshipAPIGatewayDomainMapsToAPI", got: RelationshipAPIGatewayDomainMapsToAPI, want: "apigateway_domain_maps_to_api"},
		{name: "RelationshipAPIGatewayDomainUsesACMCertificate", got: RelationshipAPIGatewayDomainUsesACMCertificate, want: "apigateway_domain_uses_acm_certificate"},
		{name: "RelationshipAPIGatewayStageLogsToResource", got: RelationshipAPIGatewayStageLogsToResource, want: "apigateway_stage_logs_to_resource"},
		{name: "RelationshipAPIGatewayAPIIntegratesWithResource", got: RelationshipAPIGatewayAPIIntegratesWithResource, want: "apigateway_api_integrates_with_resource"},
		{name: "ServiceAPIGatewayV2", got: ServiceAPIGatewayV2, want: "apigatewayv2"},
		{name: "ResourceTypeAPIGatewayV2Route", got: ResourceTypeAPIGatewayV2Route, want: "aws_apigatewayv2_route"},
		{name: "ResourceTypeAPIGatewayV2Integration", got: ResourceTypeAPIGatewayV2Integration, want: "aws_apigatewayv2_integration"},
		{name: "ResourceTypeAPIGatewayV2Authorizer", got: ResourceTypeAPIGatewayV2Authorizer, want: "aws_apigatewayv2_authorizer"},
		{name: "ResourceTypeAPIGatewayV2VPCLink", got: ResourceTypeAPIGatewayV2VPCLink, want: "aws_apigatewayv2_vpc_link"},
		{name: "RelationshipAPIGatewayV2APIHasStage", got: RelationshipAPIGatewayV2APIHasStage, want: "apigatewayv2_api_has_stage"},
		{name: "RelationshipAPIGatewayV2APIHasRoute", got: RelationshipAPIGatewayV2APIHasRoute, want: "apigatewayv2_api_has_route"},
		{name: "RelationshipAPIGatewayV2RouteUsesIntegration", got: RelationshipAPIGatewayV2RouteUsesIntegration, want: "apigatewayv2_route_uses_integration"},
		{name: "RelationshipAPIGatewayV2IntegrationTargetsLambda", got: RelationshipAPIGatewayV2IntegrationTargetsLambda, want: "apigatewayv2_integration_targets_lambda"},
		{name: "RelationshipAPIGatewayV2IntegrationTargetsHTTP", got: RelationshipAPIGatewayV2IntegrationTargetsHTTP, want: "apigatewayv2_integration_targets_http"},
		{name: "RelationshipAPIGatewayV2IntegrationUsesVPCLink", got: RelationshipAPIGatewayV2IntegrationUsesVPCLink, want: "apigatewayv2_integration_uses_vpc_link"},
		{name: "RelationshipAPIGatewayV2APIUsesUserPool", got: RelationshipAPIGatewayV2APIUsesUserPool, want: "apigatewayv2_api_uses_user_pool"},
		{name: "RelationshipAPIGatewayV2APIUsesJWTIssuer", got: RelationshipAPIGatewayV2APIUsesJWTIssuer, want: "apigatewayv2_api_uses_jwt_issuer"},
		{name: "RelationshipAPIGatewayV2DomainUsesACMCertificate", got: RelationshipAPIGatewayV2DomainUsesACMCertificate, want: "apigatewayv2_domain_uses_acm_certificate"},
		{name: "RelationshipAPIGatewayV2DomainMapsToAPI", got: RelationshipAPIGatewayV2DomainMapsToAPI, want: "apigatewayv2_domain_maps_to_api"},
		{name: "RelationshipAPIGatewayV2VPCLinkUsesSubnet", got: RelationshipAPIGatewayV2VPCLinkUsesSubnet, want: "apigatewayv2_vpc_link_uses_subnet"},
		{name: "RelationshipAPIGatewayV2VPCLinkUsesSecurityGroup", got: RelationshipAPIGatewayV2VPCLinkUsesSecurityGroup, want: "apigatewayv2_vpc_link_uses_security_group"},
		{name: "APIGatewayV2JWTIssuerTargetType", got: APIGatewayV2JWTIssuerTargetType, want: "jwt_issuer"},
		{name: "APIGatewayV2HTTPEndpointTargetType", got: APIGatewayV2HTTPEndpointTargetType, want: "http_endpoint"},
		{name: "ServiceAppSync", got: ServiceAppSync, want: "appsync"},
		{name: "ResourceTypeAppSyncGraphQLAPI", got: ResourceTypeAppSyncGraphQLAPI, want: "aws_appsync_graphql_api"},
		{name: "ResourceTypeAppSyncDataSource", got: ResourceTypeAppSyncDataSource, want: "aws_appsync_datasource"},
		{name: "ResourceTypeAppSyncResolver", got: ResourceTypeAppSyncResolver, want: "aws_appsync_resolver"},
		{name: "ResourceTypeAppSyncFunction", got: ResourceTypeAppSyncFunction, want: "aws_appsync_function"},
		{name: "ResourceTypeAppSyncSchema", got: ResourceTypeAppSyncSchema, want: "aws_appsync_schema"},
		{name: "RelationshipAppSyncAPIHasDataSource", got: RelationshipAppSyncAPIHasDataSource, want: "appsync_api_has_data_source"},
		{name: "RelationshipAppSyncResolverUsesDataSource", got: RelationshipAppSyncResolverUsesDataSource, want: "appsync_resolver_uses_data_source"},
		{name: "RelationshipAppSyncFunctionUsesDataSource", got: RelationshipAppSyncFunctionUsesDataSource, want: "appsync_function_uses_data_source"},
		{name: "RelationshipAppSyncDataSourceTargetsResource", got: RelationshipAppSyncDataSourceTargetsResource, want: "appsync_data_source_targets_resource"},
		{name: "RelationshipAppSyncAPIUsesUserPool", got: RelationshipAppSyncAPIUsesUserPool, want: "appsync_api_uses_user_pool"},
		{name: "RelationshipAppSyncAPIUsesOIDCIssuer", got: RelationshipAppSyncAPIUsesOIDCIssuer, want: "appsync_api_uses_oidc_issuer"},
		{name: "AppSyncDataSourceTargetTypeOpenSearch", got: AppSyncDataSourceTargetTypeOpenSearch, want: "aws_opensearch_domain"},
		{name: "AppSyncDataSourceTargetTypeHTTPEndpoint", got: AppSyncDataSourceTargetTypeHTTPEndpoint, want: "http_endpoint"},
		{name: "AppSyncOIDCIssuerTargetType", got: AppSyncOIDCIssuerTargetType, want: "oidc_issuer"},
		{name: "ServiceAppConfig", got: ServiceAppConfig, want: "appconfig"},
		{name: "ResourceTypeAppConfigApplication", got: ResourceTypeAppConfigApplication, want: "aws_appconfig_application"},
		{name: "ResourceTypeAppConfigEnvironment", got: ResourceTypeAppConfigEnvironment, want: "aws_appconfig_environment"},
		{name: "ResourceTypeAppConfigConfigurationProfile", got: ResourceTypeAppConfigConfigurationProfile, want: "aws_appconfig_configuration_profile"},
		{name: "ResourceTypeAppConfigDeploymentStrategy", got: ResourceTypeAppConfigDeploymentStrategy, want: "aws_appconfig_deployment_strategy"},
		{name: "RelationshipAppConfigEnvironmentInApplication", got: RelationshipAppConfigEnvironmentInApplication, want: "appconfig_environment_in_application"},
		{name: "RelationshipAppConfigProfileInApplication", got: RelationshipAppConfigProfileInApplication, want: "appconfig_profile_in_application"},
		{name: "RelationshipAppConfigEnvironmentMonitorsAlarm", got: RelationshipAppConfigEnvironmentMonitorsAlarm, want: "appconfig_environment_monitors_alarm"},
		{name: "RelationshipAppConfigEnvironmentUsesMonitorRole", got: RelationshipAppConfigEnvironmentUsesMonitorRole, want: "appconfig_environment_uses_monitor_role"},
		{name: "ServiceAppRunner", got: ServiceAppRunner, want: "apprunner"},
		{name: "ResourceTypeAppRunnerService", got: ResourceTypeAppRunnerService, want: "aws_apprunner_service"},
		{name: "ResourceTypeAppRunnerConnection", got: ResourceTypeAppRunnerConnection, want: "aws_apprunner_connection"},
		{name: "ResourceTypeAppRunnerAutoScalingConfiguration", got: ResourceTypeAppRunnerAutoScalingConfiguration, want: "aws_apprunner_autoscaling_configuration"},
		{name: "ResourceTypeAppRunnerObservabilityConfiguration", got: ResourceTypeAppRunnerObservabilityConfiguration, want: "aws_apprunner_observability_configuration"},
		{name: "ResourceTypeAppRunnerVpcConnector", got: ResourceTypeAppRunnerVpcConnector, want: "aws_apprunner_vpc_connector"},
		{name: "ResourceTypeAppRunnerVpcIngressConnection", got: ResourceTypeAppRunnerVpcIngressConnection, want: "aws_apprunner_vpc_ingress_connection"},
		{name: "RelationshipAppRunnerServiceUsesImage", got: RelationshipAppRunnerServiceUsesImage, want: "apprunner_service_uses_image"},
		{name: "RelationshipAppRunnerServiceUsesConnection", got: RelationshipAppRunnerServiceUsesConnection, want: "apprunner_service_uses_connection"},
		{name: "RelationshipAppRunnerServiceUsesIAMRole", got: RelationshipAppRunnerServiceUsesIAMRole, want: "apprunner_service_uses_iam_role"},
		{name: "RelationshipAppRunnerServiceUsesKMSKey", got: RelationshipAppRunnerServiceUsesKMSKey, want: "apprunner_service_uses_kms_key"},
		{name: "RelationshipAppRunnerServiceUsesVpcConnector", got: RelationshipAppRunnerServiceUsesVpcConnector, want: "apprunner_service_uses_vpc_connector"},
		{name: "RelationshipAppRunnerServiceUsesAutoScalingConfiguration", got: RelationshipAppRunnerServiceUsesAutoScalingConfiguration, want: "apprunner_service_uses_autoscaling_configuration"},
		{name: "RelationshipAppRunnerServiceUsesObservabilityConfiguration", got: RelationshipAppRunnerServiceUsesObservabilityConfiguration, want: "apprunner_service_uses_observability_configuration"},
		{name: "RelationshipAppRunnerServiceReferencesSecret", got: RelationshipAppRunnerServiceReferencesSecret, want: "apprunner_service_references_secret"},
		{name: "RelationshipAppRunnerVpcConnectorUsesSubnet", got: RelationshipAppRunnerVpcConnectorUsesSubnet, want: "apprunner_vpc_connector_uses_subnet"},
		{name: "RelationshipAppRunnerVpcConnectorUsesSecurityGroup", got: RelationshipAppRunnerVpcConnectorUsesSecurityGroup, want: "apprunner_vpc_connector_uses_security_group"},
		{name: "RelationshipAppRunnerVpcIngressConnectionTargetsService", got: RelationshipAppRunnerVpcIngressConnectionTargetsService, want: "apprunner_vpc_ingress_connection_targets_service"},
		{name: "ServiceSynthetics", got: ServiceSynthetics, want: "synthetics"},
		{name: "ResourceTypeSyntheticsCanary", got: ResourceTypeSyntheticsCanary, want: "aws_synthetics_canary"},
		{name: "RelationshipSyntheticsCanaryUsesS3Bucket", got: RelationshipSyntheticsCanaryUsesS3Bucket, want: "synthetics_canary_uses_s3_bucket"},
		{name: "RelationshipSyntheticsCanaryUsesIAMRole", got: RelationshipSyntheticsCanaryUsesIAMRole, want: "synthetics_canary_uses_iam_role"},
		{name: "RelationshipSyntheticsCanaryUsesSubnet", got: RelationshipSyntheticsCanaryUsesSubnet, want: "synthetics_canary_uses_subnet"},
		{name: "RelationshipSyntheticsCanaryUsesSecurityGroup", got: RelationshipSyntheticsCanaryUsesSecurityGroup, want: "synthetics_canary_uses_security_group"},
		{name: "ServiceServiceCatalog", got: ServiceServiceCatalog, want: "servicecatalog"},
		{name: "ResourceTypeServiceCatalogPortfolio", got: ResourceTypeServiceCatalogPortfolio, want: "aws_servicecatalog_portfolio"},
		{name: "ResourceTypeServiceCatalogProduct", got: ResourceTypeServiceCatalogProduct, want: "aws_servicecatalog_product"},
		{name: "ResourceTypeServiceCatalogProvisionedProduct", got: ResourceTypeServiceCatalogProvisionedProduct, want: "aws_servicecatalog_provisioned_product"},
		{name: "RelationshipServiceCatalogProvisionedProductDeploysCloudFormationStack", got: RelationshipServiceCatalogProvisionedProductDeploysCloudFormationStack, want: "servicecatalog_provisioned_product_deploys_cloudformation_stack"},
		{name: "RelationshipServiceCatalogProductInPortfolio", got: RelationshipServiceCatalogProductInPortfolio, want: "servicecatalog_product_in_portfolio"},
		{name: "RelationshipServiceCatalogPortfolioGrantsPrincipal", got: RelationshipServiceCatalogPortfolioGrantsPrincipal, want: "servicecatalog_portfolio_grants_principal"},
		{name: "ServiceProton", got: ServiceProton, want: "proton"},
		{name: "ResourceTypeProtonEnvironment", got: ResourceTypeProtonEnvironment, want: "aws_proton_environment"},
		{name: "ResourceTypeProtonService", got: ResourceTypeProtonService, want: "aws_proton_service"},
		{name: "ResourceTypeProtonEnvironmentTemplate", got: ResourceTypeProtonEnvironmentTemplate, want: "aws_proton_environment_template"},
		{name: "ResourceTypeProtonServiceTemplate", got: ResourceTypeProtonServiceTemplate, want: "aws_proton_service_template"},
		{name: "RelationshipProtonServiceInEnvironment", got: RelationshipProtonServiceInEnvironment, want: "proton_service_in_environment"},
		{name: "RelationshipProtonEnvironmentUsesRole", got: RelationshipProtonEnvironmentUsesRole, want: "proton_environment_uses_role"},
		{name: "ServiceResilienceHub", got: ServiceResilienceHub, want: "resiliencehub"},
		{name: "WarningResilienceHubAppVersionMissing", got: WarningResilienceHubAppVersionMissing, want: "resiliencehub_app_version_missing"},
		{name: "ResourceTypeResilienceHubApp", got: ResourceTypeResilienceHubApp, want: "aws_resiliencehub_app"},
		{name: "ResourceTypeResilienceHubResiliencyPolicy", got: ResourceTypeResilienceHubResiliencyPolicy, want: "aws_resiliencehub_resiliency_policy"},
		{name: "ResourceTypeResilienceHubAppComponent", got: ResourceTypeResilienceHubAppComponent, want: "aws_resiliencehub_app_component"},
		{name: "ResourceTypeResilienceHubAppInputSource", got: ResourceTypeResilienceHubAppInputSource, want: "aws_resiliencehub_app_input_source"},
		{name: "ResourceTypeResilienceHubAppAssessment", got: ResourceTypeResilienceHubAppAssessment, want: "aws_resiliencehub_app_assessment"},
		{name: "RelationshipResilienceHubAppUsesPolicy", got: RelationshipResilienceHubAppUsesPolicy, want: "resiliencehub_app_uses_policy"},
		{name: "RelationshipResilienceHubAppProtectsResource", got: RelationshipResilienceHubAppProtectsResource, want: "resiliencehub_app_protects_resource"},
		{name: "RelationshipResilienceHubComponentInApp", got: RelationshipResilienceHubComponentInApp, want: "resiliencehub_component_in_app"},
		{name: "RelationshipResilienceHubInputSourceInApp", got: RelationshipResilienceHubInputSourceInApp, want: "resiliencehub_input_source_in_app"},
		{name: "RelationshipResilienceHubAssessmentForApp", got: RelationshipResilienceHubAssessmentForApp, want: "resiliencehub_assessment_for_app"},
		{name: "ServiceFIS", got: ServiceFIS, want: "fis"},
		{name: "ResourceTypeFISExperimentTemplate", got: ResourceTypeFISExperimentTemplate, want: "aws_fis_experiment_template"},
		{name: "RelationshipFISTemplateUsesIAMRole", got: RelationshipFISTemplateUsesIAMRole, want: "fis_template_uses_iam_role"},
		{name: "RelationshipFISTemplateTargetsEC2Instance", got: RelationshipFISTemplateTargetsEC2Instance, want: "fis_template_targets_ec2_instance"},
		{name: "RelationshipFISTemplateTargetsECSCluster", got: RelationshipFISTemplateTargetsECSCluster, want: "fis_template_targets_ecs_cluster"},
		{name: "RelationshipFISTemplateTargetsRDSDBInstance", got: RelationshipFISTemplateTargetsRDSDBInstance, want: "fis_template_targets_rds_db_instance"},
		{name: "RelationshipFISTemplateTargetsRDSDBCluster", got: RelationshipFISTemplateTargetsRDSDBCluster, want: "fis_template_targets_rds_db_cluster"},
		{name: "RelationshipFISTemplateLogsToCloudWatchLogGroup", got: RelationshipFISTemplateLogsToCloudWatchLogGroup, want: "fis_template_logs_to_cloudwatch_log_group"},
		{name: "RelationshipFISTemplateLogsToS3", got: RelationshipFISTemplateLogsToS3, want: "fis_template_logs_to_s3"},
		{name: "RelationshipFISTemplateStopsOnCloudWatchAlarm", got: RelationshipFISTemplateStopsOnCloudWatchAlarm, want: "fis_template_stops_on_cloudwatch_alarm"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}
