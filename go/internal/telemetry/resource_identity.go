// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"unicode"
)

const (
	safeResourceIdentityUnknown = "unknown"
	safeResourceHashPrefix      = "sha256:"
	safeResourceHashLength      = 16
)

// ResourceLogIdentity is the safe structured-log representation of a raw
// infrastructure or cloud resource identifier.
type ResourceLogIdentity struct {
	// Fingerprint is a deterministic hash of the raw identifier. It lets
	// operators correlate repeated log lines without exposing the identifier.
	Fingerprint string
	// IdentityKind is the recognized identifier shape, such as aws_arn or
	// terraform_address.
	IdentityKind string
	// ResourceType is a bounded family derived without copying resource names.
	ResourceType string
}

// SafeResourceLogIdentity derives bounded log context from a raw resource
// identifier without returning the raw identifier itself.
func SafeResourceLogIdentity(identity string) ResourceLogIdentity {
	identity = strings.TrimSpace(identity)
	kind, resourceType := classifyResourceLogIdentity(identity)
	return ResourceLogIdentity{
		Fingerprint:  fingerprintResourceLogIdentity(identity),
		IdentityKind: kind,
		ResourceType: resourceType,
	}
}

// SafeResourceLogAttrs returns the standard structured log attrs for a raw
// resource identifier without including the raw identifier.
func SafeResourceLogAttrs(identity string) []slog.Attr {
	safe := SafeResourceLogIdentity(identity)
	return []slog.Attr{
		slog.String(LogKeyResourceFingerprint, safe.Fingerprint),
		slog.String(LogKeyResourceIdentityKind, safe.IdentityKind),
		slog.String(LogKeyResourceType, safe.ResourceType),
	}
}

func fingerprintResourceLogIdentity(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	encoded := hex.EncodeToString(sum[:])
	return safeResourceHashPrefix + encoded[:safeResourceHashLength]
}

func classifyResourceLogIdentity(identity string) (string, string) {
	if identity == "" {
		return safeResourceIdentityUnknown, safeResourceIdentityUnknown
	}
	if service, resource := parseAWSARN(identity); service != "" {
		return "aws_arn", awsARNResourceType(service, resource)
	}
	if resourceType := terraformAddressResourceType(identity); resourceType != "" {
		return "terraform_address", resourceType
	}
	return "resource_identifier", safeResourceIdentityUnknown
}

func parseAWSARN(identity string) (service string, resource string) {
	parts := strings.SplitN(identity, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" {
		return "", ""
	}
	service = normalizeResourceLogToken(parts[2])
	if service == "" {
		return "", ""
	}
	return service, strings.TrimSpace(parts[5])
}

func awsARNResourceType(service string, resource string) string {
	prefix := safeAWSARNResourcePrefix(service, resource)
	if prefix == "" {
		return service
	}
	return service + ":" + prefix
}

func safeAWSARNResourcePrefix(service string, resource string) string {
	resource = strings.TrimLeft(strings.TrimSpace(resource), ":/")
	if resource == "" {
		return ""
	}
	prefix := resource
	if before, _, ok := strings.Cut(prefix, "/"); ok {
		prefix = before
	}
	if before, _, ok := strings.Cut(prefix, ":"); ok {
		prefix = before
	}
	prefix = normalizeResourceLogToken(prefix)
	if prefix == "" {
		return ""
	}
	if _, ok := safeAWSARNResourcePrefixes[service][prefix]; !ok {
		return ""
	}
	return prefix
}

