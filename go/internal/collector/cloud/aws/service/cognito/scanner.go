// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cognito

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Scanner emits AWS Cognito user-pool and identity-pool metadata facts for one
// claimed account and region.
//
// The scanner is metadata-only. It never reads Cognito user records, never
// persists app-client secrets, and never persists identity-provider
// ProviderDetails. It also never calls Cognito mutation APIs. The RedactionKey
// is required so the scanner cannot be constructed without the means to route
// operator-supplied free text (developer provider names, group descriptions)
// through the shared AWS redaction path.
type Scanner struct {
	Client       Client
	RedactionKey redact.Key
}

// Scan observes Cognito user pools, app clients, identity providers, resource
// servers, groups, and identity pools through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("cognito scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("cognito scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceCognito:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceCognito
	default:
		return nil, fmt.Errorf("cognito scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	pools, err := s.Client.ListUserPools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Cognito user pools: %w", err)
	}
	for _, pool := range pools {
		poolEnvelopes, err := s.userPoolEnvelopes(ctx, boundary, pool)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, poolEnvelopes...)
	}

	identityPools, err := s.Client.ListIdentityPools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Cognito identity pools: %w", err)
	}
	for _, identityPool := range identityPools {
		identityPoolEnvelopes, err := s.identityPoolEnvelopes(boundary, identityPool)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, identityPoolEnvelopes...)
	}

	return envelopes, nil
}

