package terraformschema

import "testing"

func TestClassifyResourceCategory(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		resourceType string
		want         string
	}{
		{name: "compute lambda", resourceType: "aws_lambda_function", want: "compute"},
		{name: "compute ecs", resourceType: "aws_ecs_service", want: "compute"},
		{name: "compute appautoscaling", resourceType: "aws_appautoscaling_target", want: "compute"},
		{name: "compute launch template", resourceType: "aws_launch_template", want: "compute"},
		{name: "storage s3", resourceType: "aws_s3_bucket", want: "storage"},
		{name: "storage ebs", resourceType: "aws_ebs_volume", want: "storage"},
		{name: "storage datasync", resourceType: "aws_datasync_location_s3", want: "storage"},
		{name: "storage transfer", resourceType: "aws_transfer_server", want: "storage"},
		{name: "data rds", resourceType: "aws_rds_cluster", want: "data"},
		{name: "data dms", resourceType: "aws_dms_endpoint", want: "data"},
		{name: "data docdb", resourceType: "aws_docdb_cluster", want: "data"},
		{name: "networking route53", resourceType: "aws_route53_record", want: "networking"},
		{name: "networking api gateway", resourceType: "aws_api_gateway_resource", want: "networking"},
		{name: "networking subnet plural", resourceType: "aws_subnets", want: "networking"},
		{name: "networking route table plural", resourceType: "aws_route_tables", want: "networking"},
		{name: "networking eip", resourceType: "aws_eip", want: "networking"},
		{name: "networking peering", resourceType: "aws_vpc_peering_connection_accepter", want: "networking"},
		{name: "networking gateway", resourceType: "aws_internet_gateway", want: "networking"},
		{name: "networking acl", resourceType: "aws_network_acl_rule", want: "networking"},
		{name: "messaging sqs", resourceType: "aws_sqs_queue", want: "messaging"},
		{name: "messaging ses", resourceType: "aws_ses_domain_identity", want: "messaging"},
		{name: "security iam", resourceType: "aws_iam_role", want: "security"},
		{name: "security ssoadmin", resourceType: "aws_ssoadmin_permission_set", want: "security"},
		{name: "cicd codebuild", resourceType: "aws_codebuild_project", want: "cicd"},
		{name: "monitoring cloudwatch", resourceType: "aws_cloudwatch_metric_alarm", want: "monitoring"},
		{name: "cloudwatch event prefers longest prefix", resourceType: "aws_cloudwatch_event_rule", want: "messaging"},
		{name: "governance cloudformation", resourceType: "aws_cloudformation_stack", want: "governance"},
		{name: "governance caller identity", resourceType: "aws_caller_identity", want: "governance"},
		{name: "unknown defaults infrastructure", resourceType: "aws_unknown_thing", want: "infrastructure"},
		{name: "security wafv2", resourceType: "aws_wafv2_web_acl", want: "security"},
		{name: "data neptune", resourceType: "aws_neptune_cluster", want: "data"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyResourceCategory(tc.resourceType); got != tc.want {
				t.Fatalf("ClassifyResourceCategory(%q) = %q, want %q", tc.resourceType, got, tc.want)
			}
		})
	}
}

func TestClassifyResourceService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resourceType string
		want         string
	}{
		{name: "iam policy document", resourceType: "aws_iam_policy_document", want: "iam"},
		{name: "cloudwatch event", resourceType: "aws_cloudwatch_event_rule", want: "cloudwatch_event"},
		{name: "api gateway", resourceType: "aws_api_gateway_resource", want: "api_gateway"},
		{name: "api gateway v2", resourceType: "aws_apigatewayv2_api", want: "apigatewayv2"},
		{name: "vpc peering", resourceType: "aws_vpc_peering_connection_accepter", want: "vpc_peering_connection"},
		{name: "caller identity", resourceType: "aws_caller_identity", want: "caller_identity"},
		{name: "unknown falls back to first service token", resourceType: "aws_custom_service_widget", want: "custom"},
		{name: "invalid", resourceType: "aws", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyResourceService(tc.resourceType); got != tc.want {
				t.Fatalf("ClassifyResourceService(%q) = %q, want %q", tc.resourceType, got, tc.want)
			}
		})
	}
}

