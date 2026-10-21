// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

// TestMessagingConstantsMatchAWSCloudValues pins every messaging-category constant
// to the exact value it carried in package awscloud before the #6696 catalog
// consolidation. The consolidation moves declarations only: any value drift
// here is a behavior change, not a move.
func TestMessagingConstantsMatchAWSCloudValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "RelationshipEventBridgeRuleOnEventBus", got: RelationshipEventBridgeRuleOnEventBus, want: "eventbridge_rule_on_event_bus"},
		{name: "RelationshipEventBridgeRuleTargetsResource", got: RelationshipEventBridgeRuleTargetsResource, want: "eventbridge_rule_targets_resource"},
		{name: "RelationshipFirehoseStreamDeliversToOpenSearchDomain", got: RelationshipFirehoseStreamDeliversToOpenSearchDomain, want: "firehose_stream_delivers_to_opensearch_domain"},
		{name: "RelationshipFirehoseStreamDeliversToRedshiftCluster", got: RelationshipFirehoseStreamDeliversToRedshiftCluster, want: "firehose_stream_delivers_to_redshift_cluster"},
		{name: "RelationshipFirehoseStreamDeliversToS3Bucket", got: RelationshipFirehoseStreamDeliversToS3Bucket, want: "firehose_stream_delivers_to_s3_bucket"},
		{name: "RelationshipFirehoseStreamLogsToCloudWatchLogGroup", got: RelationshipFirehoseStreamLogsToCloudWatchLogGroup, want: "firehose_stream_logs_to_cloudwatch_log_group"},
		{name: "RelationshipFirehoseStreamSourcedFromKinesisStream", got: RelationshipFirehoseStreamSourcedFromKinesisStream, want: "firehose_stream_sourced_from_kinesis_stream"},
		{name: "RelationshipFirehoseStreamUsesIAMRole", got: RelationshipFirehoseStreamUsesIAMRole, want: "firehose_stream_uses_iam_role"},
		{name: "RelationshipFirehoseStreamUsesKMSKey", got: RelationshipFirehoseStreamUsesKMSKey, want: "firehose_stream_uses_kms_key"},
		{name: "RelationshipFirehoseStreamUsesLambdaTransform", got: RelationshipFirehoseStreamUsesLambdaTransform, want: "firehose_stream_uses_lambda_transform"},
		{name: "RelationshipMQBrokerInVPC", got: RelationshipMQBrokerInVPC, want: "mq_broker_in_vpc"},
		{name: "RelationshipMQBrokerLogsToCloudWatchLogGroup", got: RelationshipMQBrokerLogsToCloudWatchLogGroup, want: "mq_broker_logs_to_cloudwatch_log_group"},
		{name: "RelationshipMQBrokerUsesConfiguration", got: RelationshipMQBrokerUsesConfiguration, want: "mq_broker_uses_configuration"},
		{name: "RelationshipMQBrokerUsesKMSKey", got: RelationshipMQBrokerUsesKMSKey, want: "mq_broker_uses_kms_key"},
		{name: "RelationshipMQBrokerUsesSecurityGroup", got: RelationshipMQBrokerUsesSecurityGroup, want: "mq_broker_uses_security_group"},
		{name: "RelationshipMQBrokerUsesSubnet", got: RelationshipMQBrokerUsesSubnet, want: "mq_broker_uses_subnet"},
		{name: "RelationshipMSKClusterInVPC", got: RelationshipMSKClusterInVPC, want: "msk_cluster_in_vpc"},
		{name: "RelationshipMSKClusterUsesConfiguration", got: RelationshipMSKClusterUsesConfiguration, want: "msk_cluster_uses_configuration"},
		{name: "RelationshipMSKClusterUsesIAMRole", got: RelationshipMSKClusterUsesIAMRole, want: "msk_cluster_uses_iam_role"},
		{name: "RelationshipMSKClusterUsesKMSKey", got: RelationshipMSKClusterUsesKMSKey, want: "msk_cluster_uses_kms_key"},
		{name: "RelationshipMSKClusterUsesSecurityGroup", got: RelationshipMSKClusterUsesSecurityGroup, want: "msk_cluster_uses_security_group"},
		{name: "RelationshipMSKClusterUsesSubnet", got: RelationshipMSKClusterUsesSubnet, want: "msk_cluster_uses_subnet"},
		{name: "RelationshipMSKReplicatorUsesIAMRole", got: RelationshipMSKReplicatorUsesIAMRole, want: "msk_replicator_uses_iam_role"},
		{name: "RelationshipPinpointApplicationHasSegment", got: RelationshipPinpointApplicationHasSegment, want: "pinpoint_application_has_segment"},
		{name: "RelationshipPinpointChannelInApplication", got: RelationshipPinpointChannelInApplication, want: "pinpoint_channel_in_application"},
		{name: "RelationshipPinpointEmailChannelUsesSESConfigurationSet", got: RelationshipPinpointEmailChannelUsesSESConfigurationSet, want: "pinpoint_email_channel_uses_ses_configuration_set"},
		{name: "RelationshipPinpointEmailChannelUsesSESIdentity", got: RelationshipPinpointEmailChannelUsesSESIdentity, want: "pinpoint_email_channel_uses_ses_identity"},
		{name: "RelationshipSESConfigurationSetUsesDedicatedIPPool", got: RelationshipSESConfigurationSetUsesDedicatedIPPool, want: "ses_configuration_set_uses_dedicated_ip_pool"},
		{name: "RelationshipSESEmailIdentityDKIMUsesKMSKey", got: RelationshipSESEmailIdentityDKIMUsesKMSKey, want: "ses_email_identity_dkim_uses_kms_key"},
		{name: "RelationshipSESEmailIdentityUsesConfigurationSet", got: RelationshipSESEmailIdentityUsesConfigurationSet, want: "ses_email_identity_uses_configuration_set"},
		{name: "RelationshipSESEventDestinationPublishesToSNSTopic", got: RelationshipSESEventDestinationPublishesToSNSTopic, want: "ses_event_destination_publishes_to_sns_topic"},
		{name: "RelationshipSESEventDestinationStreamsToFirehose", got: RelationshipSESEventDestinationStreamsToFirehose, want: "ses_event_destination_streams_to_firehose"},
		{name: "RelationshipSNSTopicDeliversToResource", got: RelationshipSNSTopicDeliversToResource, want: "sns_topic_delivers_to_resource"},
		{name: "RelationshipSQSQueueUsesDeadLetterQueue", got: RelationshipSQSQueueUsesDeadLetterQueue, want: "sqs_queue_uses_dead_letter_queue"},
		{name: "RelationshipStepFunctionsStateMachineReferencesResource", got: RelationshipStepFunctionsStateMachineReferencesResource, want: "stepfunctions_state_machine_references_resource"},
		{name: "RelationshipStepFunctionsStateMachineUsesIAMRole", got: RelationshipStepFunctionsStateMachineUsesIAMRole, want: "stepfunctions_state_machine_uses_iam_role"},
		{name: "ResourceTypeEventBridgeEventBus", got: ResourceTypeEventBridgeEventBus, want: "aws_eventbridge_event_bus"},
		{name: "ResourceTypeEventBridgeRule", got: ResourceTypeEventBridgeRule, want: "aws_eventbridge_rule"},
		{name: "ResourceTypeFirehoseDeliveryStream", got: ResourceTypeFirehoseDeliveryStream, want: "aws_firehose_delivery_stream"},
		{name: "ResourceTypeMQBroker", got: ResourceTypeMQBroker, want: "aws_mq_broker"},
		{name: "ResourceTypeMQConfiguration", got: ResourceTypeMQConfiguration, want: "aws_mq_configuration"},
		{name: "ResourceTypeMSKCluster", got: ResourceTypeMSKCluster, want: "aws_msk_cluster"},
		{name: "ResourceTypeMSKConfiguration", got: ResourceTypeMSKConfiguration, want: "aws_msk_configuration"},
		{name: "ResourceTypeMSKReplicator", got: ResourceTypeMSKReplicator, want: "aws_msk_replicator"},
		{name: "ResourceTypePinpointApplication", got: ResourceTypePinpointApplication, want: "aws_pinpoint_application"},
		{name: "ResourceTypePinpointChannel", got: ResourceTypePinpointChannel, want: "aws_pinpoint_channel"},
		{name: "ResourceTypePinpointSegment", got: ResourceTypePinpointSegment, want: "aws_pinpoint_segment"},
		{name: "ResourceTypeSESConfigurationSet", got: ResourceTypeSESConfigurationSet, want: "aws_ses_configuration_set"},
		{name: "ResourceTypeSESDedicatedIPPool", got: ResourceTypeSESDedicatedIPPool, want: "aws_ses_dedicated_ip_pool"},
		{name: "ResourceTypeSESEmailIdentity", got: ResourceTypeSESEmailIdentity, want: "aws_ses_email_identity"},
		{name: "ResourceTypeSESEventDestination", got: ResourceTypeSESEventDestination, want: "aws_ses_event_destination"},
		{name: "ResourceTypeSNSTopic", got: ResourceTypeSNSTopic, want: "aws_sns_topic"},
		{name: "ResourceTypeSQSQueue", got: ResourceTypeSQSQueue, want: "aws_sqs_queue"},
		{name: "ResourceTypeStepFunctionsActivity", got: ResourceTypeStepFunctionsActivity, want: "aws_stepfunctions_activity"},
		{name: "ResourceTypeStepFunctionsStateMachine", got: ResourceTypeStepFunctionsStateMachine, want: "aws_stepfunctions_state_machine"},
		{name: "ServiceEventBridge", got: ServiceEventBridge, want: "eventbridge"},
		{name: "ServiceFirehose", got: ServiceFirehose, want: "firehose"},
		{name: "ServiceMQ", got: ServiceMQ, want: "mq"},
		{name: "ServiceMSK", got: ServiceMSK, want: "msk"},
		{name: "ServicePinpoint", got: ServicePinpoint, want: "pinpoint"},
		{name: "ServiceSES", got: ServiceSES, want: "ses"},
		{name: "ServiceSNS", got: ServiceSNS, want: "sns"},
		{name: "ServiceSQS", got: ServiceSQS, want: "sqs"},
		{name: "ServiceStepFunctions", got: ServiceStepFunctions, want: "stepfunctions"},
	}
	for _, tt := range tests {
		tt, name := tt, tt.name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q (byte-identical to pre-consolidation awscloud value)", tt.name, tt.got, tt.want)
			}
		})
	}
}
