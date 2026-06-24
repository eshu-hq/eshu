// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package route53resolver

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Route 53 Resolver metadata facts for one claimed account
// and region. It covers resolver endpoints, resolver rules and rule
// associations, DNS Firewall rule groups and domain lists (metadata and counts
// only), firewall rule group associations, and query log configurations.
//
// The scanner is metadata-only. It never reads DNS Firewall domain list
// contents or query log records, and it never mutates a resource. Relationship
// edges cross back to EC2-owned VPCs and subnets by AWS-reported identifier.
type Scanner struct {
	Client Client
}

// Scan observes Route 53 Resolver metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("route53resolver scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceRoute53Resolver:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceRoute53Resolver
	default:
		return nil, fmt.Errorf("route53resolver scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	endpoints, err := s.Client.ListResolverEndpoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("list resolver endpoints: %w", err)
	}
	for _, endpoint := range endpoints {
		emitted, err := emit(endpointObservation(boundary, endpoint), endpointRelationships(boundary, endpoint))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	rules, err := s.Client.ListResolverRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list resolver rules: %w", err)
	}
	for _, rule := range rules {
		emitted, err := emit(ruleObservation(boundary, rule), ruleRelationships(boundary, rule))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	ruleAssociations, err := s.Client.ListResolverRuleAssociations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list resolver rule associations: %w", err)
	}
	for _, association := range ruleAssociations {
		emitted, err := emit(
			ruleAssociationObservation(boundary, association),
			ruleAssociationRelationships(boundary, association),
		)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	firewallRuleGroups, err := s.Client.ListFirewallRuleGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list firewall rule groups: %w", err)
	}
	for _, group := range firewallRuleGroups {
		envelope, err := awscloud.NewResourceEnvelope(firewallRuleGroupObservation(boundary, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	domainLists, err := s.Client.ListFirewallDomainLists(ctx)
	if err != nil {
		return nil, fmt.Errorf("list firewall domain lists: %w", err)
	}
	for _, list := range domainLists {
		envelope, err := awscloud.NewResourceEnvelope(firewallDomainListObservation(boundary, list))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	firewallAssociations, err := s.Client.ListFirewallRuleGroupAssociations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list firewall rule group associations: %w", err)
	}
	for _, association := range firewallAssociations {
		emitted, err := emit(
			firewallRuleGroupAssociationObservation(boundary, association),
			firewallRuleGroupAssociationRelationships(boundary, association),
		)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	queryLogConfigs, err := s.Client.ListQueryLogConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list resolver query log configs: %w", err)
	}
	for _, config := range queryLogConfigs {
		envelope, err := awscloud.NewResourceEnvelope(queryLogConfigObservation(boundary, config))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

// emit builds one resource envelope plus its relationship envelopes.
func emit(
	resource awscloud.ResourceObservation,
	relationships []awscloud.RelationshipObservation,
) ([]facts.Envelope, error) {
	envelope, err := awscloud.NewResourceEnvelope(resource)
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{envelope}
	for _, observation := range relationships {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}
