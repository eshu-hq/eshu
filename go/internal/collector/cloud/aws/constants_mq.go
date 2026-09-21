// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
