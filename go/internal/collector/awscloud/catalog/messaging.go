// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Messaging-category constants consolidated from constants_sns.go (#6696).

const (
	// ServiceSNS identifies the regional Amazon Simple Notification Service
	// metadata scan slice.
	ServiceSNS = "sns"
)

const (
	// ResourceTypeSNSTopic identifies an SNS topic metadata resource.
	ResourceTypeSNSTopic = "aws_sns_topic"
)

const (
	// RelationshipSNSTopicDeliversToResource records SNS subscription
	// evidence from a topic to an ARN-addressable subscriber.
	RelationshipSNSTopicDeliversToResource = "sns_topic_delivers_to_resource"
)

// Messaging-category constants consolidated from constants_sqs.go (#6696).

const (
	// ServiceSQS identifies the regional Amazon Simple Queue Service metadata
	// scan slice.
	ServiceSQS = "sqs"
)

const (
	// ResourceTypeSQSQueue identifies an SQS queue metadata resource.
	ResourceTypeSQSQueue = "aws_sqs_queue"
)

const (
	// RelationshipSQSQueueUsesDeadLetterQueue records SQS redrive policy
	// evidence from a source queue to its dead-letter queue.
	RelationshipSQSQueueUsesDeadLetterQueue = "sqs_queue_uses_dead_letter_queue"
)

// Messaging-category constants consolidated from constants_eventbridge.go (#6696).

const (
	// ServiceEventBridge identifies the regional Amazon EventBridge metadata
	// scan slice.
	ServiceEventBridge = "eventbridge"
)

const (
	// ResourceTypeEventBridgeEventBus identifies an EventBridge event bus
	// metadata resource.
	ResourceTypeEventBridgeEventBus = "aws_eventbridge_event_bus"
	// ResourceTypeEventBridgeRule identifies an EventBridge rule metadata
	// resource.
	ResourceTypeEventBridgeRule = "aws_eventbridge_rule"
)

const (
	// RelationshipEventBridgeRuleOnEventBus records EventBridge rule
	// membership on an event bus.
	RelationshipEventBridgeRuleOnEventBus = "eventbridge_rule_on_event_bus"
	// RelationshipEventBridgeRuleTargetsResource records EventBridge rule target
	// evidence when the target is ARN-addressable.
	RelationshipEventBridgeRuleTargetsResource = "eventbridge_rule_targets_resource"
)

// Messaging-category constants consolidated from constants_stepfunctions.go (#6696).

const (
	// ServiceStepFunctions identifies the regional AWS Step Functions metadata
	// scan slice.
	ServiceStepFunctions = "stepfunctions"
)

const (
	// ResourceTypeStepFunctionsStateMachine identifies a Step Functions state
	// machine metadata resource.
	ResourceTypeStepFunctionsStateMachine = "aws_stepfunctions_state_machine"
	// ResourceTypeStepFunctionsActivity identifies a Step Functions activity
	// metadata resource.
	ResourceTypeStepFunctionsActivity = "aws_stepfunctions_activity"
)

const (
	// RelationshipStepFunctionsStateMachineUsesIAMRole records a state
	// machine's reported execution-role dependency.
	RelationshipStepFunctionsStateMachineUsesIAMRole = "stepfunctions_state_machine_uses_iam_role"
	// RelationshipStepFunctionsStateMachineReferencesResource records evidence
	// that a state machine definition references an ARN-addressable resource as
	// a Task target.
	RelationshipStepFunctionsStateMachineReferencesResource = "stepfunctions_state_machine_references_resource"
)

// Messaging-category constants consolidated from constants_mq.go (#6696).

const (
	// ServiceMQ identifies the regional Amazon MQ metadata-only scan slice.
	// One slice covers both ActiveMQ and RabbitMQ broker engine types.
	ServiceMQ = "mq"
)

