// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apigatewayv2

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			output[trimmed] = value
		}
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
	seen := make(map[string]struct{}, len(input))
	output := make([]string, 0, len(input))
	for _, value := range input {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		output = append(output, trimmed)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func boolOrNil(input *bool) any {
	if input == nil {
		return nil
	}
	return *input
}

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// stageResourceID keys a stage by its API id and stage name so two APIs cannot
// collide on a shared stage name.
func stageResourceID(apiID, stageName string) string {
	return strings.TrimSpace(apiID) + "/stages/" + strings.TrimSpace(stageName)
}

// routeResourceID keys a route by its API id and route id.
func routeResourceID(apiID, routeID string) string {
	return strings.TrimSpace(apiID) + "/routes/" + strings.TrimSpace(routeID)
}

// integrationResourceID keys an integration by its API id and integration id.
func integrationResourceID(apiID, integrationID string) string {
	return strings.TrimSpace(apiID) + "/integrations/" + strings.TrimSpace(integrationID)
}

// authorizerResourceID keys an authorizer by its API id and authorizer id.
func authorizerResourceID(apiID, authorizerID string) string {
	return strings.TrimSpace(apiID) + "/authorizers/" + strings.TrimSpace(authorizerID)
}

func domainResourceID(domain DomainName) string {
	return firstNonEmpty(domain.Name, domain.ARN)
}

// apiARN synthesizes the API Gateway v2 API ARN from the region and API id. The
// partition is derived from the region (mirroring the v1 API Gateway sibling),
// and the API id is the bare control-plane id, so the synthesized ARN never
// hardcodes an account or a commercial partition.
func apiARN(region, apiID string) string {
	apiID = strings.TrimSpace(apiID)
	if apiID == "" {
		return ""
	}
	region = strings.TrimSpace(region)
	return "arn:" + awscloud.PartitionForRegion(region) + ":apigateway:" + region + "::/apis/" + apiID
}

func stageARN(region, apiID, stageName string) string {
	apiID = strings.TrimSpace(apiID)
	stageName = strings.TrimSpace(stageName)
	if apiID == "" || stageName == "" {
		return ""
	}
	return apiARN(region, apiID) + "/stages/" + stageName
}

// isARN reports whether value looks like an AWS ARN.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// integrationTargetFromRoute extracts the integration id a route target
// references. API Gateway v2 reports route targets as "integrations/<id>".
func integrationTargetFromRoute(target string) string {
	target = strings.TrimSpace(target)
	const prefix = "integrations/"
	if !strings.HasPrefix(target, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(target, prefix))
}

// lambdaARNFromIntegrationURI extracts the Lambda function ARN an AWS_PROXY
// integration invokes. v2 HTTP APIs report the bare function ARN directly; v2
// WebSocket and legacy shapes use the apigateway path form
// "arn:aws:apigateway:<region>:lambda:path/.../functions/<lambdaArn>/invocations".
// It returns "" when the URI is not a Lambda invocation target.
func lambdaARNFromIntegrationURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	if strings.HasPrefix(uri, "arn:") && strings.Contains(uri, ":lambda:") && !strings.HasPrefix(uri, "arn:aws:apigateway:") {
		// Bare Lambda function ARN.
		return uri
	}
	if strings.HasPrefix(uri, "arn:aws:apigateway:") {
		if idx := strings.Index(uri, "/functions/"); idx >= 0 {
			candidate := uri[idx+len("/functions/"):]
			if end := strings.Index(candidate, "/invocations"); end >= 0 {
				candidate = candidate[:end]
			}
			candidate = strings.TrimSpace(candidate)
			if strings.HasPrefix(candidate, "arn:") && strings.Contains(candidate, ":lambda:") {
				return candidate
			}
		}
	}
	return ""
}

// httpEndpointFromIntegrationURI returns the external HTTP(S) endpoint an
// HTTP_PROXY integration forwards to, or "" when the URI is not an HTTP URL.
func httpEndpointFromIntegrationURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
		return uri
	}
	return ""
}

// userPoolIDFromIssuer extracts the bare Cognito user pool id from a JWT issuer
// URL of the form "https://cognito-idp.<region>.amazonaws.com/<poolId>". It
// returns "" for non-Cognito issuers so a generic OIDC issuer does not dangle as
// a Cognito user pool join. The bare pool id is the resource_id the Cognito
// scanner publishes for the user pool node.
func userPoolIDFromIssuer(issuer string) string {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return ""
	}
	const scheme = "https://"
	trimmed := strings.TrimPrefix(issuer, scheme)
	if trimmed == issuer {
		return ""
	}
	host, path, ok := strings.Cut(trimmed, "/")
	if !ok {
		return ""
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if !strings.HasPrefix(host, "cognito-idp.") || !strings.HasSuffix(host, ".amazonaws.com") {
		return ""
	}
	poolID := strings.TrimSpace(strings.Trim(path, "/"))
	// A Cognito user pool id has the shape "<region>_<suffix>"; reject empty or
	// further-pathed values so only the bare pool id becomes a join key.
	if poolID == "" || strings.Contains(poolID, "/") {
		return ""
	}
	return poolID
}
