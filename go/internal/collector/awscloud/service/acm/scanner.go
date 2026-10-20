// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Certificate Manager metadata facts for one claimed account
// and region. It never calls GetCertificate, never calls ExportCertificate,
// and never persists certificate body PEM or private key material.
type Scanner struct {
	Client Client
}

// Scan observes ACM certificates through the configured client and emits
// metadata-only resource facts plus ACM-reported in-use-by relationships.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("acm scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceACM:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceACM
	default:
		return nil, fmt.Errorf("acm scanner received service_kind %q", boundary.ServiceKind)
	}

	certificates, err := s.Client.ListCertificates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ACM certificates: %w", err)
	}
	var envelopes []facts.Envelope
	for _, certificate := range certificates {
		resource, err := awscloud.NewResourceEnvelope(certificateObservation(boundary, certificate))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, observation := range inUseByRelationships(boundary, certificate) {
			envelope, err := awscloud.NewRelationshipEnvelope(observation)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func certificateObservation(boundary awscloud.Boundary, certificate Certificate) awscloud.ResourceObservation {
	certificateARN := strings.TrimSpace(certificate.ARN)
	subjectAlternativeNames := cloneStrings(certificate.SubjectAlternativeNames)
	inUseBy := cloneStrings(certificate.InUseBy)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          certificateARN,
		ResourceID:   firstNonEmpty(certificateARN, certificate.DomainName),
		ResourceType: awscloud.ResourceTypeACMCertificate,
		Name:         strings.TrimSpace(certificate.DomainName),
		State:        strings.TrimSpace(certificate.Status),
		Tags:         cloneStringMap(certificate.Tags),
		Attributes: map[string]any{
			"domain_name":               strings.TrimSpace(certificate.DomainName),
			"subject_alternative_names": subjectAlternativeNames,
			"status":                    strings.TrimSpace(certificate.Status),
			"type":                      strings.TrimSpace(certificate.Type),
			"issuer":                    strings.TrimSpace(certificate.Issuer),
			"not_before":                timeOrNil(certificate.NotBefore),
			"not_after":                 timeOrNil(certificate.NotAfter),
			"key_algorithm":             strings.TrimSpace(certificate.KeyAlgorithm),
			"signature_algorithm":       strings.TrimSpace(certificate.SignatureAlgorithm),
			"in_use_by":                 inUseBy,
		},
		CorrelationAnchors: append([]string{certificateARN, certificate.DomainName}, subjectAlternativeNames...),
		SourceRecordID:     firstNonEmpty(certificateARN, certificate.DomainName),
	}
}

func inUseByRelationships(boundary awscloud.Boundary, certificate Certificate) []awscloud.RelationshipObservation {
	certificateARN := strings.TrimSpace(certificate.ARN)
	if certificateARN == "" {
		return nil
	}
	relationships := make([]awscloud.RelationshipObservation, 0, len(certificate.InUseBy))
	for _, entry := range certificate.InUseBy {
		targetARN := strings.TrimSpace(entry)
		if !looksLikeARN(targetARN) {
			continue
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipACMCertificateUsedByResource,
			SourceResourceID: certificateARN,
			SourceARN:        certificateARN,
			TargetResourceID: targetARN,
			TargetARN:        targetARN,
			TargetType:       targetTypeForARN(targetARN),
			SourceRecordID:   certificateARN + "->" + targetARN,
		})
	}
	return relationships
}

// looksLikeARN keeps the relationship contract honest: ACM occasionally
// returns blank or hash-shaped entries when callers race a deletion. The
// scanner emits a relationship only when the in-use-by entry is an ARN.
func looksLikeARN(value string) bool {
	return strings.HasPrefix(value, "arn:")
}

// targetTypeForARN maps an ARN service prefix to one of the Eshu resource
// type constants when the target service is one ACM commonly fronts (ELB v2,
// CloudFront, API Gateway, AppSync, App Runner). Unknown ARN-addressable
// targets fall back to the generic "aws_resource" type so a relationship is
// still emitted; downstream correlation can resolve the precise resource later.
func targetTypeForARN(arn string) string {
	switch {
	case strings.Contains(arn, ":elasticloadbalancing:"):
		return awscloud.ResourceTypeELBv2LoadBalancer
	case strings.Contains(arn, ":cloudfront:"):
		return awscloud.ResourceTypeCloudFrontDistribution
	case strings.Contains(arn, ":apigateway:"):
		return awscloud.ResourceTypeAPIGatewayDomainName
	case strings.Contains(arn, ":appsync:"):
		return "aws_appsync_api"
	case strings.Contains(arn, ":apprunner:"):
		return "aws_apprunner_service"
	default:
		return "aws_resource"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