const (
	// ResourceTypeMQBroker identifies an Amazon MQ broker metadata resource.
	// The scanner emits broker identity, engine, deployment, instance, status,
	// and encryption metadata only; broker user passwords and queue/topic
	// message contents are never persisted.
	ResourceTypeMQBroker = "aws_mq_broker"
	// ResourceTypeMQConfiguration identifies an Amazon MQ broker configuration
	// metadata resource. The scanner emits configuration identity and the
	// latest revision summary; the configuration XML body is never persisted
	// because it can carry inline credentials, broker ACL rules, and
	// queue/topic names that may include customer identifiers.
	ResourceTypeMQConfiguration = "aws_mq_configuration"
)

const (
	// RelationshipMQBrokerInVPC records an Amazon MQ broker's reported VPC
	// placement when AWS reports a VPC identity. Amazon MQ describes brokers by
	// subnet rather than VPC, so this edge is reserved for evidence sources
	// that do report the VPC identity directly.
	RelationshipMQBrokerInVPC = "mq_broker_in_vpc"
	// RelationshipMQBrokerUsesSubnet records an Amazon MQ broker's reported
	// subnet placement.
	RelationshipMQBrokerUsesSubnet = "mq_broker_uses_subnet"
	// RelationshipMQBrokerUsesSecurityGroup records an Amazon MQ broker's
	// reported security group attachment.
	RelationshipMQBrokerUsesSecurityGroup = "mq_broker_uses_security_group"
	// RelationshipMQBrokerUsesKMSKey records an Amazon MQ broker's reported
	// customer-managed KMS key dependency for encryption at rest.
	RelationshipMQBrokerUsesKMSKey = "mq_broker_uses_kms_key"
	// RelationshipMQBrokerUsesConfiguration records the Amazon MQ broker
	// configuration currently applied to a broker.
	RelationshipMQBrokerUsesConfiguration = "mq_broker_uses_configuration"
	// RelationshipMQBrokerLogsToCloudWatchLogGroup records an Amazon MQ broker's
	// reported CloudWatch Logs log group target for general or audit logs.
	RelationshipMQBrokerLogsToCloudWatchLogGroup = "mq_broker_logs_to_cloudwatch_log_group"
)

// Messaging-category constants consolidated from constants_msk.go (#6696).

const (
	// ServiceMSK identifies the regional Amazon Managed Streaming for Apache
	// Kafka metadata-only scan slice.
	ServiceMSK = "msk"
)

const (
	// ResourceTypeMSKCluster identifies an MSK provisioned or serverless
	// cluster metadata resource.
	ResourceTypeMSKCluster = "aws_msk_cluster"
	// ResourceTypeMSKConfiguration identifies an MSK broker configuration
	// metadata resource. The scanner emits configuration identifiers and the
	// latest revision summary; raw server.properties bodies are never
	// persisted.
	ResourceTypeMSKConfiguration = "aws_msk_configuration"
	// ResourceTypeMSKReplicator identifies an MSK Replicator metadata
	// resource.
	ResourceTypeMSKReplicator = "aws_msk_replicator"
)

const (
	// RelationshipMSKClusterInVPC records an MSK cluster's reported VPC
	// placement when AWS reports the VPC identity.
	RelationshipMSKClusterInVPC = "msk_cluster_in_vpc"
	// RelationshipMSKClusterUsesSubnet records an MSK cluster's reported
	// client subnet placement.
	RelationshipMSKClusterUsesSubnet = "msk_cluster_uses_subnet"
	// RelationshipMSKClusterUsesSecurityGroup records an MSK cluster's
	// reported security group attachment.
	RelationshipMSKClusterUsesSecurityGroup = "msk_cluster_uses_security_group"
	// RelationshipMSKClusterUsesKMSKey records an MSK cluster's reported
	// data-volume KMS key dependency.
	RelationshipMSKClusterUsesKMSKey = "msk_cluster_uses_kms_key"
	// RelationshipMSKClusterUsesIAMRole records an MSK cluster's reported IAM
	// role dependency when AWS reports an ARN-shaped role identity.
	RelationshipMSKClusterUsesIAMRole = "msk_cluster_uses_iam_role"
	// RelationshipMSKClusterUsesConfiguration records the MSK broker
	// configuration currently applied to an MSK cluster.
	RelationshipMSKClusterUsesConfiguration = "msk_cluster_uses_configuration"
	// RelationshipMSKReplicatorUsesIAMRole records an MSK Replicator's
	// reported service-execution IAM role dependency.
	RelationshipMSKReplicatorUsesIAMRole = "msk_replicator_uses_iam_role"
)

