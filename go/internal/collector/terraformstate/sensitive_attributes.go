// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import "strings"

var hardSensitiveStateAttributes = map[string]map[string]struct{}{
	"aws_cloudwatch_event_target": {
		"input":             {},
		"input_path":        {},
		"input_transformer": {},
	},
	"aws_iam_policy": {
		"policy": {},
	},
	"aws_lambda_function": {
		"environment": {},
	},
	"aws_sns_topic_subscription": {
		"endpoint": {},
	},
	"aws_ssm_parameter": {
		"value": {},
	},
	"pagerduty_service_integration": {
		"integration_key": {},
		"routing_key":     {},
	},
	"pagerduty_user": {
		"email": {},
	},
	"pagerduty_webhook_subscription": {
		"config":         {},
		"html_url":       {},
		"private_url":    {},
		"secret":         {},
		"webhook_secret": {},
	},
}

func isHardSensitiveStateAttribute(resourceType string, attributeKey string) bool {
	attributes, ok := hardSensitiveStateAttributes[strings.TrimSpace(resourceType)]
	if !ok {
		return false
	}
	_, ok = attributes[strings.TrimSpace(attributeKey)]
	return ok
}
