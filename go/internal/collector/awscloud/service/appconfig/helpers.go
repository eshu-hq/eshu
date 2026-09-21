// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appconfig

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// applicationARN synthesizes the partition-aware AppConfig application ARN for
// id within boundary, or returns "" when id is blank. AppConfig list responses
// carry no ARN, so the scanner derives the application identity AWS itself uses
// (arn:<partition>:appconfig:<region>:<account>:application/<id>) with the
// boundary partition so GovCloud and China resolve to the real node instead of
// dangling the graph join. It never hardcodes arn:aws:.
func applicationARN(boundary awscloud.Boundary, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return appconfigARN(boundary, "application/"+id)
}

// environmentARN synthesizes the partition-aware AppConfig environment ARN for
// the environment id under applicationID, or returns "" when either id is
// blank. The form is
// arn:<partition>:appconfig:<region>:<account>:application/<app>/environment/<env>.
func environmentARN(boundary awscloud.Boundary, applicationID, id string) string {
	applicationID = strings.TrimSpace(applicationID)
	id = strings.TrimSpace(id)
	if applicationID == "" || id == "" {
		return ""
	}
	return appconfigARN(boundary, "application/"+applicationID+"/environment/"+id)
}

// profileARN synthesizes the partition-aware AppConfig configuration profile ARN
// for the profile id under applicationID, or returns "" when either id is blank.
// The form is
// arn:<partition>:appconfig:<region>:<account>:application/<app>/configurationprofile/<profile>.
func profileARN(boundary awscloud.Boundary, applicationID, id string) string {
	applicationID = strings.TrimSpace(applicationID)
	id = strings.TrimSpace(id)
	if applicationID == "" || id == "" {
		return ""
	}
	return appconfigARN(boundary, "application/"+applicationID+"/configurationprofile/"+id)
}

// deploymentStrategyARN synthesizes the partition-aware AppConfig deployment
// strategy ARN for id within boundary, or returns "" when id is blank. The form
// is arn:<partition>:appconfig:<region>:<account>:deploymentstrategy/<id>.
func deploymentStrategyARN(boundary awscloud.Boundary, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return appconfigARN(boundary, "deploymentstrategy/"+id)
}

// appconfigARN assembles a partition-aware AppConfig ARN with the trailing
// resource segment from boundary. The partition is derived from the boundary
// region (aws / aws-cn / aws-us-gov) so synthesized identities match the real
// node in every partition; arn:aws: is never hardcoded.
func appconfigARN(boundary awscloud.Boundary, resource string) string {
	partition := awscloud.PartitionForBoundary(boundary)
	account := strings.TrimSpace(boundary.AccountID)
	region := strings.TrimSpace(boundary.Region)
	return "arn:" + partition + ":appconfig:" + region + ":" + account + ":" + resource
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty, keeping omitempty-style payload behavior consistent.
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

// cloneStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
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
