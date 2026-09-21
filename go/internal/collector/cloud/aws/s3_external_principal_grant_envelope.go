// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

const (
	// S3ExternalPrincipalKindPublic identifies the wildcard public principal.
	S3ExternalPrincipalKindPublic = "public"
	// S3ExternalPrincipalKindAWSAccount identifies a bare AWS account principal.
	S3ExternalPrincipalKindAWSAccount = "aws_account"
	// S3ExternalPrincipalKindAWSARN identifies an AWS principal ARN.
	S3ExternalPrincipalKindAWSARN = "aws_arn"
	// S3ExternalPrincipalKindAWSService identifies an AWS service principal.
	S3ExternalPrincipalKindAWSService = "aws_service"
	// S3ExternalPrincipalKindUnsupported identifies a principal type that was
	// present but intentionally not persisted as exact graph-ready identity.
	S3ExternalPrincipalKindUnsupported = "unsupported"

	// S3ExternalPrincipalGrantOutcomePublic marks a wildcard public grant.
	S3ExternalPrincipalGrantOutcomePublic = "public"
	// S3ExternalPrincipalGrantOutcomeCrossAccount marks an exact AWS principal in
	// an account other than the bucket owner.
	S3ExternalPrincipalGrantOutcomeCrossAccount = "cross_account"
	// S3ExternalPrincipalGrantOutcomeAWSService marks an AWS service principal.
	S3ExternalPrincipalGrantOutcomeAWSService = "aws_service"
	// S3ExternalPrincipalGrantOutcomeUnsupported marks a principal shape that was
	// detected but not exact enough for graph projection.
	S3ExternalPrincipalGrantOutcomeUnsupported = "unsupported_principal"
)

// S3ExternalPrincipalGrantObservation describes one bounded S3 bucket-policy
// grant to an external principal. It carries only principal identity metadata
// and derived outcome flags. The raw policy document, statement body, actions,
// resources, conditions, ACL grants, object keys, and object data must not reach
// this observation.
type S3ExternalPrincipalGrantObservation struct {
	Boundary Boundary

	BucketARN  string
	BucketName string

	PrincipalKind      string
	PrincipalValue     string
	PrincipalAccountID string
	PrincipalPartition string
	PrincipalService   string
	GrantOutcome       string

	Public           bool
	CrossAccount     bool
	ServicePrincipal bool
	Unsupported      bool
	UnsupportedKey   string

	SourceStatementID string
	SourceURI         string
	SourceRecordID    string
}

// NewS3ExternalPrincipalGrantEnvelope builds a durable
// s3_external_principal_grant fact for one metadata-only bucket-policy
// principal observation. It validates that both bucket and principal identities
// are present and emits no graph truth.
func NewS3ExternalPrincipalGrantEnvelope(observation S3ExternalPrincipalGrantObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	bucketARN := strings.TrimSpace(observation.BucketARN)
	bucketName := strings.TrimSpace(observation.BucketName)
	if bucketARN == "" && bucketName == "" {
		return facts.Envelope{}, fmt.Errorf("s3 external principal grant observation requires bucket_arn or bucket_name")
	}
	bucketIdentity := bucketARN
	if bucketIdentity == "" {
		bucketIdentity = bucketName
	}
	principalKind := strings.TrimSpace(observation.PrincipalKind)
	principalValue := strings.TrimSpace(observation.PrincipalValue)
	if principalKind == "" || principalValue == "" {
		return facts.Envelope{}, fmt.Errorf("s3 external principal grant observation requires principal_kind and principal_value")
	}
	grantOutcome := strings.TrimSpace(observation.GrantOutcome)
	if grantOutcome == "" {
		return facts.Envelope{}, fmt.Errorf("s3 external principal grant observation requires grant_outcome")
	}

	stableKey := facts.StableID(facts.S3ExternalPrincipalGrantFactKind, map[string]any{
		"account_id":      observation.Boundary.AccountID,
		"bucket":          bucketIdentity,
		"grant_outcome":   grantOutcome,
		"principal_kind":  principalKind,
		"principal_value": principalValue,
		"region":          observation.Boundary.Region,
	})
	anchors := normalizedAnchors(nil, bucketARN, bucketName, s3PostureURI(bucketName), principalValue)
	payload, err := factschema.EncodeS3ExternalPrincipalGrant(awsv1.S3ExternalPrincipalGrant{
		AccountID:           observation.Boundary.AccountID,
		Region:              observation.Boundary.Region,
		ServiceKind:         boundaryValue(observation.Boundary.ServiceKind),
		CollectorInstanceID: boundaryValue(observation.Boundary.CollectorInstanceID),
		BucketARN:           stringValuePtr(bucketARN),
		BucketName:          stringValuePtr(bucketName),
		PrincipalKind:       principalKind,
		PrincipalValue:      principalValue,
		PrincipalAccountID:  stringValuePtr(strings.TrimSpace(observation.PrincipalAccountID)),
		PrincipalPartition:  stringValuePtr(strings.TrimSpace(observation.PrincipalPartition)),
		PrincipalService:    stringValuePtr(strings.TrimSpace(observation.PrincipalService)),
		GrantOutcome:        grantOutcome,
		IsPublic:            observation.Public || principalKind == S3ExternalPrincipalKindPublic || grantOutcome == S3ExternalPrincipalGrantOutcomePublic,
		IsCrossAccount:      observation.CrossAccount || grantOutcome == S3ExternalPrincipalGrantOutcomeCrossAccount,
		IsServicePrincipal:  observation.ServicePrincipal || principalKind == S3ExternalPrincipalKindAWSService || grantOutcome == S3ExternalPrincipalGrantOutcomeAWSService,
		IsUnsupported:       observation.Unsupported || principalKind == S3ExternalPrincipalKindUnsupported || grantOutcome == S3ExternalPrincipalGrantOutcomeUnsupported,
		UnsupportedKey:      stringValuePtr(strings.TrimSpace(observation.UnsupportedKey)),
		SourceStatementID:   stringValuePtr(strings.TrimSpace(observation.SourceStatementID)),
		CorrelationAnchors:  anchors,
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode s3_external_principal_grant payload: %w", err)
	}
	return newEnvelope(
		observation.Boundary,
		facts.S3ExternalPrincipalGrantFactKind,
		facts.S3ExternalPrincipalGrantSchemaVersionV1,
		stableKey,
		sourceRecordID(observation.SourceRecordID, bucketIdentity+"#grant#"+principalKind+"#"+principalValue),
		observation.SourceURI,
		payload,
	), nil
}