// Messaging-category constants consolidated from constants_ses.go (#6696).

const (
	// ServiceSES identifies the regional Amazon Simple Email Service (SES v2)
	// metadata-only scan slice. The scanner reads email-identity, configuration
	// set, configuration-set event-destination, and dedicated-IP-pool
	// control-plane metadata through the SES v2 management APIs
	// (ListEmailIdentities, GetEmailIdentity, ListConfigurationSets,
	// GetConfigurationSet, GetConfigurationSetEventDestinations, and
	// ListDedicatedIpPools). It never sends email, never reads message or
	// template bodies, and never persists DKIM private keys, DKIM signing
	// tokens, identity policies, or SMTP credentials.
	ServiceSES = "ses"
)

const (
	// ResourceTypeSESEmailIdentity identifies an Amazon SES email identity
	// metadata resource (a verified email address or domain). The scanner emits
	// identity, identity type, verification status, DKIM enabled/status and
	// signing-attributes-origin enums, the MAIL FROM domain and behavior, and
	// the feedback-forwarding flag only. DKIM private keys and signing tokens
	// are never read or emitted.
	ResourceTypeSESEmailIdentity = "aws_ses_email_identity"
	// ResourceTypeSESConfigurationSet identifies an Amazon SES configuration set
	// metadata resource. The scanner emits identity, sending/reputation/TLS
	// options, the referenced sending (dedicated IP) pool name, and the tracking
	// custom-redirect domain only.
	ResourceTypeSESConfigurationSet = "aws_ses_configuration_set"
	// ResourceTypeSESEventDestination identifies an Amazon SES configuration-set
	// event destination metadata resource. The scanner emits identity, enabled
	// flag, matching event types, and the destination class (SNS, Firehose,
	// EventBridge, CloudWatch, Pinpoint) only. No destination secret, HEC token,
	// or access key is ever read.
	ResourceTypeSESEventDestination = "aws_ses_event_destination"
	// ResourceTypeSESDedicatedIPPool identifies an Amazon SES dedicated IP pool
	// metadata resource. The scanner emits the pool name only; the dedicated IP
	// addresses themselves are not enumerated.
	ResourceTypeSESDedicatedIPPool = "aws_ses_dedicated_ip_pool"
)

const (
	// RelationshipSESEmailIdentityUsesConfigurationSet records an SES email
	// identity's reported default configuration set. The target is keyed by the
	// configuration set name, which is the resource_id the SES configuration-set
	// node publishes, so the edge joins that node.
	RelationshipSESEmailIdentityUsesConfigurationSet = "ses_email_identity_uses_configuration_set"
	// RelationshipSESEmailIdentityDKIMUsesKMSKey records an SES email identity's
	// reported DKIM customer KMS key dependency. SES v2 does not surface a
	// customer KMS key on the DKIM attributes today, so the scanner emits this
	// edge only on the defensive path where AWS ever reports a key identifier;
	// the target is keyed by the KMS key identifier the KMS scanner publishes.
	RelationshipSESEmailIdentityDKIMUsesKMSKey = "ses_email_identity_dkim_uses_kms_key"
	// RelationshipSESConfigurationSetUsesDedicatedIPPool records a configuration
	// set's reported sending (dedicated IP) pool. The target is keyed by the pool
	// name, the resource_id the SES dedicated-IP-pool node publishes.
	RelationshipSESConfigurationSetUsesDedicatedIPPool = "ses_configuration_set_uses_dedicated_ip_pool"
	// RelationshipSESEventDestinationPublishesToSNSTopic records a configuration-
	// set event destination's reported Amazon SNS topic target. SES reports the
	// topic ARN, which matches how the SNS scanner publishes its topic
	// resource_id, so the edge joins the topic node.
	RelationshipSESEventDestinationPublishesToSNSTopic = "ses_event_destination_publishes_to_sns_topic"
	// RelationshipSESEventDestinationStreamsToFirehose records a configuration-
	// set event destination's reported Amazon Data Firehose delivery stream
	// target. SES reports the delivery stream ARN, which matches how the Firehose
	// scanner publishes its delivery-stream resource_id, so the edge joins the
	// stream node.
	RelationshipSESEventDestinationStreamsToFirehose = "ses_event_destination_streams_to_firehose"
)

