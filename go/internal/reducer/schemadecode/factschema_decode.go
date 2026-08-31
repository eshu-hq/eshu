// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// DecodeAWSResource decodes one aws_resource envelope into the typed
// awsv1.Resource struct through the contracts seam, returning a self-classifying
// *factDecodeError when the payload is missing a required field or otherwise
// malformed. It is the single decode site for the aws_resource kind on the
// reducer side: every handler and join-index builder that consumes aws_resource
// facts decodes through here, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent empty-string graph identity or a whole-intent
// abort.
func DecodeAWSResource(env facts.Envelope) (awsv1.Resource, error) {
	resource, err := factschema.DecodeAWSResource(FactschemaEnvelope(env))
	if err != nil {
		return awsv1.Resource{}, factdecode.NewFactDecodeError(factschema.FactKindAWSResource, err)
	}
	return resource, nil
}

// DecodeAWSRelationship decodes one aws_relationship envelope into the typed
// awsv1.Relationship struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required field
// (account_id, region, relationship_type, source_resource_id,
// target_resource_id) or is otherwise malformed. It is the single decode site
// for the aws_relationship kind on the reducer side.
func DecodeAWSRelationship(env facts.Envelope) (awsv1.Relationship, error) {
	relationship, err := factschema.DecodeAWSRelationship(FactschemaEnvelope(env))
	if err != nil {
		return awsv1.Relationship{}, factdecode.NewFactDecodeError(factschema.FactKindAWSRelationship, err)
	}
	return relationship, nil
}

// DecodeAWSSecurityGroupRule decodes one aws_security_group_rule envelope into
// the typed awsv1.SecurityGroupRule struct through the contracts seam, returning
// a self-classifying *factDecodeError when the payload is missing a required
// field (account_id, region, group_id, direction, ip_protocol, source_kind,
// source_value). It is the single decode site for this kind on the reducer side.
func DecodeAWSSecurityGroupRule(env facts.Envelope) (awsv1.SecurityGroupRule, error) {
	rule, err := factschema.DecodeAWSSecurityGroupRule(FactschemaEnvelope(env))
	if err != nil {
		return awsv1.SecurityGroupRule{}, factdecode.NewFactDecodeError(factschema.FactKindAWSSecurityGroupRule, err)
	}
	return rule, nil
}

// DecodeEC2InstancePosture decodes one ec2_instance_posture envelope into the
// typed awsv1.EC2InstancePosture struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required field
// (account_id, region). It is the single decode site for this kind on the
// reducer side.
func DecodeEC2InstancePosture(env facts.Envelope) (awsv1.EC2InstancePosture, error) {
	posture, err := factschema.DecodeEC2InstancePosture(FactschemaEnvelope(env))
	if err != nil {
		return awsv1.EC2InstancePosture{}, factdecode.NewFactDecodeError(factschema.FactKindEC2InstancePosture, err)
	}
	return posture, nil
}

// DecodeS3BucketPosture decodes one s3_bucket_posture envelope into the typed
// awsv1.S3BucketPosture struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required field
// (account_id, region). It is the single decode site for this kind on the
// reducer side.
func DecodeS3BucketPosture(env facts.Envelope) (awsv1.S3BucketPosture, error) {
	posture, err := factschema.DecodeS3BucketPosture(FactschemaEnvelope(env))
	if err != nil {
		return awsv1.S3BucketPosture{}, factdecode.NewFactDecodeError(factschema.FactKindS3BucketPosture, err)
	}
	return posture, nil
}

// DecodeRDSInstancePosture decodes one rds_instance_posture envelope into the
// typed awsv1.RDSInstancePosture struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (account_id, region, publicly_accessible, storage_encrypted,
// iam_database_authentication_enabled, multi_az, deletion_protection,
// backup_retention_period, performance_insights_enabled,
// performance_insights_retention_days — every non-pointer field the collector's
// NewRDSInstancePostureEnvelope always stamps). It is the single decode site
// for this kind on the reducer side: ExtractRDSPostureRows decodes through
// here so a fact missing its account/region identity dead-letters as a
// per-fact input_invalid quarantine instead of fabricating a
// CloudResource uid from an empty account_id/region.
func DecodeRDSInstancePosture(env facts.Envelope) (awsv1.RDSInstancePosture, error) {
	posture, err := factschema.DecodeRDSInstancePosture(FactschemaEnvelope(env))
	if err != nil {
		return awsv1.RDSInstancePosture{}, factdecode.NewFactDecodeError(factschema.FactKindRDSInstancePosture, err)
	}
	return posture, nil
}

// DecodeS3ExternalPrincipalGrant decodes one s3_external_principal_grant
// envelope into the typed awsv1.S3ExternalPrincipalGrant struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing a required field (account_id, region, principal_kind,
// principal_value, grant_outcome, is_public, is_cross_account,
// is_service_principal, is_unsupported — every non-pointer field the
// collector's NewS3ExternalPrincipalGrantEnvelope always stamps). It is the
// single decode site for this kind on the reducer side:
// ExtractS3ExternalPrincipalGrantRows decodes through here so a fact missing
// its account/region or principal identity dead-letters as a per-fact
// input_invalid quarantine instead of fabricating a GRANTS_ACCESS_TO edge from
// an empty principal identity.
func DecodeS3ExternalPrincipalGrant(env facts.Envelope) (awsv1.S3ExternalPrincipalGrant, error) {
	grant, err := factschema.DecodeS3ExternalPrincipalGrant(FactschemaEnvelope(env))
	if err != nil {
		return awsv1.S3ExternalPrincipalGrant{}, factdecode.NewFactDecodeError(factschema.FactKindS3ExternalPrincipalGrant, err)
	}
	return grant, nil
}