func (s Scanner) userPoolEnvelopes(
	ctx context.Context,
	boundary aws.Boundary,
	pool UserPool,
) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(userPoolObservation(boundary, pool))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range userPoolLambdaTriggerRelationships(boundary, pool) {
		relationship, err := aws.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}

	poolID := strings.TrimSpace(pool.ID)
	if poolID == "" {
		return envelopes, nil
	}

	clients, err := s.Client.ListUserPoolClients(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("list Cognito user pool clients for pool %q: %w", poolID, err)
	}
	for _, client := range clients {
		resource, err := aws.NewResourceEnvelope(userPoolClientObservation(boundary, client))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		if relationship := userPoolClientRelationship(boundary, client, pool.ARN); relationship != nil {
			envelope, err := aws.NewRelationshipEnvelope(*relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}

	providers, err := s.Client.ListIdentityProviders(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("list Cognito identity providers for pool %q: %w", poolID, err)
	}
	for _, provider := range providers {
		resource, err := aws.NewResourceEnvelope(identityProviderObservation(boundary, provider))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	resourceServers, err := s.Client.ListResourceServers(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("list Cognito resource servers for pool %q: %w", poolID, err)
	}
	for _, resourceServer := range resourceServers {
		resource, err := aws.NewResourceEnvelope(resourceServerObservation(boundary, resourceServer))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	groups, err := s.Client.ListGroups(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("list Cognito groups for pool %q: %w", poolID, err)
	}
	for _, group := range groups {
		resource, err := aws.NewResourceEnvelope(s.groupObservation(boundary, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	return envelopes, nil
}

func (s Scanner) identityPoolEnvelopes(
	boundary aws.Boundary,
	pool IdentityPool,
) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(s.identityPoolObservation(boundary, pool))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range identityPoolRelationships(boundary, pool) {
		relationship, err := aws.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func userPoolObservation(boundary aws.Boundary, pool UserPool) aws.ResourceObservation {
	poolARN := strings.TrimSpace(pool.ARN)
	poolID := strings.TrimSpace(pool.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          poolARN,
		ResourceID:   firstNonEmpty(poolID, poolARN),
		ResourceType: aws.ResourceTypeCognitoUserPool,
		Name:         strings.TrimSpace(pool.Name),
		// The Cognito API deprecated the user-pool Status field and no longer
		// populates it, so the envelope state stays empty rather than carrying a
		// fabricated value. Deletion protection is reported as an attribute.
		Tags: pool.Tags,
		Attributes: map[string]any{
			"user_pool_id":              poolID,
			"mfa_configuration":         strings.TrimSpace(pool.MFAConfiguration),
			"deletion_protection":       strings.TrimSpace(pool.DeletionProtection),
			"domain":                    strings.TrimSpace(pool.Domain),
			"custom_domain":             strings.TrimSpace(pool.CustomDomain),
			"estimated_number_of_users": pool.EstimatedNumUsers,
			"created_at":                timeOrNil(pool.CreatedAt),
			"last_modified_at":          timeOrNil(pool.LastModifiedAt),
			"password_policy":           passwordPolicyMap(pool.PasswordPolicy),
			"lambda_triggers":           lambdaTriggerMaps(pool.LambdaTriggers),
		},
		CorrelationAnchors: []string{poolARN, poolID, strings.TrimSpace(pool.Name)},
		SourceRecordID:     firstNonEmpty(poolARN, poolID),
	}
}

func userPoolClientObservation(boundary aws.Boundary, client UserPoolClient) aws.ResourceObservation {
	clientID := strings.TrimSpace(client.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   clientID,
		ResourceType: aws.ResourceTypeCognitoUserPoolClient,
		Name:         strings.TrimSpace(client.Name),
		Attributes: map[string]any{
			"client_id":                            clientID,
			"user_pool_id":                         strings.TrimSpace(client.UserPoolID),
			"allowed_oauth_flows":                  cloneStrings(client.AllowedOAuthFlows),
			"allowed_oauth_scopes":                 cloneStrings(client.AllowedOAuthScopes),
			"allowed_oauth_flows_user_pool_client": client.AllowedOAuthFlowsUserPoolClient,
			"callback_urls":                        cloneStrings(client.CallbackURLs),
			"logout_urls":                          cloneStrings(client.LogoutURLs),
			"supported_identity_providers":         cloneStrings(client.SupportedIdentityProviders),
			"explicit_auth_flows":                  cloneStrings(client.ExplicitAuthFlows),
			"created_at":                           timeOrNil(client.CreatedAt),
			"last_modified_at":                     timeOrNil(client.LastModifiedAt),
		},
		CorrelationAnchors: []string{clientID, strings.TrimSpace(client.UserPoolID)},
		SourceRecordID:     strings.TrimSpace(client.UserPoolID) + "#client#" + clientID,
	}
}

func identityProviderObservation(boundary aws.Boundary, provider IdentityProvider) aws.ResourceObservation {
	poolID := strings.TrimSpace(provider.UserPoolID)
	providerName := strings.TrimSpace(provider.ProviderName)
	resourceID := poolID + "#provider#" + providerName
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeCognitoIdentityProvider,
		Name:         providerName,
		Attributes: map[string]any{
			"user_pool_id":     poolID,
			"provider_name":    providerName,
			"provider_type":    strings.TrimSpace(provider.ProviderType),
			"created_at":       timeOrNil(provider.CreatedAt),
			"last_modified_at": timeOrNil(provider.LastModifiedAt),
		},
		CorrelationAnchors: []string{resourceID, poolID, providerName},
		SourceRecordID:     resourceID,
	}
}

func resourceServerObservation(boundary aws.Boundary, resourceServer ResourceServer) aws.ResourceObservation {
	poolID := strings.TrimSpace(resourceServer.UserPoolID)
	identifier := strings.TrimSpace(resourceServer.Identifier)
	resourceID := poolID + "#resource-server#" + identifier
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeCognitoResourceServer,
		Name:         strings.TrimSpace(resourceServer.Name),
		Attributes: map[string]any{
			"user_pool_id": poolID,
			"identifier":   identifier,
			"scopes":       cloneStrings(resourceServer.Scopes),
		},
		CorrelationAnchors: []string{resourceID, poolID, identifier},
		SourceRecordID:     resourceID,
	}
}

func (s Scanner) groupObservation(boundary aws.Boundary, group Group) aws.ResourceObservation {
	poolID := strings.TrimSpace(group.UserPoolID)
	name := strings.TrimSpace(group.Name)
	resourceID := poolID + "#group#" + name
	attributes := map[string]any{
		"user_pool_id":     poolID,
		"group_name":       name,
		"role_arn":         strings.TrimSpace(group.RoleARN),
		"created_at":       timeOrNil(group.CreatedAt),
		"last_modified_at": timeOrNil(group.LastModifiedAt),
	}
	if group.Precedence != nil {
		attributes["precedence"] = *group.Precedence
	}
	if description := strings.TrimSpace(group.Description); description != "" {
		attributes["description"] = aws.RedactString(
			description,
			"aws_cognito_user_pool_group.description",
			s.RedactionKey,
		)
	}
	return aws.ResourceObservation{
		Boundary:           boundary,
		ResourceID:         resourceID,
		ResourceType:       aws.ResourceTypeCognitoUserPoolGroup,
		Name:               name,
		Attributes:         attributes,
		CorrelationAnchors: []string{resourceID, poolID, name, strings.TrimSpace(group.RoleARN)},
		SourceRecordID:     resourceID,
	}
}

func (s Scanner) identityPoolObservation(boundary aws.Boundary, pool IdentityPool) aws.ResourceObservation {
	poolARN := strings.TrimSpace(pool.ARN)
	poolID := strings.TrimSpace(pool.ID)
	attributes := map[string]any{
		"identity_pool_id":                 poolID,
		"allow_unauthenticated_identities": pool.AllowUnauthenticatedIdentities,
		"allow_classic_flow":               pool.AllowClassicFlow,
		"user_pool_providers":              identityPoolProviderMaps(pool.UserPoolProviders),
		"open_id_connect_provider_arns":    cloneStrings(pool.OpenIDConnectProviderARNs),
		"saml_provider_arns":               cloneStrings(pool.SAMLProviderARNs),
		"roles":                            rolesSummaryMap(pool.RolesSummary),
	}
	if developerProviderName := strings.TrimSpace(pool.DeveloperProviderName); developerProviderName != "" {
		attributes["developer_provider_name"] = aws.RedactString(
			developerProviderName,
			"aws_cognito_identity_pool.developer_provider_name",
			s.RedactionKey,
		)
	}
	return aws.ResourceObservation{
		Boundary:           boundary,
		ARN:                poolARN,
		ResourceID:         firstNonEmpty(poolARN, poolID),
		ResourceType:       aws.ResourceTypeCognitoIdentityPool,
		Name:               strings.TrimSpace(pool.Name),
		Tags:               pool.Tags,
		Attributes:         attributes,
		CorrelationAnchors: []string{poolARN, poolID, strings.TrimSpace(pool.Name)},
		SourceRecordID:     firstNonEmpty(poolARN, poolID),
	}
}

func passwordPolicyMap(policy *PasswordPolicy) map[string]any {
	if policy == nil {
		return nil
	}
	return map[string]any{
		"minimum_length":                   policy.MinimumLength,
		"require_uppercase":                policy.RequireUppercase,
		"require_lowercase":                policy.RequireLowercase,
		"require_numbers":                  policy.RequireNumbers,
		"require_symbols":                  policy.RequireSymbols,
		"temporary_password_validity_days": policy.TemporaryPasswordValidityDays,
		"password_history_size":            policy.PasswordHistorySize,
	}
}

func lambdaTriggerMaps(triggers []LambdaTrigger) []map[string]string {
	if len(triggers) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(triggers))
	for _, trigger := range triggers {
		output = append(output, map[string]string{
			"trigger": strings.TrimSpace(trigger.Trigger),
			"arn":     strings.TrimSpace(trigger.ARN),
		})
	}
	return output
}

func identityPoolProviderMaps(providers []IdentityPoolUserPoolProvider) []map[string]string {
	if len(providers) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(providers))
	for _, provider := range providers {
		output = append(output, map[string]string{
			"provider_name": strings.TrimSpace(provider.ProviderName),
			"client_id":     strings.TrimSpace(provider.ClientID),
		})
	}
	return output
}

func rolesSummaryMap(roles map[string]string) map[string]string {
	if len(roles) == 0 {
		return nil
	}
	output := make(map[string]string, len(roles))
	for key, value := range roles {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		output[trimmedKey] = strings.TrimSpace(value)
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
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

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}