func TestClassifyCommonAWSResourceTypesAvoidsGenericInfrastructure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		resourceType string
		wantService  string
		wantCategory string
	}{
		{resourceType: "aws_account_alternate_contact", wantService: "account", wantCategory: "governance"},
		{resourceType: "aws_ami", wantService: "ami", wantCategory: "compute"},
		{resourceType: "aws_appconfig_application", wantService: "appconfig", wantCategory: "governance"},
		{resourceType: "aws_athena_workgroup", wantService: "athena", wantCategory: "data"},
		{resourceType: "aws_availability_zones", wantService: "availability_zones", wantCategory: "governance"},
		{resourceType: "aws_bedrock_guardrail", wantService: "bedrock", wantCategory: "compute"},
		{resourceType: "aws_canonical_user_id", wantService: "canonical_user_id", wantCategory: "governance"},
		{resourceType: "aws_ce_cost_allocation_tag", wantService: "ce", wantCategory: "governance"},
		{resourceType: "aws_codestarnotifications_notification_rule", wantService: "codestarnotifications", wantCategory: "cicd"},
		{resourceType: "aws_default_network_acl", wantService: "default_network_acl", wantCategory: "networking"},
		{resourceType: "aws_default_route_table", wantService: "default_route_table", wantCategory: "networking"},
		{resourceType: "aws_default_security_group", wantService: "default_security_group", wantCategory: "networking"},
		{resourceType: "aws_default_vpc", wantService: "default_vpc", wantCategory: "networking"},
		{resourceType: "aws_detective_graph", wantService: "detective", wantCategory: "security"},
		{resourceType: "aws_dlm_lifecycle_policy", wantService: "dlm", wantCategory: "storage"},
		{resourceType: "aws_egress_only_internet_gateway", wantService: "egress_only_internet_gateway", wantCategory: "networking"},
		{resourceType: "aws_flow_log", wantService: "flow_log", wantCategory: "monitoring"},
		{resourceType: "aws_glue_catalog_database", wantService: "glue", wantCategory: "data"},
		{resourceType: "aws_identitystore_group", wantService: "identitystore", wantCategory: "security"},
		{resourceType: "aws_instances", wantService: "instances", wantCategory: "compute"},
		{resourceType: "aws_key_pair", wantService: "key_pair", wantCategory: "security"},
		{resourceType: "aws_lakeformation_permissions", wantService: "lakeformation", wantCategory: "data"},
		{resourceType: "aws_macie2_account", wantService: "macie2", wantCategory: "security"},
		{resourceType: "aws_network_acls", wantService: "network_acls", wantCategory: "networking"},
		{resourceType: "aws_osis_pipeline", wantService: "osis", wantCategory: "data"},
		{resourceType: "aws_outposts_outpost", wantService: "outposts", wantCategory: "compute"},
		{resourceType: "aws_partition", wantService: "partition", wantCategory: "governance"},
		{resourceType: "aws_quicksight_folder", wantService: "quicksight", wantCategory: "data"},
		{resourceType: "aws_redshiftserverless_namespace", wantService: "redshiftserverless", wantCategory: "data"},
		{resourceType: "aws_regions", wantService: "regions", wantCategory: "governance"},
		{resourceType: "aws_security_groups", wantService: "security_groups", wantCategory: "networking"},
		{resourceType: "aws_securityhub_account", wantService: "securityhub", wantCategory: "security"},
		{resourceType: "aws_vpcs", wantService: "vpcs", wantCategory: "networking"},
		{resourceType: "aws_volume_attachment", wantService: "volume", wantCategory: "storage"},
		{resourceType: "aws_wafregional_web_acl_association", wantService: "wafregional", wantCategory: "security"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.resourceType, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyResourceService(tc.resourceType); got != tc.wantService {
				t.Fatalf("ClassifyResourceService(%q) = %q, want %q", tc.resourceType, got, tc.wantService)
			}
			if got := ClassifyResourceCategory(tc.resourceType); got != tc.wantCategory {
				t.Fatalf("ClassifyResourceCategory(%q) = %q, want %q", tc.resourceType, got, tc.wantCategory)
			}
		})
	}
}