// DecodeAWSIAMPermission decodes one aws_iam_permission envelope into the typed
// iamv1.Permission struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required field
// (account_id, region, principal_arn, effect, policy_source). It is the single
// decode site for this kind on the reducer side.
func DecodeAWSIAMPermission(env facts.Envelope) (iamv1.Permission, error) {
	permission, err := factschema.DecodeAWSIAMPermission(FactschemaEnvelope(env))
	if err != nil {
		return iamv1.Permission{}, factdecode.NewFactDecodeError(factschema.FactKindAWSIAMPermission, err)
	}
	return permission, nil
}

// DecodeAWSResourcePolicyPermission decodes one aws_resource_policy_permission
// envelope into the typed iamv1.ResourcePolicyPermission struct through the
// contracts seam, returning a self-classifying *factDecodeError when the payload
// is missing a required field (account_id, region, resource_arn, resource_type,
// effect). It is the single decode site for this kind on the reducer side.
func DecodeAWSResourcePolicyPermission(env facts.Envelope) (iamv1.ResourcePolicyPermission, error) {
	permission, err := factschema.DecodeAWSResourcePolicyPermission(FactschemaEnvelope(env))
	if err != nil {
		return iamv1.ResourcePolicyPermission{}, factdecode.NewFactDecodeError(factschema.FactKindAWSResourcePolicyPermission, err)
	}
	return permission, nil
}

// DecodeAWSIAMPrincipal decodes one aws_iam_principal envelope into the typed
// iamv1.Principal struct through the contracts seam, returning a self-classifying
// *factDecodeError when the payload is missing a required field (account_id,
// region, principal_arn, principal_type). It is the single decode site for this
// kind on the reducer side.
func DecodeAWSIAMPrincipal(env facts.Envelope) (iamv1.Principal, error) {
	principal, err := factschema.DecodeAWSIAMPrincipal(FactschemaEnvelope(env))
	if err != nil {
		return iamv1.Principal{}, factdecode.NewFactDecodeError(factschema.FactKindAWSIAMPrincipal, err)
	}
	return principal, nil
}

// DecodeGCPCloudResource decodes one gcp_cloud_resource envelope into the typed
// gcpv1.Resource struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (full_resource_name, asset_type) or is otherwise malformed. It is the
// single decode site for the gcp_cloud_resource kind on the reducer side:
// every handler and join-index builder that consumes gcp_cloud_resource facts
// decodes through here, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent empty-string graph identity or a whole-intent
// abort. This mirrors DecodeAWSResource.
func DecodeGCPCloudResource(env facts.Envelope) (gcpv1.Resource, error) {
	resource, err := factschema.DecodeGCPCloudResource(FactschemaEnvelope(env))
	if err != nil {
		return gcpv1.Resource{}, factdecode.NewFactDecodeError(factschema.FactKindGCPCloudResource, err)
	}
	return resource, nil
}

// DecodeGCPCloudRelationship decodes one gcp_cloud_relationship envelope into
// the typed gcpv1.Relationship struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (source_full_resource_name, target_full_resource_name,
// relationship_type) or is otherwise malformed. It is the single decode site
// for the gcp_cloud_relationship kind on the reducer side. This mirrors
// DecodeAWSRelationship.
func DecodeGCPCloudRelationship(env facts.Envelope) (gcpv1.Relationship, error) {
	relationship, err := factschema.DecodeGCPCloudRelationship(FactschemaEnvelope(env))
	if err != nil {
		return gcpv1.Relationship{}, factdecode.NewFactDecodeError(factschema.FactKindGCPCloudRelationship, err)
	}
	return relationship, nil
}

// FactschemaEnvelope adapts a go/internal/facts.Envelope to the contracts-module
// factschema.Envelope the Decode* seam accepts through the generated shared
// adapter. Keeping this wrapper preserves the reducer-local call sites while
// making factenvelope the single source for field mapping and version-less
// schema normalization.
//
// A version-less SchemaVersion is normalized to the current major-1 schema
// version. "Version-less" means either an empty string (what a fact carries
// in-memory before persistence) OR the sentinel "0.0.0" that the Postgres
// persist layer stamps for a fact its collector emitted with no version
// (go/internal/storage/postgres/facts.go, facts_streaming.go:
// emptyToDefault(SchemaVersion, "0.0.0")). A fact LOADED from Postgres for
// reduction therefore carries "0.0.0", not "", so both spellings of
// "the collector emitted no version" must normalize identically — otherwise a
// version-less family loaded from storage (the git code family: "file",
// "repository") dead-letters as an unsupported major and its whole graph
// collapses (PR #4753 corpus-gate P0). "0.0.0" is used nowhere as a real
// schema version — it is exclusively the persist-layer's empty marker
// (schemaVersionPattern accepts it, but no collector emits it), so treating it
// as version-less is safe for every other family, all of which stamp a concrete
// "1.0.0".
//
// This does NOT weaken accuracy: a present, genuine, unsupported major (for
// example "2.0.0") is NOT normalized and still dead-letters through the Decode*
// seam's default branch, and a fact missing a required identity field still
// dead-letters as input_invalid regardless of its version.
func FactschemaEnvelope(env facts.Envelope) factschema.Envelope {
	return factenvelope.FactSchemaFromInternal(env)
}
