// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package xray

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// encryptionTypeKMS is the X-Ray encryption type value that indicates a
// customer-managed KMS key encrypts X-Ray data. Any other value (NONE, empty)
// means X-Ray uses its default encryption and no KMS edge is emitted.
const encryptionTypeKMS = "KMS"

// encryptionConfigResourceID builds the synthetic resource id for the X-Ray
// account-region encryption configuration. X-Ray exposes one encryption
// configuration per account and region with no ARN, so the scanner keys it by
// "<account>/<region>/xray-encryption-config" derived from the scan boundary.
func encryptionConfigResourceID(boundary awscloud.Boundary) string {
	account := strings.TrimSpace(boundary.AccountID)
	region := strings.TrimSpace(boundary.Region)
	return strings.Join([]string{account, region, "xray-encryption-config"}, "/")
}

// serviceCorrelationID builds the labeled correlation anchor identity a
// sampling rule matches. It joins the matched service name and service type so
// reducers can resolve the rule to the real service node by name during
// materialization. A wildcard ("*") match in either field is preserved so the
// anchor stays faithful to the rule configuration.
func serviceCorrelationID(serviceName, serviceType string) string {
	name := strings.TrimSpace(serviceName)
	serviceKind := strings.TrimSpace(serviceType)
	switch {
	case name == "" && serviceKind == "":
		return ""
	case serviceKind == "":
		return name
	case name == "":
		return serviceKind
	default:
		return name + "/" + serviceKind
	}
}

// isARN reports whether value has the canonical AWS ARN prefix. The scanner
// uses it to decide whether a reported KMS key reference is ARN-keyed (so the
// KMS edge carries a target_arn) or a bare key id/alias (so it does not).
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// isKMSAliasReference reports whether a reported X-Ray encryption KeyID names a
// KMS alias rather than a key. X-Ray PutEncryptionConfig accepts a bare alias
// name ("alias/MyKey") or an alias ARN ("arn:<partition>:kms:...:alias/MyKey"),
// and GetEncryptionConfig reports the reference verbatim. The KMS scanner emits
// aliases as aws_kms_alias nodes and keys as aws_kms_key nodes, so the X-Ray
// KMS edge must target the alias family for these references or it dangles. A
// bare alias name is identified by the "alias/" prefix; an alias ARN by an
// "alias/" resource segment after the final ":" (a key ARN carries "key/").
func isKMSAliasReference(value string) bool {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "alias/") {
		return true
	}
	if !isARN(trimmed) {
		return false
	}
	resource := trimmed[strings.LastIndex(trimmed, ":")+1:]
	return strings.HasPrefix(resource, "alias/")
}

// firstNonEmpty returns the first trimmed non-empty value, or "".
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// int32OrNil returns the dereferenced int32 value, or nil when the pointer is
// nil, so absent optional fields stay absent in the fact attributes instead of
// reporting a misleading zero.
func int32OrNil(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}