// Messaging-category constants consolidated from constants_pinpoint.go (#6696).

const (
	// ServicePinpoint identifies the regional Amazon Pinpoint metadata-only scan
	// slice. The scanner reads application, segment, and channel-settings
	// control-plane metadata through the Pinpoint management APIs (GetApps,
	// GetSegments, GetChannels, and GetEmailChannel) and never reads or persists
	// endpoint records, addresses, message or template content, segment targeting
	// criteria values, channel credentials, or API/auth keys. It never sends a
	// message and never mutates Pinpoint state.
	ServicePinpoint = "pinpoint"
)

const (
	// ResourceTypePinpointApplication identifies an Amazon Pinpoint application
	// (project) metadata resource. The scanner emits identity (id, ARN, name),
	// creation time, and resource tags only.
	ResourceTypePinpointApplication = "aws_pinpoint_application"
	// ResourceTypePinpointSegment identifies an Amazon Pinpoint segment metadata
	// resource. The scanner emits identity (id, ARN, name, parent application
	// id), segment type (DIMENSIONAL or IMPORT), version, an imported-from-S3
	// presence flag with the import format and reported size, and lifecycle
	// timestamps only. Targeting dimensions, segment group criteria, the import
	// S3 URL, the import external id, and any endpoint attribute value are never
	// read or emitted.
	ResourceTypePinpointSegment = "aws_pinpoint_segment"
	// ResourceTypePinpointChannel identifies an Amazon Pinpoint channel-settings
	// metadata resource for one application and channel type (for example EMAIL,
	// SMS, APNS, GCM, VOICE). The scanner emits the channel type, the enabled and
	// archived flags, the version, and lifecycle timestamps only. For the email
	// channel it additionally records the referenced SES configuration set name
	// and SES identity. Channel credentials, API keys, certificates, tokens, and
	// the email from-address are never read or emitted.
	ResourceTypePinpointChannel = "aws_pinpoint_channel"
)

const (
	// RelationshipPinpointApplicationHasSegment records a Pinpoint segment's
	// membership in its parent application. The target is keyed by the
	// application id the application node publishes, so the edge joins that node.
	RelationshipPinpointApplicationHasSegment = "pinpoint_application_has_segment"
	// RelationshipPinpointChannelInApplication records a Pinpoint channel's
	// membership in its parent application. The target is keyed by the
	// application id the application node publishes, so the edge joins that node.
	RelationshipPinpointChannelInApplication = "pinpoint_channel_in_application"
	// RelationshipPinpointEmailChannelUsesSESIdentity records a Pinpoint email
	// channel's reported SES sending identity. Pinpoint reports the identity ARN;
	// the scanner extracts the identity name from the ARN to match the resource_id
	// the SES email-identity node publishes (the bare verified email/domain name),
	// so the edge joins that node.
	RelationshipPinpointEmailChannelUsesSESIdentity = "pinpoint_email_channel_uses_ses_identity"
	// RelationshipPinpointEmailChannelUsesSESConfigurationSet records a Pinpoint
	// email channel's reported SES configuration set. Pinpoint reports the
	// configuration set name, which matches the resource_id the SES
	// configuration-set node publishes, so the edge joins that node.
	RelationshipPinpointEmailChannelUsesSESConfigurationSet = "pinpoint_email_channel_uses_ses_configuration_set"
)

