// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transfer

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Transfer Family metadata-only facts for one claimed account
// and region. It never creates, updates, deletes, starts, or stops a server or
// user, never imports or reads host keys or SSH public keys, and never persists
// user policy JSON, POSIX credential material, or any secret.
type Scanner struct {
	Client Client
}

// Scan observes AWS Transfer Family servers and their service-managed users
// through the configured client. Host key fingerprints, host key material, SSH
// public key bodies, user policy JSON, POSIX UID/GID material, login banners,
// and identity-provider invocation secrets stay outside the scanner contract.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("transfer scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceTransfer:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceTransfer
	default:
		return nil, fmt.Errorf("transfer scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	servers, err := s.Client.ListServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Transfer servers: %w", err)
	}
	for _, server := range servers {
		next, err := serverEnvelopes(boundary, server)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	users, err := s.Client.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Transfer users: %w", err)
	}
	for _, user := range users {
		next, err := userEnvelopes(boundary, user)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	return envelopes, nil
}

func serverEnvelopes(boundary awscloud.Boundary, server Server) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(serverObservation(boundary, server))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range serverRelationships(boundary, server) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func userEnvelopes(boundary awscloud.Boundary, user User) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(userObservation(boundary, user))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range userRelationships(boundary, user) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func serverObservation(boundary awscloud.Boundary, server Server) awscloud.ResourceObservation {
	serverARN := strings.TrimSpace(server.ARN)
	serverID := strings.TrimSpace(server.ServerID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          serverARN,
		ResourceID:   firstNonEmpty(serverARN, serverID),
		ResourceType: awscloud.ResourceTypeTransferServer,
		Name:         serverID,
		State:        strings.TrimSpace(server.State),
		Attributes: map[string]any{
			"server_id":              serverID,
			"domain":                 strings.TrimSpace(server.Domain),
			"endpoint_type":          strings.TrimSpace(server.EndpointType),
			"identity_provider_type": strings.TrimSpace(server.IdentityProviderType),
			"protocols":              cloneStringSlice(server.Protocols),
			"user_count":             server.UserCount,
			"security_policy_name":   strings.TrimSpace(server.SecurityPolicyName),
			"ip_address_type":        strings.TrimSpace(server.IPAddressType),
			"vpc_endpoint_id":        strings.TrimSpace(server.VPCEndpointID),
			"vpc_id":                 strings.TrimSpace(server.VPCID),
			"address_allocation_ids": cloneStringSlice(server.AddressAllocationIDs),
			"subnet_ids":             cloneStringSlice(server.SubnetIDs),
			"security_group_ids":     cloneStringSlice(server.SecurityGroupIDs),
			"certificate_arn":        strings.TrimSpace(server.CertificateARN),
			"logging_role_arn":       strings.TrimSpace(server.LoggingRoleARN),
			"structured_log_groups":  cloneStringSlice(server.StructuredLogDestinations),
		},
		CorrelationAnchors: []string{serverARN, serverID},
		SourceRecordID:     firstNonEmpty(serverARN, serverID),
	}
}

func userObservation(boundary awscloud.Boundary, user User) awscloud.ResourceObservation {
	userARN := strings.TrimSpace(user.ARN)
	userID := userResourceID(user)
	serverID := strings.TrimSpace(user.ServerID)
	userName := strings.TrimSpace(user.UserName)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          userARN,
		ResourceID:   userID,
		ResourceType: awscloud.ResourceTypeTransferUser,
		Name:         userName,
		Attributes: map[string]any{
			"server_id":               serverID,
			"user_name":               userName,
			"home_directory":          strings.TrimSpace(user.HomeDirectory),
			"home_directory_type":     strings.TrimSpace(user.HomeDirectoryType),
			"role_arn":                strings.TrimSpace(user.RoleARN),
			"home_directory_mappings": homeDirectoryMappingMaps(user.HomeDirectoryMappings),
		},
		CorrelationAnchors: []string{userARN, userID},
		SourceRecordID:     userID,
	}
}

// homeDirectoryMappingMaps renders the LOGICAL home-directory mappings as
// path-only maps. Each map carries the virtual entry path and the backing
// target path; no object or file contents are read or persisted.
func homeDirectoryMappingMaps(mappings []HomeDirectoryMapping) []map[string]any {
	if len(mappings) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(mappings))
	for _, mapping := range mappings {
		entry := strings.TrimSpace(mapping.Entry)
		target := strings.TrimSpace(mapping.Target)
		if entry == "" && target == "" {
			continue
		}
		output = append(output, map[string]any{
			"entry":  entry,
			"target": target,
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
