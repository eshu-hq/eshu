// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ses

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// identityResourceID returns the resource_id the email-identity node publishes.
// SES identity names are the email address or domain and are unique within an
// account/region, so the trimmed name is the stable identity.
func identityResourceID(identity EmailIdentity) string {
	return strings.TrimSpace(identity.Name)
}

// configurationSetResourceID returns the resource_id the configuration-set node
// publishes, the trimmed configuration set name.
func configurationSetResourceID(set ConfigurationSet) string {
	return strings.TrimSpace(set.Name)
}

// eventDestinationResourceID returns the resource_id an event-destination node
// publishes. It qualifies the destination name with its parent configuration
// set name so destinations with the same name under different sets stay
// distinct, and so the in-set edge can key the parent set exactly.
func eventDestinationResourceID(configurationSet string, destination EventDestination) string {
	configurationSet = strings.TrimSpace(configurationSet)
	name := strings.TrimSpace(destination.Name)
	switch {
	case configurationSet != "" && name != "":
		return configurationSet + "/" + name
	case name != "":
		return name
	default:
		return ""
	}
}

// dedicatedIPPoolResourceID returns the resource_id the dedicated-IP-pool node
// publishes, the trimmed pool name.
func dedicatedIPPoolResourceID(pool DedicatedIPPool) string {
	return strings.TrimSpace(pool.Name)
}

// identityARN synthesizes the partition-aware SES email-identity ARN for the
// resource node's arn field. SES does not return an ARN for an identity, so the
// scanner derives the partition from the scan boundary (aws / aws-cn /
// aws-us-gov) instead of hardcoding it. It returns "" when the boundary lacks
// the account id, region, or identity name needed to form a real ARN.
func identityARN(boundary awscloud.Boundary, identity EmailIdentity) string {
	name := strings.TrimSpace(identity.Name)
	account := strings.TrimSpace(boundary.AccountID)
	region := strings.TrimSpace(boundary.Region)
	if name == "" || account == "" || region == "" {
		return ""
	}
	return "arn:" + awscloud.PartitionForBoundary(boundary) + ":ses:" + region + ":" + account + ":identity/" + name
}

// configurationSetARN synthesizes the partition-aware SES configuration-set ARN
// for the resource node's arn field. SES does not return an ARN for a
// configuration set, so the scanner derives the partition from the scan boundary
// (aws / aws-cn / aws-us-gov) instead of hardcoding it. It returns "" when the
// boundary lacks the account id, region, or set name needed to form a real ARN.
func configurationSetARN(boundary awscloud.Boundary, set ConfigurationSet) string {
	name := strings.TrimSpace(set.Name)
	account := strings.TrimSpace(boundary.AccountID)
	region := strings.TrimSpace(boundary.Region)
	if name == "" || account == "" || region == "" {
		return ""
	}
	return "arn:" + awscloud.PartitionForBoundary(boundary) + ":ses:" + region + ":" + account +
		":configuration-set/" + name
}

// dedicatedIPPoolARN synthesizes the partition-aware SES dedicated-IP-pool ARN
// for the resource node's arn field. SES returns only the pool name, so the
// scanner derives the partition from the scan boundary instead of hardcoding it.
// It returns "" when the boundary lacks the account id, region, or pool name.
func dedicatedIPPoolARN(boundary awscloud.Boundary, pool DedicatedIPPool) string {
	name := strings.TrimSpace(pool.Name)
	account := strings.TrimSpace(boundary.AccountID)
	region := strings.TrimSpace(boundary.Region)
	if name == "" || account == "" || region == "" {
		return ""
	}
	return "arn:" + awscloud.PartitionForBoundary(boundary) + ":ses:" + region + ":" + account +
		":dedicated-ip-pool/" + name
}

// destinationClasses returns the bounded set of configured destination class
// enums for an event destination (sns, kinesis_firehose, event_bridge,
// cloud_watch, pinpoint). Only the destination kind is recorded; no destination
// secret, HEC token, or access key is ever read. It returns nil when nothing is
// configured so the attribute payload stays omitempty-consistent.
func destinationClasses(destination EventDestination) []string {
	var classes []string
	if strings.TrimSpace(destination.SNSTopicARN) != "" {
		classes = append(classes, "sns")
	}
	if strings.TrimSpace(destination.FirehoseDeliveryStreamARN) != "" {
		classes = append(classes, "kinesis_firehose")
	}
	if strings.TrimSpace(destination.EventBridgeBusARN) != "" {
		classes = append(classes, "event_bridge")
	}
	if destination.CloudWatchEnabled {
		classes = append(classes, "cloud_watch")
	}
	if strings.TrimSpace(destination.PinpointApplicationARN) != "" {
		classes = append(classes, "pinpoint")
	}
	return classes
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