var safeAWSARNResourcePrefixes = map[string]map[string]struct{}{
	"access-analyzer": {"analyzer": {}, "archive-rule": {}},
	"apigateway":      {"apis": {}, "domainnames": {}, "restapis": {}, "stages": {}},
	"cloudfront":      {"distribution": {}, "origin-access-control": {}, "origin-access-identity": {}},
	"dynamodb":        {"table": {}, "global-table": {}},
	"ec2":             {"instance": {}, "security-group": {}, "subnet": {}, "vpc": {}, "volume": {}},
	"ecr":             {"repository": {}},
	"ecs":             {"cluster": {}, "service": {}, "task": {}, "task-definition": {}},
	"eks":             {"cluster": {}, "nodegroup": {}, "addon": {}, "identityproviderconfig": {}},
	"elasticache":     {"cluster": {}, "replicationgroup": {}, "snapshot": {}, "subnetgroup": {}},
	"elasticloadbalancing": {
		"app": {}, "listener": {}, "listener-rule": {}, "loadbalancer": {}, "net": {}, "targetgroup": {},
	},
	"events":         {"archive": {}, "event-bus": {}, "rule": {}},
	"glue":           {"catalog": {}, "connection": {}, "database": {}, "job": {}, "table": {}},
	"guardduty":      {"detector": {}, "filter": {}, "ipset": {}, "threatintelset": {}},
	"iam":            {"group": {}, "instance-profile": {}, "oidc-provider": {}, "policy": {}, "role": {}, "saml-provider": {}, "server-certificate": {}, "user": {}},
	"kafka":          {"cluster": {}, "configuration": {}, "vpc-connection": {}},
	"kms":            {"alias": {}, "key": {}},
	"lambda":         {"code-signing-config": {}, "event-source-mapping": {}, "function": {}, "layer": {}},
	"logs":           {"destination": {}, "log-group": {}, "resource-policy": {}},
	"organizations":  {"account": {}, "policy": {}, "root": {}, "ou": {}},
	"rds":            {"cluster": {}, "db": {}, "es": {}, "og": {}, "pg": {}, "ri": {}, "secgrp": {}, "snapshot": {}, "subgrp": {}},
	"redshift":       {"cluster": {}, "eventsubscription": {}, "parametergroup": {}, "securitygroup": {}, "snapshot": {}, "subnetgroup": {}},
	"route53":        {"change": {}, "delegationset": {}, "healthcheck": {}, "hostedzone": {}, "trafficpolicy": {}},
	"s3":             {"accesspoint": {}, "job": {}, "storagelens": {}},
	"secretsmanager": {"secret": {}},
	"sns":            {"topic": {}},
	"sqs":            {"queue": {}},
	"ssm":            {"document": {}, "maintenancewindow": {}, "managed-instance": {}, "parameter": {}, "patchbaseline": {}},
	"states":         {"activity": {}, "execution": {}, "statemachine": {}},
}

func terraformAddressResourceType(identity string) string {
	segments := strings.Split(strings.TrimSpace(identity), ".")
	if len(segments) < 2 {
		return ""
	}
	resourceTypeIndex := 0
	for resourceTypeIndex+1 < len(segments) && strings.TrimSpace(segments[resourceTypeIndex]) == "module" {
		if terraformModuleAddressSegment(segments[resourceTypeIndex+1]) == "" {
			return ""
		}
		resourceTypeIndex += 2
	}
	if resourceTypeIndex >= len(segments)-1 {
		return ""
	}
	resourceType := strings.TrimSpace(segments[resourceTypeIndex])
	if !strings.HasPrefix(resourceType, "aws_") {
		return ""
	}
	return normalizeTerraformAddressType(resourceType)
}

func terraformModuleAddressSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	if before, _, ok := strings.Cut(segment, "["); ok {
		segment = before
	}
	return strings.TrimSpace(segment)
}

func normalizeTerraformAddressType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, current := range value {
		if current != '_' && current != '-' && !unicode.IsLower(current) && !unicode.IsDigit(current) {
			return ""
		}
	}
	return value
}

func normalizeResourceLogToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, current := range value {
		switch {
		case current == '-' || current == '_':
			builder.WriteRune(current)
		case unicode.IsLetter(current) || unicode.IsDigit(current):
			builder.WriteRune(unicode.ToLower(current))
		default:
			return ""
		}
	}
	return builder.String()
}