// Messaging-category constants consolidated from constants_firehose.go (#6696).

const (
	// ServiceFirehose identifies the regional Amazon Data Firehose
	// metadata-only scan slice. The slice covers Kinesis Data Firehose delivery
	// streams and the destination, source, encryption, and transform
	// relationships those streams report. The scanner never reads delivery
	// records or persists destination secret material.
	ServiceFirehose = "firehose"
)

const (
	// ResourceTypeFirehoseDeliveryStream identifies an Amazon Data Firehose
	// delivery stream metadata resource. The scanner records the stream name,
	// ARN, status, stream type, source type, encryption mode, and creation
	// time; it never reads delivery records and never persists destination
	// access keys, HEC tokens, Redshift passwords, or processing-configuration
	// Lambda bodies.
	ResourceTypeFirehoseDeliveryStream = "aws_firehose_delivery_stream"
)

const (
	// RelationshipFirehoseStreamDeliversToS3Bucket records a Firehose delivery
	// stream's reported S3 bucket destination (primary or backup). AWS reports
	// the bucket ARN directly, so the edge keys the S3 bucket node by its ARN.
	RelationshipFirehoseStreamDeliversToS3Bucket = "firehose_stream_delivers_to_s3_bucket"
	// RelationshipFirehoseStreamDeliversToRedshiftCluster records a Firehose
	// delivery stream's reported Amazon Redshift cluster destination, keyed by
	// the cluster identifier parsed from the JDBC URL host so it joins the
	// cluster node the Redshift scanner publishes.
	RelationshipFirehoseStreamDeliversToRedshiftCluster = "firehose_stream_delivers_to_redshift_cluster"
	// RelationshipFirehoseStreamDeliversToOpenSearchDomain records a Firehose
	// delivery stream's reported Amazon OpenSearch Service domain destination.
	// AWS reports the domain ARN directly, so the edge keys the domain node by
	// its ARN.
	RelationshipFirehoseStreamDeliversToOpenSearchDomain = "firehose_stream_delivers_to_opensearch_domain"
	// RelationshipFirehoseStreamSourcedFromKinesisStream records a Firehose
	// delivery stream's reported Kinesis Data Streams source. AWS reports the
	// source stream ARN directly, so the edge keys the data-stream node by its
	// ARN.
	RelationshipFirehoseStreamSourcedFromKinesisStream = "firehose_stream_sourced_from_kinesis_stream"
	// RelationshipFirehoseStreamUsesIAMRole records a Firehose delivery
	// stream's reported delivery IAM role dependency. AWS reports the role ARN
	// directly, so the edge keys the role node by its ARN.
	RelationshipFirehoseStreamUsesIAMRole = "firehose_stream_uses_iam_role"
	// RelationshipFirehoseStreamUsesKMSKey records a Firehose delivery stream's
	// reported server-side encryption (SSE) customer-managed KMS key. The
	// relationship emits only when AWS reports a customer-managed key ARN.
	RelationshipFirehoseStreamUsesKMSKey = "firehose_stream_uses_kms_key"
	// RelationshipFirehoseStreamLogsToCloudWatchLogGroup records a Firehose
	// delivery stream's reported CloudWatch Logs log group used for delivery
	// error logging, keyed by the log group name AWS reports.
	RelationshipFirehoseStreamLogsToCloudWatchLogGroup = "firehose_stream_logs_to_cloudwatch_log_group"
	// RelationshipFirehoseStreamUsesLambdaTransform records a Firehose delivery
	// stream's reported data-transformation Lambda function. Only the Lambda
	// ARN is recorded; the processing-configuration body is never persisted.
	RelationshipFirehoseStreamUsesLambdaTransform = "firehose_stream_uses_lambda_transform"
)
