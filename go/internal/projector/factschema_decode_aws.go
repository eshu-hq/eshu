// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// This file holds projector-side decode wrappers for AWS fact kinds that feed
// reducer intent admission. The wrappers keep the raw payload access out of
// intent routing while preserving the factschema Decode* error classification.

func decodeAWSResource(env facts.Envelope) (awsv1.Resource, error) {
	resource, err := factschema.DecodeAWSResource(factschemaEnvelope(env))
	if err != nil {
		return awsv1.Resource{}, newProjectorDecodeError(factschema.FactKindAWSResource, err)
	}
	return resource, nil
}

func decodeAWSRelationship(env facts.Envelope) (awsv1.Relationship, error) {
	relationship, err := factschema.DecodeAWSRelationship(factschemaEnvelope(env))
	if err != nil {
		return awsv1.Relationship{}, newProjectorDecodeError(factschema.FactKindAWSRelationship, err)
	}
	return relationship, nil
}

func decodeEC2InstancePosture(env facts.Envelope) (awsv1.EC2InstancePosture, error) {
	posture, err := factschema.DecodeEC2InstancePosture(factschemaEnvelope(env))
	if err != nil {
		return awsv1.EC2InstancePosture{}, newProjectorDecodeError(factschema.FactKindEC2InstancePosture, err)
	}
	return posture, nil
}

func decodeS3BucketPosture(env facts.Envelope) (awsv1.S3BucketPosture, error) {
	posture, err := factschema.DecodeS3BucketPosture(factschemaEnvelope(env))
	if err != nil {
		return awsv1.S3BucketPosture{}, newProjectorDecodeError(factschema.FactKindS3BucketPosture, err)
	}
	return posture, nil
}
