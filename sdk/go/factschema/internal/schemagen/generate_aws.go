// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// AWSResourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_resource" payload.
const AWSResourceSchemaID = schemaBaseID + "aws/v1/resource.schema.json"

// AWSResourceSchema returns the JSON Schema bytes for awsv1.Resource. Both the
// generator's go:generate target and schema_gen_test.go's drift check call this
// function, so a generated artifact and its drift test can never disagree about
// how the schema is built.
func AWSResourceSchema() ([]byte, error) {
	return reflectSchema(AWSResourceSchemaID, "Eshu aws_resource Payload (schema version 1)", &awsv1.Resource{})
}

// AWSRelationshipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_relationship" payload.
const AWSRelationshipSchemaID = schemaBaseID + "aws/v1/relationship.schema.json"

// AWSRelationshipSchema returns the JSON Schema bytes for awsv1.Relationship.
func AWSRelationshipSchema() ([]byte, error) {
	return reflectSchema(AWSRelationshipSchemaID, "Eshu aws_relationship Payload (schema version 1)", &awsv1.Relationship{})
}

// AWSDNSRecordSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_dns_record" payload.
const AWSDNSRecordSchemaID = schemaBaseID + "aws/v1/dns_record.schema.json"

// AWSDNSRecordSchema returns the JSON Schema bytes for awsv1.DNSRecord.
func AWSDNSRecordSchema() ([]byte, error) {
	return reflectSchema(AWSDNSRecordSchemaID, "Eshu aws_dns_record Payload (schema version 1)", &awsv1.DNSRecord{})
}

// AWSImageReferenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_image_reference" payload.
const AWSImageReferenceSchemaID = schemaBaseID + "aws/v1/image_reference.schema.json"

// AWSImageReferenceSchema returns the JSON Schema bytes for awsv1.ImageReference.
func AWSImageReferenceSchema() ([]byte, error) {
	return reflectSchema(AWSImageReferenceSchemaID, "Eshu aws_image_reference Payload (schema version 1)", &awsv1.ImageReference{})
}

// AWSSecurityGroupRuleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_security_group_rule" payload.
const AWSSecurityGroupRuleSchemaID = schemaBaseID + "aws/v1/security_group_rule.schema.json"

// AWSSecurityGroupRuleSchema returns the JSON Schema bytes for
// awsv1.SecurityGroupRule.
func AWSSecurityGroupRuleSchema() ([]byte, error) {
	return reflectSchema(AWSSecurityGroupRuleSchemaID, "Eshu aws_security_group_rule Payload (schema version 1)", &awsv1.SecurityGroupRule{})
}

// AWSWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_warning" payload.
const AWSWarningSchemaID = schemaBaseID + "aws/v1/warning.schema.json"

// AWSWarningSchema returns the JSON Schema bytes for awsv1.Warning.
func AWSWarningSchema() ([]byte, error) {
	return reflectSchema(AWSWarningSchemaID, "Eshu aws_warning Payload (schema version 1)", &awsv1.Warning{})
}

// EC2InstancePostureSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "ec2_instance_posture" payload.
const EC2InstancePostureSchemaID = schemaBaseID + "aws/v1/ec2_instance_posture.schema.json"

// EC2InstancePostureSchema returns the JSON Schema bytes for
// awsv1.EC2InstancePosture.
func EC2InstancePostureSchema() ([]byte, error) {
	return reflectSchema(EC2InstancePostureSchemaID, "Eshu ec2_instance_posture Payload (schema version 1)", &awsv1.EC2InstancePosture{})
}

// RDSInstancePostureSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "rds_instance_posture" payload.
const RDSInstancePostureSchemaID = schemaBaseID + "aws/v1/rds_instance_posture.schema.json"

// RDSInstancePostureSchema returns the JSON Schema bytes for
// awsv1.RDSInstancePosture.
func RDSInstancePostureSchema() ([]byte, error) {
	return reflectSchema(RDSInstancePostureSchemaID, "Eshu rds_instance_posture Payload (schema version 1)", &awsv1.RDSInstancePosture{})
}

// S3BucketPostureSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "s3_bucket_posture" payload.
const S3BucketPostureSchemaID = schemaBaseID + "aws/v1/s3_bucket_posture.schema.json"

// S3BucketPostureSchema returns the JSON Schema bytes for awsv1.S3BucketPosture.
func S3BucketPostureSchema() ([]byte, error) {
	return reflectSchema(S3BucketPostureSchemaID, "Eshu s3_bucket_posture Payload (schema version 1)", &awsv1.S3BucketPosture{})
}

// S3ExternalPrincipalGrantSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "s3_external_principal_grant" payload.
const S3ExternalPrincipalGrantSchemaID = schemaBaseID + "aws/v1/s3_external_principal_grant.schema.json"

// S3ExternalPrincipalGrantSchema returns the JSON Schema bytes for
// awsv1.S3ExternalPrincipalGrant.
func S3ExternalPrincipalGrantSchema() ([]byte, error) {
	return reflectSchema(S3ExternalPrincipalGrantSchemaID, "Eshu s3_external_principal_grant Payload (schema version 1)", &awsv1.S3ExternalPrincipalGrant{})
}
