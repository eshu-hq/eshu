// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// DecodeAWSResource decodes env.Payload into the latest awsv1.Resource struct
// for the "aws_resource" fact kind, dispatching on env.SchemaVersion major per
// Contract System v1 §3.2. Callers (reducer handlers) receive either the decoded
// struct or a classified *DecodeError; they must never substitute a zero-value
// struct on error.
func DecodeAWSResource(env Envelope) (awsv1.Resource, error) {
	return decodeLatestMajor[awsv1.Resource](FactKindAWSResource, env)
}

// EncodeAWSResource marshals an awsv1.Resource into the map[string]any payload
// shape an Envelope carries. It is the inverse of DecodeAWSResource for
// schema-version-1 payloads, used by collectors emitting this fact kind and by
// this module's round-trip tests.
func EncodeAWSResource(resource awsv1.Resource) (map[string]any, error) {
	return encodeToPayload(resource)
}

// DecodeAWSRelationship decodes env.Payload into the latest awsv1.Relationship
// struct for the "aws_relationship" fact kind. See DecodeAWSResource for the
// dispatch and error contract.
func DecodeAWSRelationship(env Envelope) (awsv1.Relationship, error) {
	return decodeLatestMajor[awsv1.Relationship](FactKindAWSRelationship, env)
}

// EncodeAWSRelationship marshals an awsv1.Relationship into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeAWSRelationship
// for schema-version-1 payloads.
func EncodeAWSRelationship(relationship awsv1.Relationship) (map[string]any, error) {
	return encodeToPayload(relationship)
}

// DecodeAWSSecurityGroupRule decodes env.Payload into the latest
// awsv1.SecurityGroupRule struct for the "aws_security_group_rule" fact kind.
// See DecodeAWSResource for the dispatch and error contract.
func DecodeAWSSecurityGroupRule(env Envelope) (awsv1.SecurityGroupRule, error) {
	return decodeLatestMajor[awsv1.SecurityGroupRule](FactKindAWSSecurityGroupRule, env)
}

// EncodeAWSSecurityGroupRule marshals an awsv1.SecurityGroupRule into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeAWSSecurityGroupRule for schema-version-1 payloads.
func EncodeAWSSecurityGroupRule(rule awsv1.SecurityGroupRule) (map[string]any, error) {
	return encodeToPayload(rule)
}

// DecodeEC2InstancePosture decodes env.Payload into the latest
// awsv1.EC2InstancePosture struct for the "ec2_instance_posture" fact kind. See
// DecodeAWSResource for the dispatch and error contract.
func DecodeEC2InstancePosture(env Envelope) (awsv1.EC2InstancePosture, error) {
	return decodeLatestMajor[awsv1.EC2InstancePosture](FactKindEC2InstancePosture, env)
}

// EncodeEC2InstancePosture marshals an awsv1.EC2InstancePosture into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeEC2InstancePosture for schema-version-1 payloads.
func EncodeEC2InstancePosture(posture awsv1.EC2InstancePosture) (map[string]any, error) {
	return encodeToPayload(posture)
}

// DecodeS3BucketPosture decodes env.Payload into the latest
// awsv1.S3BucketPosture struct for the "s3_bucket_posture" fact kind. See
// DecodeAWSResource for the dispatch and error contract.
func DecodeS3BucketPosture(env Envelope) (awsv1.S3BucketPosture, error) {
	return decodeLatestMajor[awsv1.S3BucketPosture](FactKindS3BucketPosture, env)
}

// EncodeS3BucketPosture marshals an awsv1.S3BucketPosture into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeS3BucketPosture for schema-version-1 payloads.
func EncodeS3BucketPosture(posture awsv1.S3BucketPosture) (map[string]any, error) {
	return encodeToPayload(posture)
}
